package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tjohnson/maestro/internal/config"
)

func TestLoadAndValidateMVP(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	promptDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	promptPath := filepath.Join(promptDir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Issue {{.Issue.Identifier}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
user:
  name: TJ
  gitlab_username: tjohnson
sources:
  - name: platform-dev
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
      assignee: $MAESTRO_USER
    agent_type: code-pr
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: agents/code-pr/prompt.md
    approval_policy: auto
    max_concurrent: 1
workspace:
  root: ./workspaces
logging:
  level: debug
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if err := config.ValidateMVP(cfg); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	if got, want := cfg.Sources[0].Connection.Token, "secret-token"; got != want {
		t.Fatalf("resolved token = %q, want %q", got, want)
	}
	if got, want := cfg.Sources[0].Filter.Assignee, "tjohnson"; got != want {
		t.Fatalf("resolved assignee = %q, want %q", got, want)
	}
	if got, want := cfg.Sources[0].PollInterval.Duration, 5*time.Second; got != want {
		t.Fatalf("poll interval = %s, want %s", got, want)
	}
	if got, want := cfg.State.MaxAttempts, 3; got != want {
		t.Fatalf("max attempts = %d, want %d", got, want)
	}
	if got, want := cfg.Hooks.Execution, "host"; got != want {
		t.Fatalf("hooks execution = %q, want %q", got, want)
	}
}

func TestLoadResolvesLinearAssignee(t *testing.T) {
	t.Setenv("LINEAR_TOKEN", "secret-token")

	root := t.TempDir()
	promptDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	promptPath := filepath.Join(promptDir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Issue {{.Issue.Identifier}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
user:
  name: TJ
  linear_username: tj
sources:
  - name: personal-linear
    tracker: linear
    connection:
      token_env: LINEAR_TOKEN
      project: project-1
    repo: https://gitlab.example.com/team/maestro-testbed.git
    filter:
      states: [Todo]
      assignee: $MAESTRO_USER
    agent_type: code-pr
agent_types:
  - name: code-pr
    harness: codex
    workspace: git-clone
    prompt: agents/code-pr/prompt.md
    approval_policy: auto
    max_concurrent: 1
workspace:
  root: ./workspaces
logging:
  level: debug
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if err := config.ValidateMVP(cfg); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	if got, want := cfg.Sources[0].Filter.Assignee, "tj"; got != want {
		t.Fatalf("resolved assignee = %q, want %q", got, want)
	}
}

func TestLoadResolvesGitLabEpicIssueFilterAssignee(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	promptDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	promptPath := filepath.Join(promptDir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Issue {{.Issue.Identifier}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
user:
  name: TJ
  gitlab_username: tj
sources:
  - name: platform-epics
    tracker: gitlab-epic
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      group: team/platform
    repo: https://gitlab.example.com/team/project.git
    epic_filter:
      labels: [bucket:ready]
    issue_filter:
      labels: [agent:ready]
      assignee: $MAESTRO_USER
    agent_type: code-pr
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: agents/code-pr/prompt.md
    approval_policy: auto
    max_concurrent: 1
workspace:
  root: ./workspaces
logging:
  level: debug
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if got, want := cfg.Sources[0].IssueFilter.Assignee, "tj"; got != want {
		t.Fatalf("resolved issue_filter assignee = %q, want %q", got, want)
	}
}

func TestLoadAppliesServerDefaults(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	promptDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	promptPath := filepath.Join(promptDir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Issue {{.Issue.Identifier}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
user:
  name: TJ
  gitlab_username: tj
sources:
  - name: platform-dev
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: agents/code-pr/prompt.md
    approval_policy: auto
    max_concurrent: 1
server:
  enabled: true
workspace:
  root: ./workspaces
logging:
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got, want := cfg.Server.Host, "127.0.0.1"; got != want {
		t.Fatalf("server host = %q, want %q", got, want)
	}
	if got, want := cfg.Server.Port, 8742; got != want {
		t.Fatalf("server port = %d, want %d", got, want)
	}
}

func TestLoadAppliesDefaultApprovalTimeout(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	promptDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	promptPath := filepath.Join(promptDir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Issue {{.Issue.Identifier}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
user:
  name: TJ
  gitlab_username: tj
sources:
  - name: platform-dev
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: agents/code-pr/prompt.md
    approval_policy: manual
    max_concurrent: 1
workspace:
  root: ./workspaces
logging:
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if got, want := cfg.AgentTypes[0].ApprovalTimeout.Duration, 24*time.Hour; got != want {
		t.Fatalf("approval timeout = %s, want %s", got, want)
	}
}

func TestLoadAppliesAgentDefaultApprovalTimeout(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	promptDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	promptPath := filepath.Join(promptDir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Issue {{.Issue.Identifier}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
agent_defaults:
  approval_timeout: 2h
user:
  name: TJ
  gitlab_username: tj
sources:
  - name: platform-dev
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: agents/code-pr/prompt.md
    approval_policy: manual
    max_concurrent: 1
workspace:
  root: ./workspaces
logging:
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if got, want := cfg.AgentTypes[0].ApprovalTimeout.Duration, 2*time.Hour; got != want {
		t.Fatalf("approval timeout = %s, want %s", got, want)
	}
}

func TestLoadAppliesSourceRetryDefaultsAndOverrides(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	promptDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	promptPath := filepath.Join(promptDir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Issue {{.Issue.Identifier}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
source_defaults:
  gitlab:
    retry_base: 30s
    max_retry_backoff: 10m
    max_attempts: 4
user:
  name: TJ
  gitlab_username: tj
sources:
  - name: inherited
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
  - name: override
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
    retry_base: 45s
    max_retry_backoff: 15m
    max_attempts: 5
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: agents/code-pr/prompt.md
    approval_policy: manual
    max_concurrent: 1
workspace:
  root: ./workspaces
logging:
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if got, want := cfg.Sources[0].RetryBase.Duration, 30*time.Second; got != want {
		t.Fatalf("source retry base = %s, want %s", got, want)
	}
	if got, want := cfg.Sources[0].MaxRetryBackoff.Duration, 10*time.Minute; got != want {
		t.Fatalf("source max retry backoff = %s, want %s", got, want)
	}
	if got, want := cfg.Sources[0].MaxAttempts, 4; got != want {
		t.Fatalf("source max attempts = %d, want %d", got, want)
	}
	if got, want := cfg.Sources[1].RetryBase.Duration, 45*time.Second; got != want {
		t.Fatalf("override retry base = %s, want %s", got, want)
	}
	if got, want := cfg.Sources[1].MaxRetryBackoff.Duration, 15*time.Minute; got != want {
		t.Fatalf("override max retry backoff = %s, want %s", got, want)
	}
	if got, want := cfg.Sources[1].MaxAttempts, 5; got != want {
		t.Fatalf("override max attempts = %d, want %d", got, want)
	}
}

func TestLoadAppliesRespectBlockersDefaultsAndOverrides(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	promptDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	promptPath := filepath.Join(promptDir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Issue {{.Issue.Identifier}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
source_defaults:
  gitlab:
    respect_blockers: false
user:
  name: TJ
  gitlab_username: tj
sources:
  - name: inherited
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
  - name: override
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
    respect_blockers: true
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: agents/code-pr/prompt.md
    approval_policy: manual
    max_concurrent: 1
workspace:
  root: ./workspaces
logging:
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if got := cfg.Sources[0].EffectiveRespectBlockers(); got {
		t.Fatalf("inherited respect_blockers = %t, want false", got)
	}
	if got := cfg.Sources[1].EffectiveRespectBlockers(); !got {
		t.Fatalf("override respect_blockers = %t, want true", got)
	}
}

func TestLoadMergesAgentPackDefaultsAndOverrides(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	packsDir := filepath.Join(root, "agent-packs", "repo-maintainer")
	if err := os.MkdirAll(packsDir, 0o755); err != nil {
		t.Fatalf("mkdir pack dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packsDir, "prompt.md"), []byte("Agent {{.Agent.Name}} using {{.Agent.Harness}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packsDir, "context.md"), []byte("Run the narrowest verification."), 0o644); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(packsDir, "claude"), 0o755); err != nil {
		t.Fatalf("mkdir claude dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packsDir, "claude", "CLAUDE.md"), []byte("Pack-specific claude instructions"), 0o644); err != nil {
		t.Fatalf("write claude instructions: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(packsDir, "codex"), 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packsDir, "codex", "config.toml"), []byte("model = \"gpt-5\""), 0o644); err != nil {
		t.Fatalf("write codex config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packsDir, "agent.yaml"), []byte(`
name: repo-maintainer
description: Maintains repositories.
instance_name: maintainer
harness: claude-code
workspace: git-clone
prompt: prompt.md
approval_policy: auto
max_concurrent: 1
tools: [git, make]
skills: [small-diffs]
context_files: [context.md]
env:
  BASE_ENV: from-pack
`), 0o644); err != nil {
		t.Fatalf("write pack: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
agent_packs_dir: ./agent-packs
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
user:
  name: TJ
  gitlab_username: tjohnson
sources:
  - name: platform-dev
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: repo-maintainer
agent_types:
  - name: repo-maintainer
    agent_pack: repo-maintainer
    harness: codex
    env:
      EXTRA_ENV: from-config
workspace:
  root: ./workspaces
logging:
  level: debug
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	agent := cfg.AgentTypes[0]
	if agent.PackPath != filepath.Join(root, "agent-packs", "repo-maintainer", "agent.yaml") {
		t.Fatalf("pack path = %q", agent.PackPath)
	}
	if agent.PackClaudeDir != filepath.Join(root, "agent-packs", "repo-maintainer", "claude") {
		t.Fatalf("pack claude dir = %q", agent.PackClaudeDir)
	}
	if agent.PackCodexDir != filepath.Join(root, "agent-packs", "repo-maintainer", "codex") {
		t.Fatalf("pack codex dir = %q", agent.PackCodexDir)
	}
	if agent.Harness != "codex" {
		t.Fatalf("harness = %q, want codex", agent.Harness)
	}
	if agent.InstanceName != "maintainer" {
		t.Fatalf("instance name = %q", agent.InstanceName)
	}
	if agent.Prompt != filepath.Join(root, "agent-packs", "repo-maintainer", "prompt.md") {
		t.Fatalf("prompt = %q", agent.Prompt)
	}
	if agent.ApprovalPolicy != "auto" {
		t.Fatalf("approval policy = %q", agent.ApprovalPolicy)
	}
	if got := agent.Env["BASE_ENV"]; got != "from-pack" {
		t.Fatalf("base env = %q", got)
	}
	if got := agent.Env["EXTRA_ENV"]; got != "from-config" {
		t.Fatalf("extra env = %q", got)
	}
	if got, want := agent.Tools, []string{"git", "make"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("tools = %v, want %v", got, want)
	}
	if !strings.Contains(agent.Context, "Run the narrowest verification.") {
		t.Fatalf("context missing pack guidance: %q", agent.Context)
	}
	if !strings.Contains(agent.Context, "Do not use extra turns only to confirm that nothing has changed.") {
		t.Fatalf("context missing global guidance: %q", agent.Context)
	}
}

func TestLoadMergesAgentPackHarnessConfigPerKey(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	packsDir := filepath.Join(root, "agent-packs", "repo-maintainer")
	if err := os.MkdirAll(packsDir, 0o755); err != nil {
		t.Fatalf("mkdir pack dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packsDir, "prompt.md"), []byte("Agent {{.Agent.Name}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packsDir, "agent.yaml"), []byte(`
name: repo-maintainer
harness: codex
workspace: git-clone
prompt: prompt.md
approval_policy: auto
codex:
  model: gpt-5.4
  reasoning: high
  max_turns: 12
  thread_sandbox: workspaceWrite
  turn_sandbox_policy:
    type: workspaceWrite
  extra_args: ["--search"]
`), 0o644); err != nil {
		t.Fatalf("write pack: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
agent_packs_dir: ./agent-packs
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
user:
  name: TJ
  gitlab_username: tjohnson
sources:
  - name: platform-dev
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: repo-maintainer
agent_types:
  - name: repo-maintainer
    agent_pack: repo-maintainer
    codex:
      reasoning: medium
      extra_args: []
workspace:
  root: ./workspaces
logging:
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	agent := cfg.AgentTypes[0]
	if agent.Codex == nil {
		t.Fatal("codex config = nil, want merged config")
	}
	if got, want := agent.Codex.Model, "gpt-5.4"; got != want {
		t.Fatalf("model = %q, want %q", got, want)
	}
	if got, want := agent.Codex.Reasoning, "medium"; got != want {
		t.Fatalf("reasoning = %q, want %q", got, want)
	}
	if got, want := agent.Codex.MaxTurns, 12; got != want {
		t.Fatalf("max turns = %d, want %d", got, want)
	}
	if got, want := agent.Codex.ThreadSandbox, "workspaceWrite"; got != want {
		t.Fatalf("thread sandbox = %q, want %q", got, want)
	}
	if got := agent.Codex.TurnSandboxPolicy["type"]; got != "workspaceWrite" {
		t.Fatalf("turn sandbox policy type = %v, want workspaceWrite", got)
	}
	if agent.Codex.ExtraArgs == nil || len(agent.Codex.ExtraArgs) != 0 {
		t.Fatalf("extra args = %v, want explicit empty override", agent.Codex.ExtraArgs)
	}
}

func TestLoadResolvesDockerMountSources(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	promptDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "prompt.md"), []byte("Issue {{.Issue.Identifier}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	authDir := filepath.Join(root, "auth", "claude")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
user:
  name: TJ
  gitlab_username: tjohnson
sources:
  - name: platform-dev
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: agents/code-pr/prompt.md
    approval_policy: auto
    max_concurrent: 1
    docker:
      image: maestro-agent:latest
      pull_policy: always
      auth:
        mode: claude-config-mount
        source: ./auth/claude
      security:
        read_only_root_fs: false
        drop_capabilities: [NET_RAW]
        tmpfs: [/var/tmp]
      cache:
        profiles: [go-cache]
        mounts:
          - source: ./cache/go-build
            target: /tmp/maestro-home/.cache/go-build
      mounts:
        - source: ./auth/claude
          target: /tmp/maestro-home/.claude
          read_only: true
      env_passthrough: [ANTHROPIC_API_KEY]
workspace:
  root: ./workspaces
logging:
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	agent := cfg.AgentTypes[0]
	if agent.Docker == nil {
		t.Fatal("docker config = nil, want parsed config")
	}
	if got := agent.Docker.Mounts[0].Source; got != authDir {
		t.Fatalf("docker mount source = %q, want %q", got, authDir)
	}
	if agent.Docker.Auth == nil || agent.Docker.Auth.Source != authDir {
		t.Fatalf("docker auth source = %q, want %q", agent.Docker.Auth.Source, authDir)
	}
	if got := agent.Docker.EnvPassthrough[0]; got != "ANTHROPIC_API_KEY" {
		t.Fatalf("docker env_passthrough = %q, want ANTHROPIC_API_KEY", got)
	}
	if got, want := agent.Docker.PullPolicy, "always"; got != want {
		t.Fatalf("docker pull_policy = %q, want %q", got, want)
	}
	if agent.Docker.Auth == nil || agent.Docker.Auth.Mode != "claude-config-mount" {
		t.Fatalf("docker auth = %v, want claude-config-mount", agent.Docker.Auth)
	}
	if agent.Docker.Security == nil || agent.Docker.Security.ReadOnlyRootFS == nil || *agent.Docker.Security.ReadOnlyRootFS {
		t.Fatalf("docker security = %v, want read_only_root_fs false", agent.Docker.Security)
	}
	if got := agent.Docker.Security.DropCapabilities; len(got) != 1 || got[0] != "NET_RAW" {
		t.Fatalf("docker drop_capabilities = %v, want [NET_RAW]", got)
	}
	if got := agent.Docker.Security.Tmpfs; len(got) != 1 || got[0] != "/var/tmp" {
		t.Fatalf("docker tmpfs = %v, want [/var/tmp]", got)
	}
	if agent.Docker.Cache == nil || len(agent.Docker.Cache.Profiles) != 1 || agent.Docker.Cache.Profiles[0] != "go-cache" {
		t.Fatalf("docker cache = %v, want go-cache profile", agent.Docker.Cache)
	}
	if len(agent.Docker.Cache.Mounts) != 1 || agent.Docker.Cache.Mounts[0].Source != filepath.Join(root, "cache", "go-build") {
		t.Fatalf("docker cache mounts = %v, want resolved source", agent.Docker.Cache.Mounts)
	}
}

func TestResolveDockerConfigAppliesSafeDefaults(t *testing.T) {
	docker := config.ResolveDockerConfig(nil, nil)
	if got, want := docker.ImagePinMode, config.DockerImagePinModeAllow; got != want {
		t.Fatalf("image_pin_mode = %q, want %q", got, want)
	}
	if got, want := docker.WorkspaceMountPath, "/workspace"; got != want {
		t.Fatalf("workspace_mount_path = %q, want %q", got, want)
	}
	if got, want := docker.Network, "bridge"; got != want {
		t.Fatalf("network = %q, want %q", got, want)
	}
	if got, want := docker.PullPolicy, "missing"; got != want {
		t.Fatalf("pull_policy = %q, want %q", got, want)
	}
	if docker.Security == nil || docker.Security.NoNewPrivileges == nil || !*docker.Security.NoNewPrivileges {
		t.Fatalf("security.no_new_privileges = %v, want true", docker.Security)
	}
	if docker.Security.ReadOnlyRootFS == nil || !*docker.Security.ReadOnlyRootFS {
		t.Fatalf("security.read_only_root_fs = %v, want true", docker.Security)
	}
	if got := docker.Security.DropCapabilities; len(got) != 1 || got[0] != "ALL" {
		t.Fatalf("security.drop_capabilities = %v, want [ALL]", got)
	}
	if got := docker.Security.Tmpfs; len(got) != 1 || got[0] != "/tmp" {
		t.Fatalf("security.tmpfs = %v, want [/tmp]", got)
	}
	if got, want := docker.Security.Preset, config.DockerSecurityPresetDefault; got != want {
		t.Fatalf("security.preset = %q, want %q", got, want)
	}
}

func TestResolveDockerConfigAppliesSecurityPresetBeforeOverrides(t *testing.T) {
	docker := config.ResolveDockerConfig(nil, &config.DockerConfig{
		ImagePinMode: config.DockerImagePinModeRequire,
		Security: &config.DockerSecurityConfig{
			Preset:           config.DockerSecurityPresetCompat,
			ReadOnlyRootFS:   boolPtrTest(true),
			DropCapabilities: []string{"NET_RAW"},
		},
	})

	if got, want := docker.ImagePinMode, config.DockerImagePinModeRequire; got != want {
		t.Fatalf("image_pin_mode = %q, want %q", got, want)
	}
	if docker.Security == nil {
		t.Fatal("security = nil, want resolved security config")
	}
	if got, want := docker.Security.Preset, config.DockerSecurityPresetCompat; got != want {
		t.Fatalf("security.preset = %q, want %q", got, want)
	}
	if docker.Security.NoNewPrivileges == nil || !*docker.Security.NoNewPrivileges {
		t.Fatalf("security.no_new_privileges = %v, want true", docker.Security.NoNewPrivileges)
	}
	if docker.Security.ReadOnlyRootFS == nil || !*docker.Security.ReadOnlyRootFS {
		t.Fatalf("security.read_only_root_fs = %v, want true override", docker.Security.ReadOnlyRootFS)
	}
	if got := docker.Security.DropCapabilities; len(got) != 1 || got[0] != "NET_RAW" {
		t.Fatalf("security.drop_capabilities = %v, want [NET_RAW]", got)
	}
	if len(docker.Security.Tmpfs) != 0 {
		t.Fatalf("security.tmpfs = %v, want cleared compat preset tmpfs", docker.Security.Tmpfs)
	}
}

func TestLoadAppliesAgentDefaultDockerConfig(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	promptDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "prompt.md"), []byte("Issue {{.Issue.Identifier}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
agent_defaults:
  docker:
    image: default-image:latest
    workspace_mount_path: /workspace
    pull_policy: missing
    env_passthrough: [OPENAI_API_KEY]
    network: none
    pids_limit: 128
    auth:
      mode: codex-api-key
    security:
      read_only_root_fs: true
      tmpfs: [/tmp]
    cache:
      profiles: [codex-cache]
user:
  name: TJ
  gitlab_username: tjohnson
sources:
  - name: platform-dev
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: agents/code-pr/prompt.md
    approval_policy: auto
    max_concurrent: 1
workspace:
  root: ./workspaces
logging:
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	agent := cfg.AgentTypes[0]
	if agent.Docker == nil {
		t.Fatal("docker config = nil, want inherited agent default")
	}
	if got, want := agent.Docker.Image, "default-image:latest"; got != want {
		t.Fatalf("docker image = %q, want %q", got, want)
	}
	if got, want := agent.Docker.Network, "none"; got != want {
		t.Fatalf("docker network = %q, want %q", got, want)
	}
	if got, want := agent.Docker.PIDsLimit, 128; got != want {
		t.Fatalf("docker pids_limit = %d, want %d", got, want)
	}
	if got := agent.Docker.EnvPassthrough[0]; got != "OPENAI_API_KEY" {
		t.Fatalf("docker env_passthrough = %q, want OPENAI_API_KEY", got)
	}
	if got, want := agent.Docker.PullPolicy, "missing"; got != want {
		t.Fatalf("docker pull_policy = %q, want %q", got, want)
	}
	if agent.Docker.Auth == nil || agent.Docker.Auth.Mode != "codex-api-key" {
		t.Fatalf("docker auth = %v, want codex-api-key", agent.Docker.Auth)
	}
	if agent.Docker.Security == nil || agent.Docker.Security.ReadOnlyRootFS == nil || !*agent.Docker.Security.ReadOnlyRootFS {
		t.Fatalf("docker security = %v, want read_only_root_fs true", agent.Docker.Security)
	}
	if got := agent.Docker.Security.Tmpfs; len(got) != 1 || got[0] != "/tmp" {
		t.Fatalf("docker tmpfs = %v, want [/tmp]", got)
	}
	if agent.Docker.Cache == nil || len(agent.Docker.Cache.Profiles) != 1 || agent.Docker.Cache.Profiles[0] != "codex-cache" {
		t.Fatalf("docker cache = %v, want codex-cache profile", agent.Docker.Cache)
	}
}

func TestLoadAppliesAgentDefaultDockerNetworkPolicy(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	promptDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "prompt.md"), []byte("Issue {{.Issue.Identifier}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
agent_defaults:
  docker:
    image: default-image:latest
    network_policy:
      mode: allowlist
      allow: [api.openai.com, "*.anthropic.com"]
user:
  name: TJ
  gitlab_username: tjohnson
sources:
  - name: platform-dev
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: agents/code-pr/prompt.md
    approval_policy: auto
    max_concurrent: 1
workspace:
  root: ./workspaces
logging:
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	agent := cfg.AgentTypes[0]
	if agent.Docker == nil || agent.Docker.NetworkPolicy == nil {
		t.Fatalf("docker network policy = %v, want inherited default policy", agent.Docker)
	}
	if got, want := agent.Docker.NetworkPolicy.Mode, "allowlist"; got != want {
		t.Fatalf("docker network policy mode = %q, want %q", got, want)
	}
	if got := agent.Docker.NetworkPolicy.Allow; len(got) != 2 || got[0] != "api.openai.com" || got[1] != "*.anthropic.com" {
		t.Fatalf("docker network policy allow = %v, want inherited allowlist", got)
	}
}

func TestLoadMergesAgentPackDockerConfigPerKey(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	packDir := filepath.Join(root, "agent-packs", "repo-maintainer")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatalf("mkdir pack dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "prompt.md"), []byte("Agent {{.Agent.Name}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "agent.yaml"), []byte(`
name: repo-maintainer
harness: codex
workspace: git-clone
prompt: prompt.md
approval_policy: auto
docker:
  image: base-image:latest
  workspace_mount_path: /pack-workspace
  pull_policy: never
  mounts:
    - source: ./fixtures/tooling
      target: /opt/tooling
      read_only: true
  auth:
    mode: codex-config-mount
    source: ./codex
  env_passthrough: [OPENAI_API_KEY]
  network: none
  cache:
    profiles: [claude-cache]
    mounts:
      - source: ./cache/openai
        target: /tmp/maestro-home/.codex/cache
`), 0o644); err != nil {
		t.Fatalf("write pack: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
agent_packs_dir: ./agent-packs
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
user:
  name: TJ
  gitlab_username: tjohnson
sources:
  - name: platform-dev
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: repo-maintainer
agent_types:
  - name: repo-maintainer
    agent_pack: repo-maintainer
    docker:
      image: override-image:latest
      pids_limit: 64
workspace:
  root: ./workspaces
logging:
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	agent := cfg.AgentTypes[0]
	if agent.Docker == nil {
		t.Fatal("docker config = nil, want merged config")
	}
	if got, want := agent.Docker.Image, "override-image:latest"; got != want {
		t.Fatalf("docker image = %q, want %q", got, want)
	}
	if got, want := agent.Docker.WorkspaceMountPath, "/pack-workspace"; got != want {
		t.Fatalf("docker workspace_mount_path = %q, want %q", got, want)
	}
	if got, want := agent.Docker.Network, "none"; got != want {
		t.Fatalf("docker network = %q, want %q", got, want)
	}
	if got := agent.Docker.EnvPassthrough[0]; got != "OPENAI_API_KEY" {
		t.Fatalf("docker env_passthrough = %q, want OPENAI_API_KEY", got)
	}
	if got, want := agent.Docker.PullPolicy, "never"; got != want {
		t.Fatalf("docker pull_policy = %q, want %q", got, want)
	}
	if len(agent.Docker.Mounts) != 1 {
		t.Fatalf("docker mounts = %v, want 1 mount", agent.Docker.Mounts)
	}
	if got, want := agent.Docker.Mounts[0].Source, filepath.Join(packDir, "fixtures", "tooling"); got != want {
		t.Fatalf("docker mount source = %q, want %q", got, want)
	}
	if got, want := agent.Docker.Mounts[0].Target, "/opt/tooling"; got != want {
		t.Fatalf("docker mount target = %q, want %q", got, want)
	}
	if !agent.Docker.Mounts[0].ReadOnly {
		t.Fatalf("docker mount read_only = false, want true")
	}
	if got, want := agent.Docker.PIDsLimit, 64; got != want {
		t.Fatalf("docker pids_limit = %d, want %d", got, want)
	}
	if agent.Docker.Auth == nil || agent.Docker.Auth.Mode != "codex-config-mount" {
		t.Fatalf("docker auth = %v, want codex-config-mount", agent.Docker.Auth)
	}
	if got, want := agent.Docker.Auth.Source, filepath.Join(packDir, "codex"); got != want {
		t.Fatalf("docker auth source = %q, want %q", got, want)
	}
	if got, want := agent.Docker.Auth.Target, ""; got != want {
		t.Fatalf("docker auth target = %q, want empty until runtime default", got)
	}
	if agent.Docker.Cache == nil || len(agent.Docker.Cache.Profiles) != 1 || agent.Docker.Cache.Profiles[0] != "claude-cache" {
		t.Fatalf("docker cache = %v, want claude-cache profile", agent.Docker.Cache)
	}
	if len(agent.Docker.Cache.Mounts) != 1 || agent.Docker.Cache.Mounts[0].Source != filepath.Join(packDir, "cache", "openai") {
		t.Fatalf("docker cache mounts = %v, want resolved source", agent.Docker.Cache.Mounts)
	}
}

func TestLoadResolvesDockerStructuredAccessSources(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	promptDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "prompt.md"), []byte("Issue {{.Issue.Identifier}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
user:
  name: TJ
  gitlab_username: tjohnson
sources:
  - name: platform-dev
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: agents/code-pr/prompt.md
    approval_policy: auto
    max_concurrent: 1
    docker:
      image: maestro-agent:latest
      secrets:
        env:
          - preset: anthropic-base-url
        mounts:
          - preset: netrc
            source: ./secrets/netrc
      tools:
        mounts:
          - preset: git-config
            source: ./tools/gitconfig
workspace:
  root: ./workspaces
logging:
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	agent := cfg.AgentTypes[0]
	if agent.Docker == nil || agent.Docker.Secrets == nil || agent.Docker.Tools == nil {
		t.Fatalf("docker structured access = %#v, want secrets and tools", agent.Docker)
	}
	if got, want := agent.Docker.Secrets.Env[0].Preset, config.DockerSecretEnvPresetAnthropicBaseURL; got != want {
		t.Fatalf("docker secrets env preset = %q, want %q", got, want)
	}
	if got, want := agent.Docker.Secrets.Mounts[0].Source, filepath.Join(root, "secrets", "netrc"); got != want {
		t.Fatalf("docker secrets mount source = %q, want %q", got, want)
	}
	if got, want := agent.Docker.Tools.Mounts[0].Source, filepath.Join(root, "tools", "gitconfig"); got != want {
		t.Fatalf("docker tools mount source = %q, want %q", got, want)
	}
}

func TestLoadMergesDockerStructuredAccessPerKey(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	packDir := filepath.Join(root, "agent-packs", "repo-maintainer")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatalf("mkdir pack dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "prompt.md"), []byte("Agent {{.Agent.Name}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "agent.yaml"), []byte(`
name: repo-maintainer
harness: codex
workspace: git-clone
prompt: prompt.md
approval_policy: auto
docker:
  image: base-image:latest
  secrets:
    env:
      - preset: anthropic-base-url
    mounts:
      - source: ./secrets/netrc
        target: /run/secrets/netrc
  tools:
    mounts:
      - source: ./tools/gitconfig
        target: /tmp/maestro-home/.gitconfig
`), 0o644); err != nil {
		t.Fatalf("write pack: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
agent_packs_dir: ./agent-packs
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
user:
  name: TJ
  gitlab_username: tjohnson
sources:
  - name: platform-dev
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: repo-maintainer
agent_types:
  - name: repo-maintainer
    agent_pack: repo-maintainer
    docker:
      image: override-image:latest
      secrets:
        env:
          - source: OPENAI_API_KEY
        mounts:
          - source: ./secrets/openai
            target: /run/secrets/openai
workspace:
  root: ./workspaces
logging:
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	agent := cfg.AgentTypes[0]
	if agent.Docker == nil {
		t.Fatal("docker config = nil, want merged config")
	}
	if got, want := agent.Docker.Image, "override-image:latest"; got != want {
		t.Fatalf("docker image = %q, want %q", got, want)
	}
	if len(agent.Docker.Secrets.Env) != 1 || agent.Docker.Secrets.Env[0].Source != "OPENAI_API_KEY" {
		t.Fatalf("docker secrets env = %#v, want override to replace pack secrets env", agent.Docker.Secrets.Env)
	}
	if len(agent.Docker.Secrets.Mounts) != 1 || agent.Docker.Secrets.Mounts[0].Source != filepath.Join(root, "secrets", "openai") {
		t.Fatalf("docker secrets mounts = %#v, want override secrets mounts", agent.Docker.Secrets.Mounts)
	}
	if len(agent.Docker.Tools.Mounts) != 1 || agent.Docker.Tools.Mounts[0].Source != filepath.Join(packDir, "tools", "gitconfig") {
		t.Fatalf("docker tools mounts = %#v, want inherited pack tools mount", agent.Docker.Tools.Mounts)
	}
}

func TestLoadDefersRepoPackResolution(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
user:
  name: TJ
sources:
  - name: platform-dev
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
agent_types:
  - name: code-pr
    agent_pack: "repo:"
    harness: claude-code
    workspace: git-clone
    approval_policy: auto
    max_concurrent: 1
workspace:
  root: ./workspaces
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	agent := cfg.AgentTypes[0]
	if agent.RepoPackPath != ".maestro" {
		t.Fatalf("repo pack path = %q, want .maestro", agent.RepoPackPath)
	}
	if agent.PackPath != "" {
		t.Fatalf("pack path = %q, want empty", agent.PackPath)
	}
	if agent.Prompt != "" {
		t.Fatalf("prompt = %q, want empty pre-clone", agent.Prompt)
	}
}

func TestResolveRepoPackErrorsWhenDirectoryMissing(t *testing.T) {
	root := t.TempDir()

	_, err := config.ResolveRepoPack(root, ".maestro")
	if err == nil || !strings.Contains(err.Error(), "repo pack dir") {
		t.Fatalf("resolve repo pack error = %v, want missing dir error", err)
	}
}

func TestLoadAppliesTrackerAndAgentDefaults(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "gitlab-secret")
	t.Setenv("LINEAR_TOKEN", "linear-secret")

	root := t.TempDir()
	promptDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	promptPath := filepath.Join(promptDir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Issue {{.Issue.Identifier}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	contextPath := filepath.Join(root, "shared-context.md")
	if err := os.WriteFile(contextPath, []byte("Shared operator context."), 0o644); err != nil {
		t.Fatalf("write context: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  agent_context: |
    - Prefer the narrowest verification that proves the change worked.
  poll_interval: 5s
  max_concurrent_global: 3
  stall_timeout: 12m
source_defaults:
  gitlab:
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
    filter:
      assignee: $MAESTRO_USER
      labels: [agent:ready]
  gitlab_epic:
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      group: team/platform
    repo: https://gitlab.example.com/team/platform/repo.git
    epic_filter:
      iids: [7]
    issue_filter:
      labels: [epic:ready]
      assignee: $MAESTRO_USER
  linear:
    connection:
      token_env: LINEAR_TOKEN
    filter:
      states: [Todo]
      assignee: $MAESTRO_USER
agent_defaults:
  harness: claude-code
  workspace: git-clone
  approval_policy: auto
  max_concurrent: 2
  context_files: [shared-context.md]
user:
  name: TJ
  gitlab_username: tjohnson
  linear_username: tj@example.com
sources:
  - name: project-a
    tracker: gitlab
    connection:
      project: team/project-a
    agent_type: code-pr
  - name: epic-a
    tracker: gitlab-epic
    issue_filter:
      labels: [epic:owned]
    agent_type: repo-maintainer
  - name: linear-a
    tracker: linear
    connection:
      project: Project A
    repo: https://gitlab.example.com/team/project-b.git
    agent_type: triage
agent_types:
  - name: code-pr
    prompt: agents/code-pr/prompt.md
  - name: repo-maintainer
    prompt: agents/code-pr/prompt.md
  - name: triage
    prompt: agents/code-pr/prompt.md
workspace:
  root: ./workspaces
logging:
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if got := cfg.Sources[0].Connection.BaseURL; got != "https://gitlab.example.com" {
		t.Fatalf("gitlab base url = %q", got)
	}
	if got := cfg.Sources[0].Filter.Assignee; got != "tjohnson" {
		t.Fatalf("gitlab assignee = %q", got)
	}
	if got := cfg.Sources[1].Connection.Group; got != "team/platform" {
		t.Fatalf("epic group = %q", got)
	}
	if got := cfg.Sources[1].EpicFilter.IIDs; len(got) != 1 || got[0] != 7 {
		t.Fatalf("epic iids = %v", got)
	}
	if got := cfg.Sources[1].IssueFilter.Labels; len(got) != 1 || got[0] != "epic:owned" {
		t.Fatalf("epic issue labels = %v", got)
	}
	if got := cfg.Sources[1].IssueFilter.Assignee; got != "tjohnson" {
		t.Fatalf("epic issue assignee = %q", got)
	}
	if got := cfg.Sources[2].Filter.Assignee; got != "tj@example.com" {
		t.Fatalf("linear assignee = %q", got)
	}
	for _, agent := range cfg.AgentTypes {
		if agent.Harness != "claude-code" {
			t.Fatalf("agent %s harness = %q", agent.Name, agent.Harness)
		}
		if agent.MaxConcurrent != 2 {
			t.Fatalf("agent %s max_concurrent = %d", agent.Name, agent.MaxConcurrent)
		}
		if len(agent.ContextFiles) != 1 || agent.ContextFiles[0] != contextPath {
			t.Fatalf("agent %s context files = %v", agent.Name, agent.ContextFiles)
		}
		if !strings.Contains(agent.Context, "Shared operator context.") {
			t.Fatalf("agent %s context missing shared context: %q", agent.Name, agent.Context)
		}
		if !strings.Contains(agent.Context, "Do not use extra turns only to confirm that nothing has changed.") {
			t.Fatalf("agent %s context missing global guidance: %q", agent.Name, agent.Context)
		}
		if !strings.Contains(agent.Context, "Never print, paste, log, summarize, quote, or intentionally expose secrets.") {
			t.Fatalf("agent %s context missing global secret guidance: %q", agent.Name, agent.Context)
		}
		if !strings.Contains(agent.Context, "Prefer the narrowest verification that proves the change worked.") {
			t.Fatalf("agent %s context missing configured global context: %q", agent.Name, agent.Context)
		}
	}
}

func TestLoadPrependsGlobalAgentContext(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	promptDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "prompt.md"), []byte("Issue {{.Issue.Identifier}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	contextPath := filepath.Join(root, "context.md")
	if err := os.WriteFile(contextPath, []byte("Local pack context."), 0o644); err != nil {
		t.Fatalf("write context: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  agent_context: |
    - Prefer the narrowest verification that proves the change worked.
sources:
  - name: project-a
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project-a
    filter:
      labels: [ready]
    agent_type: code-pr
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: agents/code-pr/prompt.md
    approval_policy: auto
    max_concurrent: 1
    context_files: [context.md]
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.AgentTypes) != 1 {
		t.Fatalf("agent types = %d", len(cfg.AgentTypes))
	}
	context := cfg.AgentTypes[0].Context
	if !strings.Contains(context, "If the requested outcome is already achieved") {
		t.Fatalf("context missing global stop guidance: %q", context)
	}
	if !strings.Contains(context, "Do not use extra turns only to confirm that nothing has changed.") {
		t.Fatalf("context missing global no-recheck guidance: %q", context)
	}
	if !strings.Contains(context, "Never print, paste, log, summarize, quote, or intentionally expose secrets.") {
		t.Fatalf("context missing global secret guidance: %q", context)
	}
	if !strings.Contains(context, "Prefer the narrowest verification that proves the change worked.") {
		t.Fatalf("context missing configured global context: %q", context)
	}
	if !strings.Contains(context, "Local pack context.") {
		t.Fatalf("context missing local context: %q", context)
	}
}

func TestLoadAgentDefaultsOverridePackDefaults(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "gitlab-secret")

	root := t.TempDir()
	packDir := filepath.Join(root, "packs", "repo-maintainer")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatalf("mkdir pack dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "prompt.md"), []byte("Prompt"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "context.md"), []byte("Context"), 0o644); err != nil {
		t.Fatalf("write context: %v", err)
	}
	rawPack := `
name: repo-maintainer
harness: claude-code
workspace: git-clone
prompt: prompt.md
approval_policy: manual
max_concurrent: 1
context_files: [context.md]
`
	if err := os.WriteFile(filepath.Join(packDir, "agent.yaml"), []byte(rawPack), 0o644); err != nil {
		t.Fatalf("write pack: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
agent_packs_dir: ./packs
agent_defaults:
  approval_policy: auto
  max_concurrent: 2
sources:
  - name: project-a
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project-a
    agent_type: repo-maintainer
agent_types:
  - name: repo-maintainer
    agent_pack: repo-maintainer
workspace:
  root: ./workspaces
logging:
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	agent := cfg.AgentTypes[0]
	if got := agent.ApprovalPolicy; got != "auto" {
		t.Fatalf("approval policy = %q", got)
	}
	if got := agent.MaxConcurrent; got != 2 {
		t.Fatalf("max concurrent = %d", got)
	}
}

func TestTokenEnvAcceptsDollarPrefix(t *testing.T) {
	t.Setenv("MY_GITLAB_TOKEN", "secret-from-dollar")

	root := t.TempDir()
	promptDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "prompt.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
sources:
  - name: test
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: $MY_GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: agents/code-pr/prompt.md
    approval_policy: auto
    max_concurrent: 1
workspace:
  root: ./workspaces
logging:
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Sources[0].Connection.Token; got != "secret-from-dollar" {
		t.Fatalf("token = %q, want %q", got, "secret-from-dollar")
	}
}

func TestLoadAcceptsPerSourceLabelPrefix(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret")

	root := t.TempDir()
	promptPath := filepath.Join(root, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
defaults:
  poll_interval: 5s
  max_concurrent_global: 1
  label_prefix: maestro
sources:
  - name: project-a
    tracker: gitlab
    label_prefix: fwr
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project-a
    filter:
      labels: [agent:ready]
    agent_type: code-pr
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: ` + promptPath + `
    approval_policy: auto
    max_concurrent: 1
workspace:
  root: ./workspaces
logging:
  dir: ./logs
`
	if err := os.WriteFile(configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := cfg.Sources[0].LabelPrefix; got != "fwr" {
		t.Fatalf("label prefix = %q, want fwr", got)
	}
}

func TestResolveLifecycleTransitionsMergesDefaultsAndSourceOverrides(t *testing.T) {
	dispatch := config.ResolveDispatchTransition(
		&config.DispatchTransition{State: "In Progress"},
		&config.DispatchTransition{},
	)
	if dispatch == nil || dispatch.State != "In Progress" {
		t.Fatalf("dispatch transition = %+v, want inherited state", dispatch)
	}

	complete := config.ResolveLifecycleTransition(
		&config.LifecycleTransition{
			AddLabels:    []string{"maestro:review"},
			RemoveLabels: []string{"maestro:coding"},
			State:        "In Review",
		},
		&config.LifecycleTransition{
			State: "Human Review",
		},
	)
	if complete == nil {
		t.Fatal("complete transition = nil, want merged transition")
	}
	if got, want := complete.State, "Human Review"; got != want {
		t.Fatalf("complete state = %q, want %q", got, want)
	}
	if got, want := complete.AddLabels, []string{"maestro:review"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("complete add_labels = %v, want %v", got, want)
	}
	if got, want := complete.RemoveLabels, []string{"maestro:coding"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("complete remove_labels = %v, want %v", got, want)
	}

	failure := config.ResolveLifecycleTransition(
		&config.LifecycleTransition{
			AddLabels:    []string{"maestro:failed"},
			RemoveLabels: []string{"maestro:coding"},
		},
		&config.LifecycleTransition{
			AddLabels: []string{},
		},
	)
	if failure == nil {
		t.Fatal("failure transition = nil, want merged transition")
	}
	if failure.AddLabels == nil || len(failure.AddLabels) != 0 {
		t.Fatalf("failure add_labels = %v, want explicit empty override", failure.AddLabels)
	}
	if got, want := failure.RemoveLabels, []string{"maestro:coding"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("failure remove_labels = %v, want %v", got, want)
	}
}

func TestResolveHarnessConfigAllowsExplicitEmptyExtraArgsOverride(t *testing.T) {
	codex := config.ResolveCodexConfig(
		&config.CodexConfig{ExtraArgs: []string{"--search"}},
		&config.CodexConfig{ExtraArgs: []string{}},
	)
	if codex.ExtraArgs == nil || len(codex.ExtraArgs) != 0 {
		t.Fatalf("codex extra_args = %v, want explicit empty override", codex.ExtraArgs)
	}

	claude := config.ResolveClaudeConfig(
		&config.ClaudeConfig{ExtraArgs: []string{"--verbose"}},
		&config.ClaudeConfig{ExtraArgs: []string{}},
	)
	if claude.ExtraArgs == nil || len(claude.ExtraArgs) != 0 {
		t.Fatalf("claude extra_args = %v, want explicit empty override", claude.ExtraArgs)
	}
}
