package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/tjohnson/maestro/internal/config"
)

const defaultDockerHome = config.DockerHomeDefault

type ProcessSpec struct {
	Binary  string
	Args    []string
	Workdir string
	Env     map[string]string
}

type ProcessRunner interface {
	Kind() string
	ResolveBinary(name string) (string, error)
	VisibleWorkdir(hostPath string) string
	CommandContext(ctx context.Context, spec ProcessSpec) (*exec.Cmd, error)
}

func NewProcessRunner(docker *config.DockerConfig) (ProcessRunner, error) {
	resolved := config.ResolveDockerConfig(nil, docker)
	if strings.TrimSpace(resolved.Image) == "" {
		return localProcessRunner{}, nil
	}
	return newDockerProcessRunner(resolved)
}

type localProcessRunner struct{}

func (localProcessRunner) Kind() string {
	return "host"
}

func (localProcessRunner) ResolveBinary(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("find %s executable: %w", name, err)
	}
	return path, nil
}

func (localProcessRunner) VisibleWorkdir(hostPath string) string {
	return hostPath
}

func (localProcessRunner) CommandContext(ctx context.Context, spec ProcessSpec) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, spec.Binary, spec.Args...)
	cmd.Dir = spec.Workdir
	cmd.Env = MergeEnv(spec.Env)
	return cmd, nil
}

type dockerProcessRunner struct {
	dockerBinary string
	cfg          config.DockerConfig
}

func newDockerProcessRunner(cfg config.DockerConfig) (ProcessRunner, error) {
	dockerBinary, err := exec.LookPath("docker")
	if err != nil {
		return nil, fmt.Errorf("find docker executable: %w", err)
	}
	return &dockerProcessRunner{
		dockerBinary: dockerBinary,
		cfg:          cfg,
	}, nil
}

func (r *dockerProcessRunner) Kind() string {
	return "docker"
}

func (r *dockerProcessRunner) ResolveBinary(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("binary name is required")
	}
	return name, nil
}

func (r *dockerProcessRunner) VisibleWorkdir(hostPath string) string {
	if strings.TrimSpace(hostPath) == "" {
		return ""
	}
	return r.cfg.WorkspaceMountPath
}

func (r *dockerProcessRunner) CommandContext(ctx context.Context, spec ProcessSpec) (*exec.Cmd, error) {
	args := []string{"run", "--rm", "-i"}

	if runtime.GOOS != "windows" {
		args = append(args, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()))
	}
	if policy := strings.TrimSpace(r.cfg.PullPolicy); policy != "" {
		args = append(args, "--pull", policy)
	}

	if strings.TrimSpace(spec.Workdir) != "" {
		if err := requireExistingPath(spec.Workdir); err != nil {
			return nil, fmt.Errorf("docker workspace mount %q: %w", spec.Workdir, err)
		}
		args = append(args,
			"--mount", bindMountArg(spec.Workdir, r.cfg.WorkspaceMountPath, false),
			"--workdir", r.cfg.WorkspaceMountPath,
		)
	}

	if r.cfg.CPUs > 0 {
		args = append(args, "--cpus", strconv.FormatFloat(r.cfg.CPUs, 'f', -1, 64))
	}
	if strings.TrimSpace(r.cfg.Memory) != "" {
		args = append(args, "--memory", r.cfg.Memory)
	}
	if r.cfg.PIDsLimit > 0 {
		args = append(args, "--pids-limit", strconv.Itoa(r.cfg.PIDsLimit))
	}
	args = append(args, r.securityArgs()...)

	containerEnv, err := r.containerEnv(spec.Env)
	if err != nil {
		return nil, err
	}
	networkArgs, err := r.networkArgs(containerEnv)
	if err != nil {
		return nil, err
	}
	args = append(args, networkArgs...)
	homeTarget := strings.TrimSpace(containerEnv["HOME"])
	if homeTarget == "" {
		homeTarget = defaultDockerHome
		containerEnv["HOME"] = homeTarget
	}
	homeSource, err := r.prepareWritableHome(homeTarget)
	if err != nil {
		return nil, err
	}
	args = append(args, "--mount", bindMountArg(homeSource, homeTarget, false))

	for _, mount := range r.cfg.Mounts {
		if err := requireExistingPath(mount.Source); err != nil {
			return nil, fmt.Errorf("docker mount %q: %w", mount.Source, err)
		}
		args = append(args, "--mount", bindMountArg(mount.Source, mount.Target, mount.ReadOnly))
	}

	accessMounts, err := r.accessMounts(homeTarget)
	if err != nil {
		return nil, err
	}
	for _, mount := range accessMounts {
		args = append(args, "--mount", bindMountArg(mount.Source, mount.Target, mount.ReadOnly))
	}

	authMounts, err := r.applyAuthConfig(containerEnv, homeSource, homeTarget, spec.Binary)
	if err != nil {
		return nil, err
	}
	for _, mount := range authMounts {
		args = append(args, "--mount", bindMountArg(mount.Source, mount.Target, mount.ReadOnly))
	}

	cacheMounts, err := r.cacheMounts(homeTarget)
	if err != nil {
		return nil, err
	}
	for _, mount := range cacheMounts {
		args = append(args, "--mount", bindMountArg(mount.Source, mount.Target, false))
	}

	keys := make([]string, 0, len(containerEnv))
	for key := range containerEnv {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--env", key+"="+containerEnv[key])
	}

	args = append(args, r.cfg.Image, spec.Binary)
	args = append(args, spec.Args...)

	cmd := exec.CommandContext(ctx, r.dockerBinary, args...)
	cmd.Dir = spec.Workdir
	cmd.Env = DockerClientEnv(nil)
	return cmd, nil
}

