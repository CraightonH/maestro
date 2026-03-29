package orchestrator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tjohnson/maestro/internal/config"
)

func TestNewHarnessUsesDockerRunnerWhenConfigured(t *testing.T) {
	tmp := t.TempDir()
	dockerBinary := filepath.Join(tmp, "docker")
	if err := os.WriteFile(dockerBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write docker stub: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	h, err := newHarness(config.AgentTypeConfig{
		Name:    "code-pr",
		Harness: "claude-code",
		Docker: &config.DockerConfig{
			Image: "maestro-agent:latest",
		},
	})
	if err != nil {
		t.Fatalf("new harness: %v", err)
	}
	if got := h.Kind(); got != "claude-code" {
		t.Fatalf("harness kind = %q, want claude-code", got)
	}
}
