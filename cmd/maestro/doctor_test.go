package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
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
		"WARN image is not digest-pinned",
		"OK  image present",
		"OK  codex binary present",
		"OK  no extra Docker env or mounts configured",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q\noutput:\n%s", want, got)
		}
	}
}

func TestRunDoctorExplainsDockerAccess(t *testing.T) {
	origLookPath := doctorLookPath
	origRun := doctorRunCommand
	t.Cleanup(func() {
		doctorLookPath = origLookPath
		doctorRunCommand = origRun
	})

	root := t.TempDir()
	netrcPath := filepath.Join(root, "netrc")
	if err := os.WriteFile(netrcPath, []byte("machine example.invalid\n"), 0o600); err != nil {
		t.Fatalf("write netrc: %v", err)
	}
	gitConfigPath := filepath.Join(root, "gitconfig")
	if err := os.WriteFile(gitConfigPath, []byte("[user]\n\tname = TJ\n"), 0o644); err != nil {
		t.Fatalf("write gitconfig: %v", err)
	}

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
			return []byte("[]"), nil
		case "run --rm --entrypoint claude ghcr.io/acme/maestro-claude:latest --version":
			return []byte("claude 2.1.0\n"), nil
		default:
			return nil, errors.New("unexpected docker args: " + strings.Join(args, " "))
		}
	}

	t.Setenv("ANTHROPIC_BASE_URL", "https://proxy.example.invalid")

	cfg := &config.Config{
		ConfigPath: "/tmp/maestro.yaml",
		AgentTypes: []config.AgentTypeConfig{{
			Name:    "docker-claude",
			Harness: "claude-code",
			Docker: &config.DockerConfig{
				Image: "ghcr.io/acme/maestro-claude:latest",
				Secrets: &config.DockerSecretsConfig{
					Env: []config.DockerSecretEnvConfig{{
						Preset: config.DockerSecretEnvPresetAnthropicBaseURL,
					}},
					Mounts: []config.DockerAccessMountConfig{{
						Preset: config.DockerMountPresetNetrc,
						Source: netrcPath,
					}},
				},
				Tools: &config.DockerToolsConfig{
					Mounts: []config.DockerAccessMountConfig{{
						Preset: config.DockerMountPresetGitConfig,
						Source: gitConfigPath,
					}},
				},
			},
		}},
	}

	var out bytes.Buffer
	if err := runDoctor(&out, cfg); err != nil {
		t.Fatalf("runDoctor: %v\noutput:\n%s", err, out.String())
	}

	got := out.String()
	for _, want := range []string{
		"Env",
		"ANTHROPIC_BASE_URL <= $ANTHROPIC_BASE_URL",
		"Secret mounts",
		filepath.Join(config.DockerHomeDefault, ".netrc") + " <= " + netrcPath,
		"Tool mounts",
		filepath.Join(config.DockerHomeDefault, ".gitconfig") + " <= " + gitConfigPath,
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
		"WARN image is not digest-pinned",
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

func TestRunDoctorAcknowledgesDigestPinnedImage(t *testing.T) {
	origLookPath := doctorLookPath
	origRun := doctorRunCommand
	t.Cleanup(func() {
		doctorLookPath = origLookPath
		doctorRunCommand = origRun
	})

	image := "ghcr.io/acme/maestro-codex@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

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
		case "image inspect " + image:
			return []byte("[]"), nil
		case "run --rm --entrypoint codex " + image + " --version":
			return []byte("codex 0.116.0\n"), nil
		default:
			return nil, errors.New("unexpected docker args: " + strings.Join(args, " "))
		}
	}

	cfg := &config.Config{
		ConfigPath: "/tmp/maestro.yaml",
		AgentTypes: []config.AgentTypeConfig{{
			Name:    "docker-codex",
			Harness: "codex",
			Docker: &config.DockerConfig{
				Image: image,
			},
		}},
	}

	var out bytes.Buffer
	if err := runDoctor(&out, cfg); err != nil {
		t.Fatalf("runDoctor: %v\noutput:\n%s", err, out.String())
	}

	got := out.String()
	if !strings.Contains(got, "OK  image is digest-pinned") {
		t.Fatalf("doctor output missing digest pin acknowledgement\noutput:\n%s", got)
	}
	if strings.Contains(got, "WARN image is not digest-pinned") {
		t.Fatalf("doctor output unexpectedly warned for digest-pinned image\noutput:\n%s", got)
	}
}

func TestRunDoctorValidatesAllowlistNetworkPolicySupport(t *testing.T) {
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
		case "image inspect ghcr.io/acme/maestro-codex:latest":
			return []byte("[]"), nil
		case "run --rm --entrypoint codex ghcr.io/acme/maestro-codex:latest --version":
			return []byte("codex 0.116.0\n"), nil
		default:
			return nil, errors.New("unexpected docker args: " + strings.Join(args, " "))
		}
	}

	cfg := &config.Config{
		ConfigPath: "/tmp/maestro.yaml",
		AgentTypes: []config.AgentTypeConfig{{
			Name:    "docker-codex",
			Harness: "codex",
			Docker: &config.DockerConfig{
				Image: "ghcr.io/acme/maestro-codex:latest",
				NetworkPolicy: &config.DockerNetworkPolicyConfig{
					Mode:  config.DockerNetworkPolicyAllowlist,
					Allow: []string{"api.openai.com"},
				},
			},
		}},
	}

	var out bytes.Buffer
	if err := runDoctor(&out, cfg); err != nil {
		t.Fatalf("runDoctor: %v\noutput:\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"network policy allowlist (1 host(s)/domain(s))",
		"host-gateway resolution supported by Docker 25.0.0",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q\noutput:\n%s", want, got)
		}
	}
}

func TestRunDoctorFailsAllowlistNetworkPolicyOnOldDocker(t *testing.T) {
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
			return []byte("20.9.0\n"), nil
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
				NetworkPolicy: &config.DockerNetworkPolicyConfig{
					Mode:  config.DockerNetworkPolicyAllowlist,
					Allow: []string{"api.openai.com"},
				},
			},
		}},
	}

	var out bytes.Buffer
	err := runDoctor(&out, cfg)
	if err == nil {
		t.Fatalf("runDoctor = nil, want allowlist support error\noutput:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "allowlist mode requires Docker server 20.10+") {
		t.Fatalf("doctor output missing allowlist support failure\noutput:\n%s", out.String())
	}
}
