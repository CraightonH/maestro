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
	t.Setenv("OPENAI_API_KEY", "host-token")
	t.Setenv("DOCKER_HOST", "unix:///tmp/colima.sock")
	t.Setenv("DOCKER_CONTEXT", "colima")

	authDir := filepath.Join(tmp, "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	workdir := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runner, err := NewProcessRunner(&config.DockerConfig{
		Image:              "maestro-agent:latest",
		WorkspaceMountPath: "/workspace",
		Mounts: []config.DockerMountConfig{{
			Source:   authDir,
			Target:   "/root/.codex",
			ReadOnly: true,
		}},
		EnvPassthrough: []string{"OPENAI_API_KEY"},
		Network:        "none",
		CPUs:           1.5,
		Memory:         "2g",
		PIDsLimit:      128,
	})
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}

	cmd, err := runner.CommandContext(context.Background(), ProcessSpec{
		Binary:  "codex",
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
	if !strings.Contains(args, "--mount\ntype=bind,src="+authDir+",dst=/root/.codex,readonly") {
		t.Fatalf("args = %q, want readonly auth mount", args)
	}
	if !strings.Contains(args, "--workdir\n/workspace") {
		t.Fatalf("args = %q, want workdir /workspace", args)
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
	if !strings.Contains(args, "--env\nEXPLICIT=1") {
		t.Fatalf("args = %q, want explicit env", args)
	}
	if !strings.Contains(args, "--env\nOPENAI_API_KEY=host-token") {
		t.Fatalf("args = %q, want env passthrough", args)
	}
	if !strings.Contains(args, "--env\nHOME="+defaultDockerHome) {
		t.Fatalf("args = %q, want default docker HOME", args)
	}
	if !strings.Contains(args, "--mount\ntype=bind,src=") || !strings.Contains(args, ",dst="+defaultDockerHome) {
		t.Fatalf("args = %q, want synthetic codex auth home mount", args)
	}
	if !strings.Contains(args, "maestro-agent:latest\ncodex\napp-server") {
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