func (r *dockerProcessRunner) networkArgs(containerEnv map[string]string) ([]string, error) {
	if r.cfg.NetworkPolicy == nil {
		if strings.TrimSpace(r.cfg.Network) == "" {
			return nil, nil
		}
		return []string{"--network", r.cfg.Network}, nil
	}

	switch config.NormalizeDockerNetworkPolicyMode(r.cfg.NetworkPolicy.Mode) {
	case config.DockerNetworkPolicyNone:
		return []string{"--network", config.DockerNetworkPolicyNone}, nil
	case config.DockerNetworkPolicyBridge:
		return []string{"--network", config.DockerNetworkPolicyBridge}, nil
	case config.DockerNetworkPolicyAllowlist:
		proxy, err := ensureDockerAllowlistProxy(r.cfg.NetworkPolicy.Allow)
		if err != nil {
			return nil, fmt.Errorf("start docker allowlist proxy: %w", err)
		}
		proxyURL := proxy.containerURL()
		containerEnv["HTTP_PROXY"] = proxyURL
		containerEnv["HTTPS_PROXY"] = proxyURL
		containerEnv["ALL_PROXY"] = proxyURL
		containerEnv["http_proxy"] = proxyURL
		containerEnv["https_proxy"] = proxyURL
		containerEnv["all_proxy"] = proxyURL
		containerEnv["NO_PROXY"] = appendNoProxy(containerEnv["NO_PROXY"], "127.0.0.1", "localhost", "::1")
		containerEnv["no_proxy"] = appendNoProxy(containerEnv["no_proxy"], "127.0.0.1", "localhost", "::1")
		return []string{
			"--network", config.DockerNetworkPolicyBridge,
			"--add-host", "host.docker.internal:host-gateway",
		}, nil
	default:
		if strings.TrimSpace(r.cfg.Network) == "" {
			return nil, nil
		}
		return []string{"--network", r.cfg.Network}, nil
	}
}

func (r *dockerProcessRunner) securityArgs() []string {
	if r.cfg.Security == nil {
		return nil
	}
	var args []string
	if r.cfg.Security.NoNewPrivileges == nil || *r.cfg.Security.NoNewPrivileges {
		args = append(args, "--security-opt", "no-new-privileges")
	}
	if r.cfg.Security.ReadOnlyRootFS != nil && *r.cfg.Security.ReadOnlyRootFS {
		args = append(args, "--read-only")
	}
	for _, capName := range r.cfg.Security.DropCapabilities {
		capName = strings.TrimSpace(capName)
		if capName == "" {
			continue
		}
		args = append(args, "--cap-drop", capName)
	}
	for _, tmpfs := range r.cfg.Security.Tmpfs {
		tmpfs = strings.TrimSpace(tmpfs)
		if tmpfs == "" {
			continue
		}
		args = append(args, "--tmpfs", tmpfs)
	}
	return args
}

