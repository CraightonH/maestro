package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tjohnson/maestro/internal/config"
)

func TestNewProcessRunnerDefaultsToHost(t *testing.T) {
	runner, err := NewProcessRunner(nil)
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}
	if got := runner.Kind(); got != "host" {
		t.Fatalf("runner kind = %q, want host", got)
	}
}

func TestNewProcessRunnerRequiresDockerBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := NewProcessRunner(&config.DockerConfig{Image: "maestro-agent:latest"})
	if err == nil || !strings.Contains(err.Error(), "find docker executable") {
		t.Fatalf("new process runner error = %v, want docker lookup failure", err)
	}
}

func TestDockerProcessRunnerBuildsContainerCommand(t *testing.T) {
	tmp := t.TempDir()
	dockerBinary := filepath.Join(tmp, "docker")
	if err := os.WriteFile(dockerBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write docker stub: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANTHROPIC_API_KEY", "claude-token")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "proxy-token")
	t.Setenv("ANTHROPIC_BASE_URL", "https://proxy.example.invalid")
	t.Setenv("DOCKER_HOST", "unix:///tmp/colima.sock")
	t.Setenv("DOCKER_CONTEXT", "colima")

	authDir := filepath.Join(tmp, "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	cacheDir := filepath.Join(tmp, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	workdir := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runner, err := NewProcessRunner(&config.DockerConfig{
		Image:              "maestro-agent:latest",
		WorkspaceMountPath: "/workspace",
		PullPolicy:         "always",
		Mounts: []config.DockerMountConfig{{
			Source:   authDir,
			Target:   "/tmp/maestro-home/.claude",
			ReadOnly: true,
		}},
		EnvPassthrough: []string{"ANTHROPIC_BASE_URL"},
		Network:        "none",
		CPUs:           1.5,
		Memory:         "2g",
		PIDsLimit:      128,
		Auth: &config.DockerAuthConfig{
			Mode: config.DockerAuthClaudeProxy,
		},
		Cache: &config.DockerCacheConfig{
			Profiles: []string{config.DockerCacheProfileGo},
			Mounts: []config.DockerCacheMountConfig{{
				Source: filepath.Join(cacheDir, "custom"),
				Target: "/cache/custom",
			}},
		},
	})
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}

	cmd, err := runner.CommandContext(context.Background(), ProcessSpec{
		Binary:  "claude",
		Args:    []string{"app-server", "--listen", "stdio://"},
		Workdir: workdir,
		Env: map[string]string{
			"EXPLICIT": "1",
		},
	})
	if err != nil {
		t.Fatalf("command context: %v", err)
	}

	args := strings.Join(cmd.Args, "\n")
	if !strings.Contains(args, dockerBinary) {
		t.Fatalf("args = %q, want docker binary path", args)
	}
	if !strings.Contains(args, "--mount\ntype=bind,src="+workdir+",dst=/workspace") {
		t.Fatalf("args = %q, want workspace mount", args)
	}
	if !strings.Contains(args, "--mount\ntype=bind,src="+authDir+",dst=/tmp/maestro-home/.claude,readonly") {
		t.Fatalf("args = %q, want readonly auth mount", args)
	}
	if !strings.Contains(args, "--workdir\n/workspace") {
		t.Fatalf("args = %q, want workdir /workspace", args)
	}
	if !strings.Contains(args, "--pull\nalways") {
		t.Fatalf("args = %q, want pull policy", args)
	}
	if !strings.Contains(args, "--network\nnone") {
		t.Fatalf("args = %q, want network none", args)
	}
	if !strings.Contains(args, "--cpus\n1.5") {
		t.Fatalf("args = %q, want cpu limit", args)
	}
	if !strings.Contains(args, "--memory\n2g") {
		t.Fatalf("args = %q, want memory limit", args)
	}
	if !strings.Contains(args, "--pids-limit\n128") {
		t.Fatalf("args = %q, want pid limit", args)
	}
	if !strings.Contains(args, "--security-opt\nno-new-privileges") {
		t.Fatalf("args = %q, want no-new-privileges security opt", args)
	}
	if !strings.Contains(args, "--read-only") {
		t.Fatalf("args = %q, want read-only rootfs", args)
	}
	if !strings.Contains(args, "--cap-drop\nALL") {
		t.Fatalf("args = %q, want cap drop", args)
	}
	if !strings.Contains(args, "--tmpfs\n/tmp") {
		t.Fatalf("args = %q, want tmpfs /tmp", args)
	}
	if !strings.Contains(args, "--env\nEXPLICIT=1") {
		t.Fatalf("args = %q, want explicit env", args)
	}
	if !strings.Contains(args, "--env\nANTHROPIC_BASE_URL=https://proxy.example.invalid") {
		t.Fatalf("args = %q, want env passthrough", args)
	}
	if !strings.Contains(args, "--env\nHOME="+defaultDockerHome) {
		t.Fatalf("args = %q, want default docker HOME", args)
	}
	if !strings.Contains(args, "--env\nANTHROPIC_AUTH_TOKEN=proxy-token") {
		t.Fatalf("args = %q, want auth env", args)
	}
	if !strings.Contains(args, "--mount\ntype=bind,src=") || !strings.Contains(args, ",dst="+defaultDockerHome) {
		t.Fatalf("args = %q, want writable docker home mount", args)
	}
	if !strings.Contains(args, "--mount\ntype=bind,src=") || !strings.Contains(args, ",dst="+filepath.Join(defaultDockerHome, ".cache", "go-build")) {
		t.Fatalf("args = %q, want go cache profile mount", args)
	}
	if !strings.Contains(args, "--mount\ntype=bind,src="+filepath.Join(cacheDir, "custom")+",dst=/cache/custom") {
		t.Fatalf("args = %q, want explicit cache mount", args)
	}
	if !strings.Contains(args, "maestro-agent:latest\nclaude\napp-server") {
		t.Fatalf("args = %q, want image and binary invocation", args)
	}
	if got := runner.VisibleWorkdir(workdir); got != "/workspace" {
		t.Fatalf("visible workdir = %q, want /workspace", got)
	}
	env := strings.Join(cmd.Env, "\n")
	if !strings.Contains(env, "DOCKER_HOST=unix:///tmp/colima.sock") {
		t.Fatalf("docker client env = %q, want DOCKER_HOST", env)
	}
	if !strings.Contains(env, "DOCKER_CONTEXT=colima") {
		t.Fatalf("docker client env = %q, want DOCKER_CONTEXT", env)
	}
}

