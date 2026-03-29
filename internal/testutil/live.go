package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tjohnson/maestro/internal/config"
)

// RequireEnv skips the test unless all env vars are non-empty.
func RequireEnv(t *testing.T, names ...string) map[string]string {
	t.Helper()

	values := make(map[string]string, len(names))
	for _, name := range names {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			t.Skipf("skipping live test; %s is not set", name)
		}
		values[name] = value
	}

	return values
}

// RequireFlag skips the test unless the env flag is set to "1".
func RequireFlag(t *testing.T, name string) {
	t.Helper()

	if strings.TrimSpace(os.Getenv(name)) != "1" {
		t.Skipf("skipping live test; %s=1 is required", name)
	}
}

type LiveDockerHarnessConfig struct {
	Docker config.DockerConfig
	RunEnv map[string]string
}

func RequireLiveDockerHarness(t *testing.T, flag string, imageEnv string, authPassthroughEnvs []string, defaultAuthSource string, authSourceEnv string, authTargetEnv string) LiveDockerHarnessConfig {
	t.Helper()

	RequireFlag(t, flag)
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("skipping live docker test; docker binary not found: %v", err)
	}

	image := strings.TrimSpace(os.Getenv(imageEnv))
	if image == "" {
		t.Skipf("skipping live docker test; %s is not set", imageEnv)
	}

	cfg := LiveDockerHarnessConfig{
		Docker: config.DockerConfig{
			Image:              image,
			WorkspaceMountPath: "/workspace",
			Network:            "bridge",
		},
		RunEnv: map[string]string{},
	}

	passthrough := make([]string, 0, len(authPassthroughEnvs))
	hasExplicitAuth := false
	for _, rawName := range authPassthroughEnvs {
		name := strings.TrimPrefix(strings.TrimSpace(rawName), "$")
		if name == "" {
			continue
		}
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			hasExplicitAuth = true
			passthrough = append(passthrough, name)
		}
	}
	if hasExplicitAuth {
		cfg.Docker.EnvPassthrough = passthrough
		if mount, home := defaultClaudeWritableHome(t, defaultAuthSource); mount.Source != "" {
			cfg.Docker.Mounts = append(cfg.Docker.Mounts, mount)
			cfg.RunEnv["HOME"] = home
		}
		return cfg
	}

	authSource := strings.TrimSpace(os.Getenv(authSourceEnv))
	if authSource == "" {
		authSource = strings.TrimSpace(defaultAuthSource)
	}
	if authSource == "" {
		t.Skipf("skipping live docker test; neither explicit auth envs nor %s is set", authSourceEnv)
	}
	if _, err := os.Stat(authSource); err != nil {
		t.Skipf("skipping live docker test; auth source %q is unavailable: %v", authSource, err)
	}

	authTarget := strings.TrimSpace(os.Getenv(authTargetEnv))
	if authTarget == "" {
		authTarget = authSource
	}

	cfg.Docker.Mounts = []config.DockerMountConfig{{
		Source:   authSource,
		Target:   authTarget,
		ReadOnly: true,
	}}
	for _, mount := range defaultClaudeAuxMounts(t, defaultAuthSource, authTarget) {
		cfg.Docker.Mounts = append(cfg.Docker.Mounts, mount)
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		cfg.RunEnv["HOME"] = home
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		cfg.RunEnv["XDG_CONFIG_HOME"] = xdg
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); xdg != "" {
		cfg.RunEnv["XDG_STATE_HOME"] = xdg
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); xdg != "" {
		cfg.RunEnv["XDG_CACHE_HOME"] = xdg
	}
	return cfg
}

func DefaultClaudeAuthSource() string {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".claude")
}

func DefaultCodexAuthSource() string {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".codex")
}

func defaultClaudeAuxMounts(t *testing.T, defaultSource string, authTarget string) []config.DockerMountConfig {
	t.Helper()
	if filepath.Clean(defaultSource) != filepath.Clean(DefaultClaudeAuthSource()) {
		return nil
	}
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return nil
	}
	mounts := []config.DockerMountConfig{}
	configSource := filepath.Join(home, ".claude.json")
	if _, err := os.Stat(configSource); err == nil {
		mounts = append(mounts, config.DockerMountConfig{
			Source:   configSource,
			Target:   filepath.Join(filepath.Dir(authTarget), ".claude.json"),
			ReadOnly: true,
		})
	}
	mounts = append(mounts, config.DockerMountConfig{
		Source: t.TempDir(),
		Target: filepath.Join(authTarget, "session-env"),
	})
	return mounts
}

func defaultClaudeWritableHome(t *testing.T, defaultSource string) (config.DockerMountConfig, string) {
	t.Helper()
	if filepath.Clean(defaultSource) != filepath.Clean(DefaultClaudeAuthSource()) {
		return config.DockerMountConfig{}, ""
	}
	return config.DockerMountConfig{
			Source: t.TempDir(),
			Target: "/tmp/maestro-home",
		},
		"/tmp/maestro-home"
}