func (r *dockerProcessRunner) containerEnv(explicit map[string]string) (map[string]string, error) {
	merged := map[string]string{}
	for key, value := range explicit {
		merged[key] = value
	}
	for _, rawKey := range r.cfg.EnvPassthrough {
		key := strings.TrimPrefix(strings.TrimSpace(rawKey), "$")
		if key == "" {
			continue
		}
		if _, ok := merged[key]; ok {
			continue
		}
		value, ok := os.LookupEnv(key)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("docker env_passthrough %q is unset or empty", key)
		}
		merged[key] = value
	}
	if r.cfg.Secrets != nil {
		for _, item := range r.cfg.Secrets.Env {
			source, target, _, err := config.ResolveDockerSecretEnvConfig(item)
			if err != nil {
				return nil, fmt.Errorf("docker secrets env: %w", err)
			}
			if _, ok := merged[target]; ok {
				continue
			}
			value, ok := os.LookupEnv(source)
			if !ok || strings.TrimSpace(value) == "" {
				return nil, fmt.Errorf("docker secrets env source %q is unset or empty", source)
			}
			merged[target] = value
		}
	}
	if strings.TrimSpace(merged["HOME"]) == "" {
		merged["HOME"] = defaultDockerHome
	}
	return merged, nil
}

func (r *dockerProcessRunner) prepareWritableHome(homeTarget string) (string, error) {
	homeSource, err := os.MkdirTemp("", "maestro-docker-home-*")
	if err != nil {
		return "", fmt.Errorf("create docker home dir: %w", err)
	}
	return homeSource, nil
}

func (r *dockerProcessRunner) applyAuthConfig(containerEnv map[string]string, homeSource string, homeTarget string, binary string) ([]config.DockerMountConfig, error) {
	var mounts []config.DockerMountConfig
	auth := r.cfg.Auth
	if auth == nil {
		if filepath.Base(binary) == "codex" {
			if apiKey, ok := containerEnv["OPENAI_API_KEY"]; ok && strings.TrimSpace(apiKey) != "" {
				if err := writeCodexAuthFile(homeSource, apiKey); err != nil {
					return nil, err
				}
			}
		}
		return nil, nil
	}

	mode := config.NormalizeDockerAuthMode(auth.Mode)
	switch mode {
	case config.DockerAuthClaudeAPIKey, config.DockerAuthClaudeProxy, config.DockerAuthCodexAPIKey:
		source := strings.TrimSpace(auth.Source)
		if source == "" {
			source = config.DockerAuthDefaultSource(mode)
		}
		target := strings.TrimSpace(auth.Target)
		if target == "" {
			target = config.DockerAuthDefaultTarget(mode, homeTarget)
		}
		value, ok := os.LookupEnv(source)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("docker auth source env %q is unset or empty", source)
		}
		containerEnv[target] = value
		if mode == config.DockerAuthCodexAPIKey {
			if err := writeCodexAuthFile(homeSource, value); err != nil {
				return nil, err
			}
		}
	case config.DockerAuthClaudeConfig, config.DockerAuthCodexConfig:
		source := strings.TrimSpace(auth.Source)
		if source == "" {
			return nil, fmt.Errorf("docker auth source path is required for mode %s", mode)
		}
		target := strings.TrimSpace(auth.Target)
		if target == "" {
			target = config.DockerAuthDefaultTarget(mode, homeTarget)
		}
		if err := requireExistingPath(source); err != nil {
			return nil, fmt.Errorf("docker auth mount %q: %w", source, err)
		}
		mounts = append(mounts, config.DockerMountConfig{
			Source:   source,
			Target:   target,
			ReadOnly: true,
		})
	default:
		return nil, fmt.Errorf("unsupported docker auth mode %q", auth.Mode)
	}
	return mounts, nil
}

