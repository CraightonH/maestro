package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	RunID      string
	LineageKey string
	Binary     string
	Args       []string
	Workdir    string
	Env        map[string]string
	Lifecycle  *ProcessLifecycle
}

type ProcessRunner interface {
	Kind() string
	ResolveBinary(name string) (string, error)
	VisibleWorkdir(hostPath string) string
	CommandContext(ctx context.Context, spec ProcessSpec) (*exec.Cmd, error)
}

type ProcessRunnerOption func(*processRunnerOptions)

type processRunnerOptions struct {
	dockerReuse *DockerReuseManager
}

func WithDockerReuseManager(manager *DockerReuseManager) ProcessRunnerOption {
	return func(opts *processRunnerOptions) {
		opts.dockerReuse = manager
	}
}

func NewProcessRunner(docker *config.DockerConfig, options ...ProcessRunnerOption) (ProcessRunner, error) {
	opts := processRunnerOptions{}
	for _, option := range options {
		option(&opts)
	}
	resolved := config.ResolveDockerConfig(nil, docker)
	if strings.TrimSpace(resolved.Image) == "" {
		return localProcessRunner{}, nil
	}
	return newDockerProcessRunner(resolved, opts)
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
	if spec.Lifecycle != nil {
		spec.Lifecycle.Metadata = ExecutionMetadata{Mode: "host"}
	}
	cmd := exec.CommandContext(ctx, spec.Binary, spec.Args...)
	cmd.Dir = spec.Workdir
	cmd.Env = MergeEnv(spec.Env)
	return cmd, nil
}

type dockerProcessRunner struct {
	dockerBinary string
	cfg          config.DockerConfig
	reuse        *DockerReuseManager
}

type dockerPreparedSpec struct {
	containerEnv map[string]string
	networkArgs  []string
	homeTarget   string
	homeSource   string
	mounts       []config.DockerMountConfig
	execWorkdir  string
	profileKey   string
	reuseMode    string
	lineageKey   string
}