func TestDockerProcessRunnerCodexApiKeyWritesAuthHome(t *testing.T) {
	tmp := t.TempDir()
	dockerBinary := filepath.Join(tmp, "docker")
	if err := os.WriteFile(dockerBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write docker stub: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OPENAI_API_KEY", "host-token")

	workdir := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runner, err := NewProcessRunner(&config.DockerConfig{
		Image:      "maestro-agent:latest",
		Auth:       &config.DockerAuthConfig{Mode: config.DockerAuthCodexAPIKey},
		Cache:      &config.DockerCacheConfig{Profiles: []string{config.DockerCacheProfileCodex}},
		PullPolicy: "never",
	})
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}

	cmd, err := runner.CommandContext(context.Background(), ProcessSpec{
		Binary:  "codex",
		Args:    []string{"app-server"},
		Workdir: workdir,
	})
	if err != nil {
		t.Fatalf("command context: %v", err)
	}

	args := cmd.Args
	if !containsArgPair(args, "--pull", "never") {
		t.Fatalf("args = %q, want pull never", strings.Join(args, "\n"))
	}
	if !containsArgPair(args, "--env", "OPENAI_API_KEY=host-token") {
		t.Fatalf("args = %q, want codex api key env", strings.Join(args, "\n"))
	}
	homeSource, ok := mountSourceForTarget(args, defaultDockerHome)
	if !ok {
		t.Fatalf("args = %q, want writable home mount", strings.Join(args, "\n"))
	}
	body, err := os.ReadFile(filepath.Join(homeSource, ".codex", "auth.json"))
	if err != nil {
		t.Fatalf("read codex auth file: %v", err)
	}
	if got := string(body); !strings.Contains(got, `"OPENAI_API_KEY": "host-token"`) {
		t.Fatalf("auth.json = %q, want codex api key", got)
	}
	if !strings.Contains(strings.Join(args, "\n"), ",dst="+filepath.Join(defaultDockerHome, ".codex", "cache")) {
		t.Fatalf("args = %q, want codex cache profile mount", strings.Join(args, "\n"))
	}
}

func TestDockerProcessRunnerCodexConfigMountUsesHomeDefaultTarget(t *testing.T) {
	tmp := t.TempDir()
	dockerBinary := filepath.Join(tmp, "docker")
	if err := os.WriteFile(dockerBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write docker stub: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	authDir := filepath.Join(tmp, "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	workdir := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runner, err := NewProcessRunner(&config.DockerConfig{
		Image: "maestro-agent:latest",
		Auth: &config.DockerAuthConfig{
			Mode:   config.DockerAuthCodexConfig,
			Source: authDir,
		},
	})
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}

	cmd, err := runner.CommandContext(context.Background(), ProcessSpec{
		Binary:  "codex",
		Args:    []string{"app-server"},
		Workdir: workdir,
	})
	if err != nil {
		t.Fatalf("command context: %v", err)
	}

	if !strings.Contains(strings.Join(cmd.Args, "\n"), "--mount\ntype=bind,src="+authDir+",dst="+filepath.Join(defaultDockerHome, ".codex")+",readonly") {
		t.Fatalf("args = %q, want codex config mount", strings.Join(cmd.Args, "\n"))
	}
}

func TestWriteCodexAuthHome(t *testing.T) {
	home, err := writeCodexAuthHome("sk-test")
	if err != nil {
		t.Fatalf("write codex auth home: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil {
		t.Fatalf("read auth file: %v", err)
	}
	if got := string(body); !strings.Contains(got, `"auth_mode": "apikey"`) || !strings.Contains(got, `"OPENAI_API_KEY": "sk-test"`) {
		t.Fatalf("auth.json = %q, want api auth payload", got)
	}
}

func containsArgPair(args []string, key, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func mountSourceForTarget(args []string, target string) (string, bool) {
	for i := 0; i < len(args)-1; i++ {
		if args[i] != "--mount" {
			continue
		}
		spec := args[i+1]
		if !strings.Contains(spec, ",dst="+target) {
			continue
		}
		for _, segment := range strings.Split(spec, ",") {
			if strings.HasPrefix(segment, "src=") {
				return strings.TrimPrefix(segment, "src="), true
			}
		}
	}
	return "", false
}
