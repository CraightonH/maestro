package orchestrator

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/state"
	"github.com/tjohnson/maestro/internal/testutil"
)

func TestServiceDryRunPreviewsPollDispatchWithoutSideEffects(t *testing.T) {
	cfg := dryRunTestConfig(t)
	cfg.AgentTypes[0].Workspace = "none"
	cfg.Defaults.OnDispatch = &config.DispatchTransition{
		AddLabels:    []string{"maestro:coding"},
		RemoveLabels: []string{"maestro:retry"},
		State:        "In Progress",
	}

	tracker := &testutil.FakeTracker{
		Issues: []domain.Issue{{
			ID:          "linear:OPS-42",
			Identifier:  "OPS-42",
			Title:       "Preview this",
			State:       "Todo",
			SourceName:  cfg.Sources[0].Name,
			TrackerKind: "linear",
			Labels:      []string{"product"},
		}},
	}

	svc, warnings, err := newDryRunService(cfg, dryRunTestLogger(), tracker)
	if err != nil {
		t.Fatalf("new dry-run service: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %+v, want none", warnings)
	}

	preview, err := svc.dryRun(context.Background())
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}

	if !preview.MatchedIssue {
		t.Fatalf("matched issue = false, want true")
	}
	if got, want := preview.IssueIdentifier, "OPS-42"; got != want {
		t.Fatalf("issue identifier = %q, want %q", got, want)
	}
	if got, want := preview.CandidateSource, "poll"; got != want {
		t.Fatalf("candidate source = %q, want %q", got, want)
	}
	if got, want := preview.Attempt, 0; got != want {
		t.Fatalf("attempt = %d, want %d", got, want)
	}
	if got, want := preview.WorkspacePath, filepath.Join(cfg.Workspace.Root, "OPS-42"); got != want {
		t.Fatalf("workspace path = %q, want %q", got, want)
	}
	if preview.WorkspaceBranch != "" {
		t.Fatalf("workspace branch = %q, want empty", preview.WorkspaceBranch)
	}
	if !strings.Contains(preview.PromptPreview, "Issue OPS-42 attempt 0") {
		t.Fatalf("prompt preview = %q, want rendered prompt", preview.PromptPreview)
	}
	if got := preview.Lifecycle.State; got != "In Progress" {
		t.Fatalf("lifecycle state = %q, want In Progress", got)
	}
	if len(preview.Lifecycle.AddLabels) != 1 || preview.Lifecycle.AddLabels[0] != "maestro:coding" {
		t.Fatalf("lifecycle add = %+v", preview.Lifecycle.AddLabels)
	}
	if len(preview.Lifecycle.RemoveLabels) != 1 || preview.Lifecycle.RemoveLabels[0] != "maestro:retry" {
		t.Fatalf("lifecycle remove = %+v", preview.Lifecycle.RemoveLabels)
	}
	if _, err := os.Stat(preview.WorkspacePath); !os.IsNotExist(err) {
		t.Fatalf("workspace stat err = %v, want not exists", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.State.Dir, "runs.json")); !os.IsNotExist(err) {
		t.Fatalf("state file stat err = %v, want not exists", err)
	}
	issue, err := tracker.Get(context.Background(), "linear:OPS-42")
	if err != nil {
		t.Fatalf("tracker get: %v", err)
	}
	if got, want := issue.State, "Todo"; got != want {
		t.Fatalf("tracker state = %q, want %q", got, want)
	}
	if len(issue.Labels) != 1 || issue.Labels[0] != "product" {
		t.Fatalf("tracker labels = %+v, want unchanged", issue.Labels)
	}
}