func newDockerProcessRunner(cfg config.DockerConfig, opts processRunnerOptions) (ProcessRunner, error) {
	dockerBinary, err := exec.LookPath("docker")
	if err != nil {
		return nil, fmt.Errorf("find docker executable: %w", err)
	}
	return &dockerProcessRunner{
		dockerBinary: dockerBinary,
		cfg:          cfg,
		reuse:        opts.dockerReuse,
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
	prepared, err := r.prepareSpec(spec)
	if err != nil {
		return nil, err
	}

	if prepared.reuseMode != config.DockerReuseModeNone {
		reusablePrepared, err := r.finalizePreparedSpec(spec, prepared, true)
		if err != nil {
			return nil, err
		}
		if lease, err := r.acquireReusableContainer(ctx, spec, reusablePrepared); err == nil && lease != nil {
			args, err := r.reusableExecArgs(ctx, lease, spec, reusablePrepared)
			if err == nil {
				if spec.Lifecycle != nil {
					spec.Lifecycle.Metadata = ExecutionMetadata{
						Mode: "docker",
						ContainerReuse: &ContainerReuseMetadata{
							Mode:          reusablePrepared.reuseMode,
							Reused:        true,
							ContainerID:   lease.containerID,
							ContainerName: lease.containerName,
							ProfileKey:    reusablePrepared.profileKey,
							LineageKey:    reusablePrepared.lineageKey,
						},
					}
					spec.Lifecycle.Release = func(ctx context.Context, _ error) error {
						return r.reuse.Release(ctx, lease)
					}
				}
				cmd := exec.CommandContext(ctx, r.dockerBinary, args...)
				cmd.Dir = spec.Workdir
				cmd.Env = DockerClientEnv(nil)
				return cmd, nil
			}
			_ = r.reuse.Release(context.Background(), lease)
		}
	}

	coldPrepared, err := r.finalizePreparedSpec(spec, prepared, false)
	if err != nil {
		return nil, err
	}

	args := []string{"run", "--rm", "-i"}
	args = append(args, r.baseContainerArgs(coldPrepared, true)...)
	if coldPrepared.execWorkdir != "" {
		args = append(args, "--workdir", coldPrepared.execWorkdir)
	}
	appendSortedEnvArgs(&args, coldPrepared.containerEnv)
	args = append(args, r.cfg.Image, spec.Binary)
	args = append(args, spec.Args...)

	if spec.Lifecycle != nil {
		spec.Lifecycle.Metadata = ExecutionMetadata{
			Mode: "docker",
			ContainerReuse: &ContainerReuseMetadata{
				Mode:       prepared.reuseMode,
				Reused:     false,
				ProfileKey: prepared.profileKey,
				LineageKey: prepared.lineageKey,
			},
		}
	}

	cmd := exec.CommandContext(ctx, r.dockerBinary, args...)
	cmd.Dir = spec.Workdir
	cmd.Env = DockerClientEnv(nil)
	return cmd, nil
}

func (r *dockerProcessRunner) finalizePreparedSpec(spec ProcessSpec, prepared *dockerPreparedSpec, useReuseHome bool) (*dockerPreparedSpec, error) {
	finalized := *prepared
	finalized.containerEnv = cloneStringMap(prepared.containerEnv)

	var err error
	if useReuseHome {
		finalized.homeSource, err = r.prepareHomeSource(finalized.homeTarget, finalized.reuseMode, finalized.profileKey, finalized.lineageKey)
	} else {
		finalized.homeSource, err = r.prepareWritableHome(finalized.homeTarget)
	}
	if err != nil {
		return nil, err
	}

	mounts, execWorkdir, err := r.runtimeMounts(spec, finalized.homeTarget, finalized.homeSource)
	if err != nil {
		return nil, err
	}
	authMounts, err := r.applyAuthConfig(finalized.containerEnv, finalized.homeSource, finalized.homeTarget, spec.Binary)
	if err != nil {
		return nil, err
	}
	mounts = append(mounts, authMounts...)
	cacheMounts, err := r.cacheMounts(finalized.homeTarget)
	if err != nil {
		return nil, err
	}
	mounts = append(mounts, cacheMounts...)

	finalized.mounts = mounts
	finalized.execWorkdir = execWorkdir
	return &finalized, nil
}

func cloneStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	cloned := make(map[string]string, len(src))
	for key, value := range src {
		cloned[key] = value
	}
	return cloned
}

func (r *dockerProcessRunner) prepareSpec(spec ProcessSpec) (*dockerPreparedSpec, error) {
	prepared := &dockerPreparedSpec{
		reuseMode: config.DockerReuseModeNone,
	}
	if r.cfg.Reuse != nil {
		prepared.reuseMode = config.NormalizeDockerReuseMode(r.cfg.Reuse.Mode)
		if prepared.reuseMode == "" {
			prepared.reuseMode = config.DockerReuseModeNone
		}
	}

	containerEnv, err := r.containerEnv(spec.Env)
	if err != nil {
		return nil, err
	}
	networkArgs, err := r.networkArgs(containerEnv)
	if err != nil {
		return nil, err
	}
	homeTarget := strings.TrimSpace(containerEnv["HOME"])
	if homeTarget == "" {
		homeTarget = defaultDockerHome
		containerEnv["HOME"] = homeTarget
	}

	profileKey, err := r.profileKey(spec, homeTarget)
	if err != nil {
		return nil, err
	}
	prepared.profileKey = profileKey
	if prepared.reuseMode == config.DockerReuseModeLineage {
		prepared.lineageKey = strings.TrimSpace(spec.LineageKey)
	}

	prepared.containerEnv = containerEnv
	prepared.networkArgs = networkArgs
	prepared.homeTarget = homeTarget
	return prepared, nil
}

