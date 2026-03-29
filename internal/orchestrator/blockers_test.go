package orchestrator

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/testutil"
	"github.com/tjohnson/maestro/internal/workspace"
)

type trackerWithoutBlockers struct {
	issue domain.Issue
}

func (t trackerWithoutBlockers) Kind() string { return "without-blockers" }

func (t trackerWithoutBlockers) Poll(ctx context.Context) ([]domain.Issue, error) {
	return []domain.Issue{t.issue}, nil
}

func (t trackerWithoutBlockers) Get(ctx context.Context, issueID string) (domain.Issue, error) {
	if t.issue.ID != issueID {
		return domain.Issue{}, errors.New("not found")
	}
	return t.issue, nil
}

func (t trackerWithoutBlockers) PostOperationalComment(ctx context.Context, issueID string, body string) error {
	return nil
}

func (t trackerWithoutBlockers) AddLifecycleLabel(ctx context.Context, issueID string, label string) error {
	return nil
}

func (t trackerWithoutBlockers) RemoveLifecycleLabel(ctx context.Context, issueID string, label string) error {
	return nil
}

func (t trackerWithoutBlockers) UpdateIssueState(ctx context.Context, issueID string, stateName string) error {
	return nil
}

func TestTickSkipsIssueBlockedByOpenDependency(t *testing.T) {
	cfg := blockerTestConfig(t)
	tracker := &testutil.FakeTracker{
		Issues: []domain.Issue{{
			ID:          "linear:OPS-100",
			Identifier:  "OPS-100",
			Title:       "Blocked work",
			State:       "todo",
			SourceName:  cfg.Sources[0].Name,
			TrackerKind: "linear",
			Blockers: []domain.Issue{{
				ID:          "linear:OPS-42",
				Identifier:  "OPS-42",
				Title:       "Dependency",
				State:       "in progress",
				TrackerKind: "linear",
				Meta: map[string]string{
					"state_type": "started",
				},
			}},
		}},
	}
	harness := &testutil.FakeHarness{}

	svc, err := NewServiceWithDeps(cfg, blockerTestLogger(), Dependencies{
		Tracker:   tracker,
		Harness:   harness,
		Workspace: workspace.NewManager(cfg.Workspace.Root),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if err := svc.tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if got := len(harness.StartedRuns); got != 0 {
		t.Fatalf("started runs = %d, want 0", got)
	}
	if !snapshotHasMessage(svc.Snapshot(), "skipping OPS-100 because it is blocked by OPS-42 (in progress)") {
		t.Fatalf("recent events = %+v, want blocker skip message", svc.Snapshot().RecentEvents)
	}
}

func TestTickDispatchesIssueWhenBlockersAreTerminal(t *testing.T) {
	cfg := blockerTestConfig(t)
	tracker := &testutil.FakeTracker{
		Issues: []domain.Issue{{
			ID:          "linear:OPS-101",
			Identifier:  "OPS-101",
			Title:       "Ready after dependency",
			State:       "todo",
			SourceName:  cfg.Sources[0].Name,
			TrackerKind: "linear",
			Blockers: []domain.Issue{{
				ID:          "linear:OPS-41",
				Identifier:  "OPS-41",
				Title:       "Completed dependency",
				State:       "done",
				TrackerKind: "linear",
				Meta: map[string]string{
					"state_type": "completed",
				},
			}},
		}},
	}
	waitBlock := make(chan struct{})
	harness := &testutil.FakeHarness{WaitBlock: waitBlock}

	svc, err := NewServiceWithDeps(cfg, blockerTestLogger(), Dependencies{
		Tracker:   tracker,
		Harness:   harness,
		Workspace: workspace.NewManager(cfg.Workspace.Root),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if err := svc.tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	waitForCondition(t, time.Second, func() bool { return len(harness.StartedRuns) == 1 })
	close(waitBlock)
	waitForCondition(t, time.Second, func() bool { return svc.Snapshot().ActiveRun == nil })
	svc.runWG.Wait()
}

func TestTickDegradesCleanlyWhenTrackerDoesNotExposeBlockers(t *testing.T) {
	cfg := blockerTestConfig(t)
	issue := domain.Issue{
		ID:          "fake:OPS-102",
		Identifier:  "OPS-102",
		Title:       "Unsupported tracker",
		State:       "todo",
		SourceName:  cfg.Sources[0].Name,
		TrackerKind: "fake",
	}
	waitBlock := make(chan struct{})
	harness := &testutil.FakeHarness{WaitBlock: waitBlock}

	svc, err := NewServiceWithDeps(cfg, blockerTestLogger(), Dependencies{
		Tracker:   trackerWithoutBlockers{issue: issue},
		Harness:   harness,
		Workspace: workspace.NewManager(cfg.Workspace.Root),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if err := svc.tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	waitForCondition(t, time.Second, func() bool { return len(harness.StartedRuns) == 1 })
	close(waitBlock)
	waitForCondition(t, time.Second, func() bool { return svc.Snapshot().ActiveRun == nil })
	svc.runWG.Wait()
}

func blockerTestConfig(t *testing.T) *config.Config {
	t.Helper()

	root := t.TempDir()
	promptPath := filepath.Join(root, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Issue {{.Issue.Identifier}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	return &config.Config{
		Defaults: config.DefaultsConfig{
			PollInterval:        config.Duration{Duration: time.Minute},
			MaxConcurrentGlobal: 1,
			StallTimeout:        config.Duration{Duration: time.Minute},
			LabelPrefix:         "maestro",
		},
		Sources: []config.SourceConfig{{
			Name:         "ops",
			Tracker:      "linear",
			AgentType:    "code-pr",
			PollInterval: config.Duration{Duration: time.Minute},
			LabelPrefix:  "maestro",
		}},
		AgentTypes: []config.AgentTypeConfig{{
			Name:            "code-pr",
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
			RetryBase:       config.Duration{Duration: 20 * time.Millisecond},
			MaxRetryBackoff: config.Duration{Duration: 20 * time.Millisecond},
			MaxAttempts:     3,
		},
		Hooks: config.HooksConfig{
			Timeout: config.Duration{Duration: 30 * time.Second},
		},
	}
}

func blockerTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func snapshotHasMessage(snapshot Snapshot, want string) bool {
	for _, event := range snapshot.RecentEvents {
		if strings.Contains(event.Message, want) {
			return true
		}
	}
	return false
}
