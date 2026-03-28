package harness

import (
	"strings"
	"testing"
)

func TestMergeEnvIncludesAllowedBaseEnvAndOverrides(t *testing.T) {
	t.Setenv("PATH", "/usr/local/bin:/usr/bin")
	t.Setenv("HOME", "/tmp/home")
	t.Setenv("MAESTRO_BASE_ENV", "base")

	env := MergeEnv(map[string]string{
		"MAESTRO_EXTRA_ENV": "extra",
		"PATH":              "/custom/bin",
	})

	if !containsEnv(env, "MAESTRO_EXTRA_ENV=extra") {
		t.Fatalf("env = %v, want MAESTRO_EXTRA_ENV=extra", env)
	}
	if !containsEnv(env, "HOME=/tmp/home") {
		t.Fatalf("env = %v, want inherited HOME", env)
	}
	if !containsEnv(env, "PATH=/custom/bin") {
		t.Fatalf("env = %v, want overridden PATH", env)
	}
	if containsEnv(env, "MAESTRO_BASE_ENV=base") {
		t.Fatalf("env = %v, did not expect inherited MAESTRO_BASE_ENV", env)
	}
}

func TestMergeEnvExcludesParentSecretsByDefault(t *testing.T) {
	t.Setenv("MAESTRO_GITLAB_TOKEN", "gitlab-secret")
	t.Setenv("SLACK_BOT_TOKEN", "slack-secret")
	t.Setenv("MAESTRO_LINEAR_TOKEN", "linear-secret")
	t.Setenv("PATH", "/usr/bin")

	env := MergeEnv(nil)

	if containsEnv(env, "MAESTRO_GITLAB_TOKEN=gitlab-secret") {
		t.Fatalf("env = %v, did not expect inherited MAESTRO_GITLAB_TOKEN", env)
	}
	if containsEnv(env, "SLACK_BOT_TOKEN=slack-secret") {
		t.Fatalf("env = %v, did not expect inherited SLACK_BOT_TOKEN", env)
	}
	if containsEnv(env, "MAESTRO_LINEAR_TOKEN=linear-secret") {
		t.Fatalf("env = %v, did not expect inherited MAESTRO_LINEAR_TOKEN", env)
	}
	if !containsEnv(env, "PATH=/usr/bin") {
		t.Fatalf("env = %v, want inherited PATH", env)
	}
}

func containsEnv(env []string, want string) bool {
	for _, entry := range env {
		if strings.TrimSpace(entry) == want {
			return true
		}
	}
	return false
}