func (r *dockerProcessRunner) runtimeMounts(spec ProcessSpec, homeTarget string, homeSource string) ([]config.DockerMountConfig, string, error) {
	mounts := []config.DockerMountConfig{{
		Source: homeSource,
		Target: homeTarget,
	}}
	execWorkdir := ""
	if strings.TrimSpace(spec.Workdir) != "" {
		if err := requireExistingPath(spec.Workdir); err != nil {
			return nil, "", fmt.Errorf("docker workspace mount %q: %w", spec.Workdir, err)
		}
		mounts = append(mounts, config.DockerMountConfig{
			Source: spec.Workdir,
			Target: r.cfg.WorkspaceMountPath,
		})
		execWorkdir = r.cfg.WorkspaceMountPath
	}
	for _, mount := range r.cfg.Mounts {
		if err := requireExistingPath(mount.Source); err != nil {
			return nil, "", fmt.Errorf("docker mount %q: %w", mount.Source, err)
		}
		mounts = append(mounts, mount)
	}
	accessMounts, err := r.accessMounts(homeTarget)
	if err != nil {
		return nil, "", err
	}
	mounts = append(mounts, accessMounts...)
	return mounts, execWorkdir, nil
}

func (r *dockerProcessRunner) acquireReusableContainer(ctx context.Context, spec ProcessSpec, prepared *dockerPreparedSpec) (*dockerReusableLease, error) {
	if r.reuse == nil || prepared == nil || prepared.reuseMode == config.DockerReuseModeNone {
		return nil, fmt.Errorf("reuse disabled")
	}
	if prepared.reuseMode == config.DockerReuseModeLineage && strings.TrimSpace(prepared.lineageKey) == "" {
		return nil, fmt.Errorf("missing lineage key")
	}
	createArgs := []string{"create", "--name", reusableContainerName(prepared.reuseMode, prepared.profileKey, prepared.lineageKey, r.reuse.ownerPID)}
	createArgs = append(createArgs, "--label", dockerReuseManagedLabel, "--label", dockerReuseOwnerLabel+"="+strconv.Itoa(r.reuse.ownerPID))
	createArgs = append(createArgs, r.baseContainerArgs(prepared, false)...)
	createArgs = append(createArgs, "--entrypoint", "sh", r.cfg.Image, "-lc", "trap 'exit 0' TERM INT; while :; do sleep 3600; done")
	return r.reuse.Acquire(ctx, prepared.reuseMode, prepared.profileKey, prepared.lineageKey, createArgs)
}

func (r *dockerProcessRunner) reusableExecArgs(ctx context.Context, lease *dockerReusableLease, spec ProcessSpec, prepared *dockerPreparedSpec) ([]string, error) {
	execWorkdir := prepared.execWorkdir
	if prepared.reuseMode == config.DockerReuseModeStateless {
		if err := r.runReusableReset(ctx, lease.containerName, spec.RunID); err != nil {
			return nil, err
		}
		if execWorkdir == "" {
			execWorkdir = filepath.ToSlash(filepath.Join("/tmp/maestro-runs", safeDockerPathSegment(spec.RunID)))
		}
	}

	args := []string{"exec", "-i"}
	if execWorkdir != "" {
		args = append(args, "--workdir", execWorkdir)
	}
	appendSortedEnvArgs(&args, prepared.containerEnv)
	args = append(args, lease.containerName, spec.Binary)
	args = append(args, spec.Args...)
	return args, nil
}