func (r *dockerProcessRunner) accessMounts(homeTarget string) ([]config.DockerMountConfig, error) {
	var mounts []config.DockerMountConfig
	if r.cfg.Secrets != nil {
		for _, item := range r.cfg.Secrets.Mounts {
			source, target, _, err := config.ResolveDockerAccessMountConfig(item, homeTarget)
			if err != nil {
				return nil, fmt.Errorf("docker secrets mount: %w", err)
			}
			if err := requireExistingPath(source); err != nil {
				return nil, fmt.Errorf("docker secrets mount %q: %w", source, err)
			}
			mounts = append(mounts, config.DockerMountConfig{
				Source:   source,
				Target:   target,
				ReadOnly: true,
			})
		}
	}
	if r.cfg.Tools != nil {
		for _, item := range r.cfg.Tools.Mounts {
			source, target, _, err := config.ResolveDockerAccessMountConfig(item, homeTarget)
			if err != nil {
				return nil, fmt.Errorf("docker tools mount: %w", err)
			}
			if err := requireExistingPath(source); err != nil {
				return nil, fmt.Errorf("docker tools mount %q: %w", source, err)
			}
			mounts = append(mounts, config.DockerMountConfig{
				Source:   source,
				Target:   target,
				ReadOnly: true,
			})
		}
	}
	return mounts, nil
}

func (r *dockerProcessRunner) cacheMounts(homeTarget string) ([]config.DockerMountConfig, error) {
	var mounts []config.DockerMountConfig
	cache := r.cfg.Cache
	if cache == nil {
		return nil, nil
	}
	hostRoot := dockerCacheRoot()
	if err := os.MkdirAll(hostRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create docker cache root: %w", err)
	}
	for _, profile := range cache.Profiles {
		normalized := config.NormalizeDockerCacheProfile(profile)
		targets := config.DockerCacheProfileTargets(normalized, homeTarget)
		if len(targets) == 0 {
			return nil, fmt.Errorf("unsupported docker cache profile %q", profile)
		}
		for _, target := range targets {
			source := filepath.Join(hostRoot, normalized, strings.TrimPrefix(target, homeTarget+"/"))
			if err := os.MkdirAll(source, 0o755); err != nil {
				return nil, fmt.Errorf("create docker cache dir %q: %w", source, err)
			}
			mounts = append(mounts, config.DockerMountConfig{
				Source: source,
				Target: target,
			})
		}
	}
	for _, mount := range cache.Mounts {
		if strings.TrimSpace(mount.Source) == "" {
			return nil, fmt.Errorf("docker cache mount source is required")
		}
		if err := os.MkdirAll(mount.Source, 0o755); err != nil {
			return nil, fmt.Errorf("create docker cache dir %q: %w", mount.Source, err)
		}
		mounts = append(mounts, config.DockerMountConfig{
			Source: mount.Source,
			Target: mount.Target,
		})
	}
	return mounts, nil
}

func dockerCacheRoot() string {
	root, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(root) == "" {
		root = os.TempDir()
	}
	return filepath.Join(root, "maestro", "docker-cache")
}

func bindMountArg(source string, target string, readOnly bool) string {
	arg := fmt.Sprintf("type=bind,src=%s,dst=%s", filepath.Clean(source), target)
	if readOnly {
		arg += ",readonly"
	}
	return arg
}

func requireExistingPath(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return nil
}

func writeCodexAuthFile(homeDir string, apiKey string) error {
	authDir := filepath.Join(homeDir, ".codex")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		return fmt.Errorf("create codex auth dir: %w", err)
	}
	body, err := json.MarshalIndent(map[string]string{
		"auth_mode":      "apikey",
		"OPENAI_API_KEY": apiKey,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal codex auth: %w", err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "auth.json"), append(body, '\n'), 0o600); err != nil {
		return fmt.Errorf("write codex auth: %w", err)
	}
	return nil
}

func writeCodexAuthHome(apiKey string) (string, error) {
	homeDir, err := os.MkdirTemp("", "maestro-codex-home-*")
	if err != nil {
		return "", fmt.Errorf("create codex auth home: %w", err)
	}
	if err := writeCodexAuthFile(homeDir, apiKey); err != nil {
		return "", err
	}
	return homeDir, nil
}

func envLookup(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix), true
		}
	}
	return "", false
}
