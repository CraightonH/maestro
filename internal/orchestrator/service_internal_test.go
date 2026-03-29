package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/harness"
)

func TestNewHarnessUsesDockerRunnerWhenConfigured(t *testing.T) {
	tmp := t.TempDir()
	dockerBinary := filepath.Join(tmp, "docker")
	if err := os.WriteFile(dockerBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write docker stub: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	runner, err := harness.NewProcessRunner(&config.DockerConfig{Image: "maestro-agent:latest"})
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}
	h, err := newHarness(config.AgentTypeConfig{
		Name:    "code-pr",
		Harness: "claude-code",
		Docker:  &config.DockerConfig{Image: "maestro-agent:latest"},
	}, runner)
	if err != nil {
		t.Fatalf("new harness: %v", err)
	}
	if got := h.Kind(); got != "claude-code" {
		t.Fatalf("harness kind = %q, want claude-code", got)
	}
}

func TestRunHookUsesContainerRunnerWhenConfigured(t *testing.T) {
	cfg := &config.Config{
		Hooks: config.HooksConfig{
			Timeout:   config.Duration{Duration: 5 * time.Second},
			Execution: "container",
		},
	}
	runner := &recordingRunner{kind: "docker"}
	svc := &Service{
		cfg:           cfg,
		processRunner: runner,
	}
	run := &domain.AgentRun{
		ID:            "run-1",
		Issue:         domain.Issue{ID: "issue-1", Identifier: "OPS-1"},
		AgentName:     "dev-claude",
		AgentType:     "dev-claude",
		Status:        domain.RunStatusActive,
		WorkspacePath: t.TempDir(),
	}

	if err := svc.runHook(context.Background(), "echo hooked > hooked.txt", run.WorkspacePath, run, "before_run"); err != nil {
		t.Fatalf("run hook: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	if got := runner.lastSpec.Env["MAESTRO_RUN_STAGE"]; got != "before_run" {
		t.Fatalf("hook env stage = %q, want before_run", got)
	}
	if _, err := os.Stat(filepath.Join(run.WorkspacePath, "hooked.txt")); err != nil {
		t.Fatalf("expected hooked.txt: %v", err)
	}
}

func TestRunHookDefaultsToHostExecutionEvenWithDockerRunner(t *testing.T) {
	cfg := &config.Config{
		Hooks: config.HooksConfig{
			Timeout: config.Duration{Duration: 5 * time.Second},
		},
	}
	runner := &recordingRunner{kind: "docker"}
	svc := &Service{
		cfg:           cfg,
		processRunner: runner,
	}
	run := &domain.AgentRun{
		ID:            "run-1",
		Issue:         domain.Issue{ID: "issue-1", Identifier: "OPS-1"},
		AgentName:     "dev-claude",
		AgentType:     "dev-claude",
		Status:        domain.RunStatusActive,
		WorkspacePath: t.TempDir(),
	}

	if err := svc.runHook(context.Background(), "printf hooked > hooked.txt", run.WorkspacePath, run, "before_run"); err != nil {
		t.Fatalf("run hook: %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
	if _, err := os.Stat(filepath.Join(run.WorkspacePath, "hooked.txt")); err != nil {
		t.Fatalf("expected hooked.txt: %v", err)
	}
}

type recordingRunner struct {
	kind     string
	calls    int
	lastSpec harness.ProcessSpec
}

func (r *recordingRunner) Kind() string { return r.kind }

func (r *recordingRunner) ResolveBinary(name string) (string, error) { return name, nil }

func (r *recordingRunner) VisibleWorkdir(hostPath string) string { return hostPath }

func (r *recordingRunner) CommandContext(ctx context.Context, spec harness.ProcessSpec) (*exec.Cmd, error) {
	r.calls++
	r.lastSpec = spec
	cmd := exec.CommandContext(ctx, spec.Binary, spec.Args...)
	cmd.Dir = spec.Workdir
	cmd.Env = os.Environ()
	for key, value := range spec.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	return cmd, nil
}