func (r *dockerProcessRunner) runReusableReset(ctx context.Context, containerName string, runID string) error {
	runDir := filepath.ToSlash(filepath.Join("/tmp/maestro-runs", safeDockerPathSegment(runID)))
	cmd := exec.CommandContext(ctx, r.dockerBinary,
		"exec", containerName, "sh", "-lc",
		fmt.Sprintf("rm -rf /tmp/maestro-runs && mkdir -p %s", runDir),
	)
	cmd.Env = DockerClientEnv(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("reset reusable container %s: %w output=%s", containerName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (r *dockerProcessRunner) baseContainerArgs(prepared *dockerPreparedSpec, includePull bool) []string {
	args := []string{}
	if runtime.GOOS != "windows" {
		args = append(args, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()))
	}
	if includePull {
		if policy := strings.TrimSpace(r.cfg.PullPolicy); policy != "" {
			args = append(args, "--pull", policy)
		}
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
	args = append(args, prepared.networkArgs...)
	for _, mount := range prepared.mounts {
		args = append(args, "--mount", bindMountArg(mount.Source, mount.Target, mount.ReadOnly))
	}
	return args
}

func appendSortedEnvArgs(args *[]string, env map[string]string) {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		*args = append(*args, "--env", key+"="+env[key])
	}
}

func (r *dockerProcessRunner) prepareHomeSource(homeTarget string, reuseMode string, profileKey string, lineageKey string) (string, error) {
	if reuseMode == config.DockerReuseModeNone {
		return r.prepareWritableHome(homeTarget)
	}
	root, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(root) == "" {
		root = os.TempDir()
	}
	key := profileKey
	if reuseMode == config.DockerReuseModeLineage && strings.TrimSpace(lineageKey) != "" {
		key += "-" + lineageKey
	}
	path := filepath.Join(root, "maestro", "docker-reuse", safeDockerPathSegment(key), "home")
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", fmt.Errorf("create docker reusable home dir: %w", err)
	}
	return path, nil
}

func (r *dockerProcessRunner) profileKey(spec ProcessSpec, homeTarget string) (string, error) {
	type profile struct {
		Image           string                            `json:"image"`
		Binary          string                            `json:"binary"`
		User            string                            `json:"user,omitempty"`
		PullPolicy      string                            `json:"pull_policy,omitempty"`
		WorkspaceSource string                            `json:"workspace_source,omitempty"`
		WorkspaceTarget string                            `json:"workspace_target,omitempty"`
		Network         string                            `json:"network,omitempty"`
		NetworkPolicy   *config.DockerNetworkPolicyConfig `json:"network_policy,omitempty"`
		CPUs            float64                           `json:"cpus,omitempty"`
		Memory          string                            `json:"memory,omitempty"`
		PIDsLimit       int                               `json:"pids_limit,omitempty"`
		ImagePinMode    string                            `json:"image_pin_mode,omitempty"`
		Env             []config.DockerSecretEnvConfig    `json:"env,omitempty"`
		Mounts          []config.DockerMountConfig        `json:"mounts,omitempty"`
		Secrets         *config.DockerSecretsConfig       `json:"secrets,omitempty"`
		Tools           *config.DockerToolsConfig         `json:"tools,omitempty"`
		Auth            *config.DockerAuthConfig          `json:"auth,omitempty"`
		Security        *config.DockerSecurityConfig      `json:"security,omitempty"`
		Cache           *config.DockerCacheConfig         `json:"cache,omitempty"`
		HomeTarget      string                            `json:"home_target,omitempty"`
	}

	mounts := append([]config.DockerMountConfig{}, r.cfg.Mounts...)
	if strings.TrimSpace(spec.Workdir) != "" {
		mounts = append(mounts, config.DockerMountConfig{
			Source: spec.Workdir,
			Target: r.cfg.WorkspaceMountPath,
		})
	}
	sort.SliceStable(mounts, func(i, j int) bool {
		left := mounts[i].Source + "|" + mounts[i].Target
		right := mounts[j].Source + "|" + mounts[j].Target
		return left < right
	})

	value := profile{
		Image:           r.cfg.Image,
		Binary:          spec.Binary,
		PullPolicy:      strings.TrimSpace(r.cfg.PullPolicy),
		WorkspaceSource: spec.Workdir,
		WorkspaceTarget: r.cfg.WorkspaceMountPath,
		Network:         config.EffectiveDockerNetwork(&r.cfg),
		NetworkPolicy:   r.cfg.NetworkPolicy,
		CPUs:            r.cfg.CPUs,
		Memory:          strings.TrimSpace(r.cfg.Memory),
		PIDsLimit:       r.cfg.PIDsLimit,
		ImagePinMode:    strings.TrimSpace(r.cfg.ImagePinMode),
		Mounts:          mounts,
		Secrets:         r.cfg.Secrets,
		Tools:           r.cfg.Tools,
		Auth:            r.cfg.Auth,
		Security:        r.cfg.Security,
		Cache:           r.cfg.Cache,
		HomeTarget:      homeTarget,
	}
	if runtime.GOOS != "windows" {
		value.User = fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	}
	body, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal docker profile: %w", err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func safeDockerPathSegment(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
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