func TestServiceDryRunUsesRecoveredRetryAttempt(t *testing.T) {
	cfg := dryRunTestConfig(t)
	cfg.AgentTypes[0].Workspace = "none"
	now := time.Now().UTC().Round(time.Second)
	store := state.NewStore(cfg.State.Dir)
	if err := store.Save(state.Snapshot{
		ActiveRun: &state.PersistedRun{
			RunID:          "run-1",
			IssueID:        "linear:OPS-77",
			Identifier:     "OPS-77",
			Status:         domain.RunStatusActive,
			Attempt:        0,
			StartedAt:      now.Add(-time.Minute),
			LastActivityAt: now.Add(-time.Minute),
			IssueUpdatedAt: now,
		},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	tracker := &testutil.FakeTracker{
		Issues: []domain.Issue{{
			ID:          "linear:OPS-77",
			Identifier:  "OPS-77",
			Title:       "Recovered run",
			SourceName:  cfg.Sources[0].Name,
			TrackerKind: "linear",
		}},
	}

	svc, _, err := newDryRunService(cfg, dryRunTestLogger(), tracker)
	if err != nil {
		t.Fatalf("new dry-run service: %v", err)
	}

	preview, err := svc.dryRun(context.Background())
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}

	if !preview.MatchedIssue {
		t.Fatalf("matched issue = false, want true")
	}
	if got, want := preview.CandidateSource, "retry"; got != want {
		t.Fatalf("candidate source = %q, want %q", got, want)
	}
	if got, want := preview.Attempt, 1; got != want {
		t.Fatalf("attempt = %d, want %d", got, want)
	}
	if !strings.Contains(preview.PromptPreview, "Issue OPS-77 attempt 1") {
		t.Fatalf("prompt preview = %q, want recovered retry attempt", preview.PromptPreview)
	}
}

func TestServiceDryRunReportsFutureRetryAsIneligible(t *testing.T) {
	cfg := dryRunTestConfig(t)
	cfg.AgentTypes[0].Workspace = "none"
	dueAt := time.Now().UTC().Add(time.Hour).Round(time.Second)
	store := state.NewStore(cfg.State.Dir)
	if err := store.Save(state.Snapshot{
		RetryQueue: map[string]state.RetryEntry{
			"linear:OPS-88": {
				IssueID:    "linear:OPS-88",
				Identifier: "OPS-88",
				Attempt:    2,
				DueAt:      dueAt,
			},
		},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	tracker := &testutil.FakeTracker{
		Issues: []domain.Issue{{
			ID:          "linear:OPS-88",
			Identifier:  "OPS-88",
			Title:       "Wait for retry",
			SourceName:  cfg.Sources[0].Name,
			TrackerKind: "linear",
		}},
	}

	svc, _, err := newDryRunService(cfg, dryRunTestLogger(), tracker)
	if err != nil {
		t.Fatalf("new dry-run service: %v", err)
	}

	preview, err := svc.dryRun(context.Background())
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}

	if preview.MatchedIssue {
		t.Fatalf("matched issue = true, want false")
	}
	if got, want := preview.Reason, "all polled issues were suppressed by local recovery state"; got != want {
		t.Fatalf("reason = %q, want %q", got, want)
	}
	if len(preview.Evaluations) != 1 {
		t.Fatalf("evaluations = %+v, want one skip", preview.Evaluations)
	}
	if preview.Evaluations[0].Outcome != "skipped" {
		t.Fatalf("evaluation outcome = %q, want skipped", preview.Evaluations[0].Outcome)
	}
	if !strings.Contains(preview.Evaluations[0].Reason, dueAt.Format(time.RFC3339)) {
		t.Fatalf("evaluation reason = %q, want retry due time", preview.Evaluations[0].Reason)
	}
}

func TestServiceDryRunFailsGitClonePreviewWithoutRepoURL(t *testing.T) {
	cfg := dryRunTestConfig(t)
	cfg.AgentTypes[0].Workspace = "git-clone"

	tracker := &testutil.FakeTracker{
		Issues: []domain.Issue{{
			ID:          "linear:OPS-99",
			Identifier:  "OPS-99",
			Title:       "Missing repo metadata",
			SourceName:  cfg.Sources[0].Name,
			TrackerKind: "linear",
		}},
	}

	svc, _, err := newDryRunService(cfg, dryRunTestLogger(), tracker)
	if err != nil {
		t.Fatalf("new dry-run service: %v", err)
	}

	_, err = svc.dryRun(context.Background())
	if err == nil {
		t.Fatal("expected dry-run preview error")
	}
	if !strings.Contains(err.Error(), `missing repo_url metadata`) {
		t.Fatalf("dry-run error = %v, want missing repo_url metadata", err)
	}
}

func dryRunTestConfig(t *testing.T) *config.Config {
	t.Helper()

	root := t.TempDir()
	promptPath := filepath.Join(root, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Issue {{.Issue.Identifier}} attempt {{.Attempt}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	return &config.Config{
		User: config.UserConfig{Name: "TJ", LinearUsername: "tjohnson"},
		Defaults: config.DefaultsConfig{
			PollInterval:        config.Duration{Duration: 20 * time.Millisecond},
			MaxConcurrentGlobal: 1,
			StallTimeout:        config.Duration{Duration: time.Minute},
			LabelPrefix:         "maestro",
		},
		Sources: []config.SourceConfig{{
			Name:         "ops",
			Tracker:      "linear",
			AgentType:    "code-pr",
			PollInterval: config.Duration{Duration: 20 * time.Millisecond},
			LabelPrefix:  "maestro",
		}},
		AgentTypes: []config.AgentTypeConfig{{
			Name:            "code-pr",
			InstanceName:    "coder",
			Harness:         "claude-code",
			Workspace:       "none",
			Prompt:          promptPath,
			ApprovalPolicy:  "auto",
			ApprovalTimeout: config.Duration{Duration: 24 * time.Hour},
			MaxConcurrent:   1,
			StallTimeout:    config.Duration{Duration: time.Minute},
		}},
		Workspace: config.WorkspaceConfig{Root: filepath.Join(root, "workspaces")},
		State: config.StateConfig{
			Dir:             filepath.Join(root, "state"),
			RetryBase:       config.Duration{Duration: time.Second},
			MaxRetryBackoff: config.Duration{Duration: time.Minute},
			MaxAttempts:     3,
		},
	}
}

func dryRunTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
