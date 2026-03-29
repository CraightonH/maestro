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

const defaultDockerHome = "/tmp/maestro-home"

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

	if strings.TrimSpace(spec.Workdir) != "" {
		if err := requireExistingPath(spec.Workdir); err != nil {
			return nil, fmt.Errorf("docker workspace mount %q: %w", spec.Workdir, err)
		}
		args = append(args,
			"--mount", bindMountArg(spec.Workdir, r.cfg.WorkspaceMountPath, false),
			"--workdir", r.cfg.WorkspaceMountPath,
		)
	}

	for _, mount := range r.cfg.Mounts {
		if err := requireExistingPath(mount.Source); err != nil {
			return nil, fmt.Errorf("docker mount %q: %w", mount.Source, err)
		}
		args = append(args, "--mount", bindMountArg(mount.Source, mount.Target, mount.ReadOnly))
	}

	if strings.TrimSpace(r.cfg.Network) != "" {
		args = append(args, "--network", r.cfg.Network)
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

	envVars, err := r.containerEnv(spec.Env)
	if err != nil {
		return nil, err
	}
	if extraMount, err := r.syntheticAuthMount(spec, envVars); err != nil {
		return nil, err
	} else if extraMount != nil {
		args = append(args, "--mount", bindMountArg(extraMount.Source, extraMount.Target, extraMount.ReadOnly))
	}
	for _, entry := range envVars {
		args = append(args, "--env", entry)
	}

	args = append(args, r.cfg.Image, spec.Binary)
	args = append(args, spec.Args...)

	cmd := exec.CommandContext(ctx, r.dockerBinary, args...)
	cmd.Dir = spec.Workdir
	cmd.Env = DockerClientEnv(nil)
	return cmd, nil
}

func (r *dockerProcessRunner) syntheticAuthMount(spec ProcessSpec, envVars []string) (*config.DockerMountConfig, error) {
	if filepath.Base(spec.Binary) != "codex" {
		return nil, nil
	}

	apiKey, ok := envLookup(envVars, "OPENAI_API_KEY")
	if !ok || strings.TrimSpace(apiKey) == "" {
		return nil, nil
	}

	home, ok := envLookup(envVars, "HOME")
	if !ok || strings.TrimSpace(home) == "" {
		home = defaultDockerHome
	}
	for _, mount := range r.cfg.Mounts {
		if filepath.Clean(mount.Target) == filepath.Clean(home) {
			return nil, nil
		}
	}

	source, err := writeCodexAuthHome(apiKey)
	if err != nil {
		return nil, err
	}
	return &config.DockerMountConfig{
		Source: source,
		Target: home,
	}, nil
}

func (r *dockerProcessRunner) containerEnv(explicit map[string]string) ([]string, error) {
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
	if strings.TrimSpace(merged["HOME"]) == "" {
		merged["HOME"] = defaultDockerHome
	}

	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+merged[key])
	}
	return env, nil
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

func writeCodexAuthHome(apiKey string) (string, error) {
	homeDir, err := os.MkdirTemp("", "maestro-codex-home-*")
	if err != nil {
		return "", fmt.Errorf("create codex auth home: %w", err)
	}
	authDir := filepath.Join(homeDir, ".codex")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		return "", fmt.Errorf("create codex auth dir: %w", err)
	}
	body, err := json.MarshalIndent(map[string]string{
		"auth_mode":      "apikey",
		"OPENAI_API_KEY": apiKey,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal codex auth: %w", err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "auth.json"), append(body, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("write codex auth: %w", err)
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
