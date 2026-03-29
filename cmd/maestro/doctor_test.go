package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tjohnson/maestro/internal/config"
)

func TestRunDoctorChecksDockerAgents(t *testing.T) {
	origLookPath := doctorLookPath
	origRun := doctorRunCommand
	t.Cleanup(func() {
		doctorLookPath = origLookPath
		doctorRunCommand = origRun
	})

	doctorLookPath = func(name string) (string, error) {
		if name == "docker" {
			return "/usr/bin/docker", nil
		}
		return "", errors.New("unexpected binary lookup")
	}
	doctorRunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "/usr/bin/docker" {
			return nil, errors.New("unexpected command")
		}
		switch strings.Join(args, " ") {
		case "info --format {{.ServerVersion}}":
			return []byte("25.0.0\n"), nil
		case "image inspect ghcr.io/acme/maestro-codex:latest":
			return []byte("[]"), nil
		case "run --rm --entrypoint codex ghcr.io/acme/maestro-codex:latest --version":
			return []byte("codex 0.116.0\n"), nil
		default:
			return nil, errors.New("unexpected docker args: " + strings.Join(args, " "))
		}
	}

	t.Setenv("DOCKER_HOST", "unix:///tmp/colima.sock")

	cfg := &config.Config{
		ConfigPath: "/tmp/maestro.yaml",
		AgentTypes: []config.AgentTypeConfig{{
			Name:    "docker-codex",
			Harness: "codex",
			Docker: &config.DockerConfig{
				Image: "ghcr.io/acme/maestro-codex:latest",
			},
		}},
	}

	var out bytes.Buffer
	if err := runDoctor(&out, cfg); err != nil {
		t.Fatalf("runDoctor: %v\noutput:\n%s", err, out.String())
	}

	got := out.String()
	for _, want := range []string{
		"Maestro doctor",
		"DOCKER_HOST=unix:///tmp/colima.sock",
		"OK  docker -> /usr/bin/docker",
		"OK  docker daemon reachable (25.0.0)",
		"Agent docker-codex image ghcr.io/acme/maestro-codex:latest",
		"OK  image present",
		"OK  codex binary present",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q\noutput:\n%s", want, got)
		}
	}
}

func TestRunDoctorFallsBackToPullableImage(t *testing.T) {
	origLookPath := doctorLookPath
	origRun := doctorRunCommand
	t.Cleanup(func() {
		doctorLookPath = origLookPath
		doctorRunCommand = origRun
	})

	doctorLookPath = func(name string) (string, error) {
		if name == "docker" {
			return "/usr/bin/docker", nil
		}
		return "", errors.New("unexpected binary lookup")
	}
	doctorRunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "info --format {{.ServerVersion}}":
			return []byte("25.0.0\n"), nil
		case "image inspect ghcr.io/acme/maestro-claude:latest":
			return nil, errors.New("not found")
		case "pull ghcr.io/acme/maestro-claude:latest":
			return []byte("pulled\n"), nil
		case "run --rm --entrypoint claude ghcr.io/acme/maestro-claude:latest --version":
			return []byte("claude 2.1.0\n"), nil
		default:
			return nil, errors.New("unexpected docker args: " + strings.Join(args, " "))
		}
	}

	cfg := &config.Config{
		ConfigPath: "/tmp/maestro.yaml",
		AgentTypes: []config.AgentTypeConfig{{
			Name:    "docker-claude",
			Harness: "claude-code",
			Docker: &config.DockerConfig{
				Image: "ghcr.io/acme/maestro-claude:latest",
			},
		}},
	}

	var out bytes.Buffer
	if err := runDoctor(&out, cfg); err != nil {
		t.Fatalf("runDoctor: %v\noutput:\n%s", err, out.String())
	}

	got := out.String()
	for _, want := range []string{
		"OK  image pullable",
		"OK  claude binary present",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q\noutput:\n%s", want, got)
		}
	}
}

func TestRunDoctorFailsWhenDockerDaemonUnreachable(t *testing.T) {
	origLookPath := doctorLookPath
	origRun := doctorRunCommand
	t.Cleanup(func() {
		doctorLookPath = origLookPath
		doctorRunCommand = origRun
	})

	doctorLookPath = func(name string) (string, error) {
		if name == "docker" {
			return "/usr/bin/docker", nil
		}
		return "", errors.New("unexpected binary lookup")
	}
	doctorRunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "info --format {{.ServerVersion}}" {
			return []byte(""), errors.New("daemon unreachable")
		}
		return nil, errors.New("unexpected docker args")
	}

	cfg := &config.Config{
		ConfigPath: "/tmp/maestro.yaml",
		AgentTypes: []config.AgentTypeConfig{{
			Name:    "docker-codex",
			Harness: "codex",
			Docker: &config.DockerConfig{
				Image: "ghcr.io/acme/maestro-codex:latest",
			},
		}},
	}

	var out bytes.Buffer
	err := runDoctor(&out, cfg)
	if err == nil {
		t.Fatalf("runDoctor = nil, want daemon error\noutput:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "FAIL docker daemon unreachable") {
		t.Fatalf("doctor output missing daemon failure\noutput:\n%s", out.String())
	}
}
