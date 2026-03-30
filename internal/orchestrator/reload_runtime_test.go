package orchestrator

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/state"
)

func TestReloadWatchPathsIncludeConfigAndPackAssets(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	packDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatalf("mkdir pack dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "agent.yaml"), []byte("name: code-pr\ndescription: test\nharness: claude-code\nworkspace: git-clone\nprompt: prompt.md\napproval_policy: auto\nmax_concurrent: 1\ncontext_files: [context.md]\n"), 0o644); err != nil {
		t.Fatalf("write pack yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "prompt.md"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "context.md"), []byte("context"), 0o644); err != nil {
		t.Fatalf("write context: %v", err)
	}

	cfg := loadReloadTestConfig(t, filepath.Join(root, "maestro.yaml"), `
agent_packs_dir: agents
defaults:
  max_concurrent_global: 1
sources:
  - name: gitlab-a
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
    agent_pack: code-pr
`)

	paths := reloadWatchPaths(cfg)
	assertReloadContainsPath(t, paths, cfg.ConfigPath)
	assertReloadContainsPath(t, paths, packDir)
	assertReloadContainsPath(t, paths, filepath.Join(packDir, "prompt.md"))
}

func TestPlanReloadRestartsWhenPackPromptChanges(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	packDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatalf("mkdir pack dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "agent.yaml"), []byte("name: code-pr\ndescription: test\nharness: claude-code\nworkspace: git-clone\nprompt: prompt.md\napproval_policy: auto\nmax_concurrent: 1\n"), 0o644); err != nil {
		t.Fatalf("write pack yaml: %v", err)
	}
	promptPath := filepath.Join(packDir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("first prompt"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	raw := `
agent_packs_dir: agents
defaults:
  max_concurrent_global: 1
sources:
  - name: gitlab-a
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
    agent_pack: code-pr
`
	current := loadReloadTestConfig(t, configPath, raw)
	currentSpecs, err := buildReloadServiceSpecs(current)
	if err != nil {
		t.Fatalf("build current specs: %v", err)
	}
	if err := os.WriteFile(promptPath, []byte("second prompt"), 0o644); err != nil {
		t.Fatalf("update prompt: %v", err)
	}
	desired := loadReloadTestConfig(t, configPath, raw)

	plan, err := planReloadWithCurrentSpecs(currentSpecs, desired)
	if err != nil {
		t.Fatalf("plan reload: %v", err)
	}
	if len(plan.Transitions) != 1 {
		t.Fatalf("transitions = %d, want 1", len(plan.Transitions))
	}
	if got := plan.Transitions[0].Action; got != reloadActionRestart {
		t.Fatalf("action = %q, want restart", got)
	}
}

func TestPlanReloadClassifiesSourceTransitions(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "prompt.md"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	current := loadReloadTestConfig(t, filepath.Join(root, "current.yaml"), `
defaults:
  max_concurrent_global: 1
sources:
  - name: keep
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
  - name: change
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
  - name: remove
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
    prompt: prompt.md
    approval_policy: auto
    max_concurrent: 1
`)
	desired := loadReloadTestConfig(t, filepath.Join(root, "desired.yaml"), `
defaults:
  max_concurrent_global: 1
sources:
  - name: keep
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
  - name: change
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
    poll_interval: 5s
  - name: add
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
    prompt: prompt.md
    approval_policy: auto
    max_concurrent: 1
`)

	plan, err := planReload(current, desired)
	if err != nil {
		t.Fatalf("plan reload: %v", err)
	}

	actions := map[string]reloadAction{}
	for _, transition := range plan.Transitions {
		actions[transition.SourceName] = transition.Action
	}
	if got := actions["keep"]; got != reloadActionKeep {
		t.Fatalf("keep action = %q, want keep", got)
	}
	if got := actions["change"]; got != reloadActionRestart {
		t.Fatalf("change action = %q, want restart", got)
	}
	if got := actions["remove"]; got != reloadActionStop {
		t.Fatalf("remove action = %q, want stop", got)
	}
	if got := actions["add"]; got != reloadActionStart {
		t.Fatalf("add action = %q, want start", got)
	}
}

func TestReloadFromDiskDrainsChangedAndRemovedSourcesAndStartsNewSource(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "prompt.md"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	currentRaw := `
defaults:
  max_concurrent_global: 1
sources:
  - name: keep
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
  - name: change
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
  - name: remove
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
    prompt: prompt.md
    approval_policy: auto
    max_concurrent: 1
`
	currentCfg := loadReloadTestConfig(t, configPath, currentRaw)
	currentSpecs, err := buildReloadServiceSpecs(currentCfg)
	if err != nil {
		t.Fatalf("build current specs: %v", err)
	}

	runtime := &ReloadableRuntime{
		logger:     supervisorTestLogger(),
		shared:     newRuntimeSharedDeps(currentCfg),
		currentCfg: currentCfg,
		desired:    currentSpecs,
		slots: map[string]*reloadRuntimeSlot{
			"keep":   {spec: currentSpecs["keep"], service: reloadTestService("keep")},
			"change": {spec: currentSpecs["change"], service: reloadTestService("change")},
			"remove": {spec: currentSpecs["remove"], service: reloadTestService("remove")},
		},
		exitCh: make(chan reloadServiceExit, 10),
	}
	runtime.buildService = func(cfg *config.Config, source config.SourceConfig, logger *slog.Logger, shared *runtimeSharedDeps) (*Service, error) {
		return reloadTestService(source.Name), nil
	}

	nextRaw := `
defaults:
  max_concurrent_global: 1
sources:
  - name: keep
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
  - name: change
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
    poll_interval: 5s
  - name: add
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
    prompt: prompt.md
    approval_policy: auto
    max_concurrent: 1
`
	if err := os.WriteFile(configPath, []byte(nextRaw), 0o644); err != nil {
		t.Fatalf("write next config: %v", err)
	}

	if err := runtime.reloadFromDisk(); err != nil {
		t.Fatalf("reload from disk: %v", err)
	}

	if runtime.slots["keep"].draining {
		t.Fatal("keep source should remain active")
	}
	if !runtime.slots["change"].draining {
		t.Fatal("changed source should be draining")
	}
	if !runtime.slots["remove"].draining {
		t.Fatal("removed source should be draining")
	}
	if _, ok := runtime.slots["add"]; !ok {
		t.Fatal("added source should be installed immediately")
	}
	if _, ok := runtime.desired["remove"]; ok {
		t.Fatal("removed source should no longer be in desired config")
	}
}

func TestHandleServiceExitStartsReplacementForDrainedSource(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "prompt.md"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	currentCfg := loadReloadTestConfig(t, filepath.Join(root, "current.yaml"), `
defaults:
  max_concurrent_global: 1
sources:
  - name: change
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
    poll_interval: 5s
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: prompt.md
    approval_policy: auto
    max_concurrent: 1
`)
	desiredSpecs, err := buildReloadServiceSpecs(currentCfg)
	if err != nil {
		t.Fatalf("build desired specs: %v", err)
	}

	oldService := reloadTestService("change")
	runtime := &ReloadableRuntime{
		logger:     supervisorTestLogger(),
		shared:     newRuntimeSharedDeps(currentCfg),
		currentCfg: currentCfg,
		desired:    desiredSpecs,
		slots: map[string]*reloadRuntimeSlot{
			"change": {
				spec:     reloadServiceSpec{Source: config.SourceConfig{Name: "change"}, Agent: config.AgentTypeConfig{Name: "code-pr"}, RestartSignature: "old-signature", LiveSignature: "old-live"},
				service:  oldService,
				draining: true,
			},
		},
		exitCh: make(chan reloadServiceExit, 1),
	}
	runtime.buildService = func(cfg *config.Config, source config.SourceConfig, logger *slog.Logger, shared *runtimeSharedDeps) (*Service, error) {
		return reloadTestService(source.Name), nil
	}

	if err := runtime.handleServiceExit(reloadServiceExit{
		sourceName: "change",
		service:    oldService,
		err:        nil,
	}); err != nil {
		t.Fatalf("handle service exit: %v", err)
	}

	slot, ok := runtime.slots["change"]
	if !ok {
		t.Fatal("replacement slot missing")
	}
	if slot.draining {
		t.Fatal("replacement slot should not be draining")
	}
	if slot.spec.RestartSignature != desiredSpecs["change"].RestartSignature {
		t.Fatalf("replacement restart signature = %q, want %q", slot.spec.RestartSignature, desiredSpecs["change"].RestartSignature)
	}
}

func TestPlanReloadClassifiesConcurrencyOnlyChangesAsLiveUpdate(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "prompt.md"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	current := loadReloadTestConfig(t, filepath.Join(root, "current.yaml"), `
defaults:
  max_concurrent_global: 1
sources:
  - name: change
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
    max_active_runs: 1
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: prompt.md
    approval_policy: auto
    max_concurrent: 1
`)
	desired := loadReloadTestConfig(t, filepath.Join(root, "desired.yaml"), `
defaults:
  max_concurrent_global: 5
sources:
  - name: change
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
    max_active_runs: 3
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: prompt.md
    approval_policy: auto
    max_concurrent: 3
`)

	plan, err := planReload(current, desired)
	if err != nil {
		t.Fatalf("plan reload: %v", err)
	}
	if len(plan.Transitions) != 1 {
		t.Fatalf("transitions = %d, want 1", len(plan.Transitions))
	}
	if got := plan.Transitions[0].Action; got != reloadActionUpdate {
		t.Fatalf("action = %q, want update", got)
	}
}

func TestReloadRuntimeAppliesLiveConcurrencyUpdateWithoutDraining(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "secret-token")

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "prompt.md"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	currentCfg := loadReloadTestConfig(t, filepath.Join(root, "current.yaml"), `
defaults:
  max_concurrent_global: 1
sources:
  - name: change
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
    max_active_runs: 1
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: prompt.md
    approval_policy: auto
    max_concurrent: 1
`)
	desiredCfg := loadReloadTestConfig(t, filepath.Join(root, "desired.yaml"), `
defaults:
  max_concurrent_global: 5
sources:
  - name: change
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: [agent:ready]
    agent_type: code-pr
    max_active_runs: 3
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: prompt.md
    approval_policy: auto
    max_concurrent: 3
`)

	currentSpecs, err := buildReloadServiceSpecs(currentCfg)
	if err != nil {
		t.Fatalf("build current specs: %v", err)
	}
	plan, err := planReloadWithCurrentSpecs(currentSpecs, desiredCfg)
	if err != nil {
		t.Fatalf("plan reload: %v", err)
	}

	svc := reloadTestService("change")
	svc.agent = currentCfg.AgentTypes[0]
	svc.source = currentCfg.Sources[0]
	svc.globalMaxConcurrent = currentCfg.Defaults.MaxConcurrentGlobal
	runtime := &ReloadableRuntime{
		logger:     supervisorTestLogger(),
		shared:     newRuntimeSharedDeps(currentCfg),
		currentCfg: currentCfg,
		desired:    currentSpecs,
		slots: map[string]*reloadRuntimeSlot{
			"change": {
				spec:    currentSpecs["change"],
				service: svc,
			},
		},
	}

	runtime.shared.applyConfig(desiredCfg)
	runtime.currentCfg = desiredCfg
	runtime.desired = plan.Desired

	updated := runtime.updateSourceLiveConfig("change", plan.Desired["change"], desiredCfg.Defaults.MaxConcurrentGlobal)
	if !updated {
		t.Fatal("expected live update to apply")
	}
	if svc.IsDraining() {
		t.Fatal("service should not be draining after live update")
	}
	if got := svc.source.EffectiveMaxActiveRuns(); got != 3 {
		t.Fatalf("source max_active_runs = %d, want 3", got)
	}
	if got := svc.agent.MaxConcurrent; got != 3 {
		t.Fatalf("agent max_concurrent = %d, want 3", got)
	}
	if got := svc.globalMaxConcurrent; got != 5 {
		t.Fatalf("global max concurrent = %d, want 5", got)
	}
}

func loadReloadTestConfig(t *testing.T, path string, raw string) *config.Config {
	t.Helper()
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config %s: %v", path, err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config %s: %v", path, err)
	}
	if err := config.ValidateMVP(cfg); err != nil {
		t.Fatalf("validate config %s: %v", path, err)
	}
	return cfg
}

func reloadTestService(sourceName string) *Service {
	return &Service{
		logger:         supervisorTestLogger(),
		source:         config.SourceConfig{Name: sourceName},
		claimed:        map[string]struct{}{},
		finished:       map[string]state.TerminalIssue{},
		retryQueue:     map[string]state.RetryEntry{},
		pendingStops:   map[string]pendingStop{},
		approvals:      map[string]ApprovalView{},
		messages:       map[string]MessageView{},
		messageWaiters: map[string]chan string{},
		runOutputs:     map[string]*runOutputBuffer{},
		forcePollCh:    make(chan struct{}, 1),
		controlCh:      make(chan struct{}, 1),
	}
}

func assertReloadContainsPath(t *testing.T, paths []string, want string) {
	t.Helper()
	for _, path := range paths {
		if path == want {
			return
		}
	}
	t.Fatalf("paths %v do not include %q", paths, want)
}
