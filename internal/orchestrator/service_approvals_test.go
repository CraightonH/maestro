package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/harness"
	"github.com/tjohnson/maestro/internal/orchestrator"
	"github.com/tjohnson/maestro/internal/state"
	"github.com/tjohnson/maestro/internal/testutil"
	"github.com/tjohnson/maestro/internal/workspace"
)

func TestServiceTracksAndResolvesApprovalRequests(t *testing.T) {
	cfg := testConfig(t)
	cfg.AgentTypes[0].ApprovalPolicy = "manual"
	repoURL := createGitRepo(t)

	fakeTracker := &testutil.FakeTracker{
		Issues: singleIssue(cfg, repoURL, "gitlab:team/project#55", "team/project#55"),
	}
	fakeHarness := &testutil.FakeHarness{
		WaitBlock:  make(chan struct{}),
		ApprovalCh: make(chan harness.ApprovalRequest, 1),
	}

	svc, err := orchestrator.NewServiceWithDeps(cfg, testLogger(), orchestrator.Dependencies{
		Tracker:   fakeTracker,
		Harness:   fakeHarness,
		Workspace: workspace.NewManager(cfg.Workspace.Root),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.Run(ctx)
	}()

	waitFor(t, 2*time.Second, func() bool {
		return len(fakeHarness.StartedRuns) == 1 && svc.Snapshot().ActiveRun != nil
	})

	runID := svc.Snapshot().ActiveRun.ID
	fakeHarness.ApprovalCh <- harness.ApprovalRequest{
		RequestID:      "req-1",
		RunID:          runID,
		ToolName:       "write_file",
		ToolInput:      "create APPROVAL.txt",
		ApprovalPolicy: "manual",
		RequestedAt:    time.Now().Add(-time.Minute),
	}

	waitFor(t, 2*time.Second, func() bool {
		return len(svc.Snapshot().PendingApprovals) == 1
	})

	if err := svc.ResolveApproval("req-1", "approve"); err != nil {
		t.Fatalf("resolve approval: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		snapshot := svc.Snapshot()
		return len(snapshot.PendingApprovals) == 0 && len(snapshot.ApprovalHistory) == 1 && snapshot.ActiveRun != nil && snapshot.ActiveRun.ApprovalState == domain.ApprovalStateApproved
	})

	close(fakeHarness.WaitBlock)
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("run service: %v", err)
	}
	if len(fakeHarness.Decisions) != 1 || fakeHarness.Decisions[0].Decision != "approve" {
		t.Fatalf("approval decisions = %+v", fakeHarness.Decisions)
	}
}

func TestServiceKeepsApprovalPendingWhenApprovalFails(t *testing.T) {
	cfg := testConfig(t)
	cfg.AgentTypes[0].ApprovalPolicy = "manual"
	repoURL := createGitRepo(t)

	fakeTracker := &testutil.FakeTracker{
		Issues: singleIssue(cfg, repoURL, "gitlab:team/project#56", "team/project#56"),
	}
	fakeHarness := &testutil.FakeHarness{
		WaitBlock:  make(chan struct{}),
		ApprovalCh: make(chan harness.ApprovalRequest, 1),
		ApproveErr: errors.New("approval transport failed"),
	}

	svc, err := orchestrator.NewServiceWithDeps(cfg, testLogger(), orchestrator.Dependencies{
		Tracker:   fakeTracker,
		Harness:   fakeHarness,
		Workspace: workspace.NewManager(cfg.Workspace.Root),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.Run(ctx)
	}()

	waitFor(t, 2*time.Second, func() bool {
		return len(fakeHarness.StartedRuns) == 1 && svc.Snapshot().ActiveRun != nil
	})

	runID := svc.Snapshot().ActiveRun.ID
	fakeHarness.ApprovalCh <- harness.ApprovalRequest{
		RequestID:      "req-fail",
		RunID:          runID,
		ToolName:       "shell",
		ToolInput:      "rm -rf /tmp/demo",
		ApprovalPolicy: "manual",
		RequestedAt:    time.Now(),
	}

	waitFor(t, 2*time.Second, func() bool {
		return len(svc.Snapshot().PendingApprovals) == 1
	})

	if err := svc.ResolveApproval("req-fail", "approve"); err == nil {
		t.Fatal("expected approval failure")
	}

	snapshot := svc.Snapshot()
	if len(snapshot.PendingApprovals) != 1 {
		t.Fatalf("pending approvals = %d, want 1", len(snapshot.PendingApprovals))
	}
	if !snapshot.PendingApprovals[0].Resolvable {
		t.Fatal("expected failed approval request to remain resolvable")
	}
	if len(snapshot.ApprovalHistory) != 0 {
		t.Fatalf("approval history = %d, want 0", len(snapshot.ApprovalHistory))
	}

	close(fakeHarness.WaitBlock)
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("run service: %v", err)
	}
}

func TestServiceRejectsConcurrentApprovalResolve(t *testing.T) {
	cfg := testConfig(t)
	cfg.AgentTypes[0].ApprovalPolicy = "manual"
	repoURL := createGitRepo(t)

	fakeTracker := &testutil.FakeTracker{
		Issues: singleIssue(cfg, repoURL, "gitlab:team/project#56b", "team/project#56b"),
	}
	fakeHarness := &blockingApproveHarness{
		FakeHarness: &testutil.FakeHarness{
			WaitBlock:  make(chan struct{}),
			ApprovalCh: make(chan harness.ApprovalRequest, 1),
		},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}

	svc, err := orchestrator.NewServiceWithDeps(cfg, testLogger(), orchestrator.Dependencies{
		Tracker:   fakeTracker,
		Harness:   fakeHarness,
		Workspace: workspace.NewManager(cfg.Workspace.Root),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.Run(ctx)
	}()

	waitFor(t, 2*time.Second, func() bool {
		return len(fakeHarness.StartedRuns) == 1 && svc.Snapshot().ActiveRun != nil
	})

	runID := svc.Snapshot().ActiveRun.ID
	fakeHarness.ApprovalCh <- harness.ApprovalRequest{
		RequestID:      "req-race",
		RunID:          runID,
		ToolName:       "shell",
		ToolInput:      "echo hi",
		ApprovalPolicy: "manual",
		RequestedAt:    time.Now(),
	}

	waitFor(t, 2*time.Second, func() bool {
		return len(svc.Snapshot().PendingApprovals) == 1
	})

	firstErrCh := make(chan error, 1)
	go func() {
		firstErrCh <- svc.ResolveApproval("req-race", "approve")
	}()

	<-fakeHarness.started

	secondErr := svc.ResolveApproval("req-race", "approve")
	if secondErr == nil || !strings.Contains(secondErr.Error(), "already being resolved") {
		t.Fatalf("second resolve error = %v, want already being resolved", secondErr)
	}

	snapshot := svc.Snapshot()
	if len(snapshot.PendingApprovals) != 1 {
		t.Fatalf("pending approvals = %d, want 1", len(snapshot.PendingApprovals))
	}
	if snapshot.PendingApprovals[0].Resolvable {
		t.Fatal("expected approval to be marked non-resolvable while first resolve is in progress")
	}

	close(fakeHarness.release)
	if err := <-firstErrCh; err != nil {
		t.Fatalf("first resolve approval: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		snapshot := svc.Snapshot()
		return len(snapshot.PendingApprovals) == 0 && len(snapshot.ApprovalHistory) == 1
	})

	close(fakeHarness.WaitBlock)
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("run service: %v", err)
	}
	if len(fakeHarness.Decisions) != 1 {
		t.Fatalf("approval decisions = %+v, want exactly 1", fakeHarness.Decisions)
	}
}

func TestServiceTimesOutPendingApprovalAndFailsRun(t *testing.T) {
	cfg := testConfig(t)
	cfg.AgentTypes[0].ApprovalPolicy = "manual"
	cfg.AgentTypes[0].ApprovalTimeout = config.Duration{Duration: 80 * time.Millisecond}
	repoURL := createGitRepo(t)

	fakeTracker := &testutil.FakeTracker{
		Issues: singleIssue(cfg, repoURL, "gitlab:team/project#57", "team/project#57"),
	}
	fakeHarness := &testutil.FakeHarness{
		WaitBlock:  make(chan struct{}),
		ApprovalCh: make(chan harness.ApprovalRequest, 1),
	}

	svc, err := orchestrator.NewServiceWithDeps(cfg, testLogger(), orchestrator.Dependencies{
		Tracker:   fakeTracker,
		Harness:   fakeHarness,
		Workspace: workspace.NewManager(cfg.Workspace.Root),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.Run(ctx)
	}()

	waitFor(t, 2*time.Second, func() bool {
		return len(fakeHarness.StartedRuns) == 1 && svc.Snapshot().ActiveRun != nil
	})

	runID := svc.Snapshot().ActiveRun.ID
	fakeHarness.ApprovalCh <- harness.ApprovalRequest{
		RequestID:      "req-timeout",
		RunID:          runID,
		ToolName:       "shell",
		ToolInput:      "rm -rf /tmp/demo",
		ApprovalPolicy: "manual",
		RequestedAt:    time.Now().Add(-time.Second),
	}

	waitFor(t, 2*time.Second, func() bool {
		snapshot := svc.Snapshot()
		return len(fakeHarness.StopCalls) == 1 && snapshot.ActiveRun == nil && len(snapshot.PendingApprovals) == 0 && len(snapshot.ApprovalHistory) == 1
	})

	snapshot := svc.Snapshot()
	if got := snapshot.ApprovalHistory[0].Outcome; got != "timed_out" {
		t.Fatalf("approval outcome = %q, want timed_out", got)
	}
	if got := snapshot.ApprovalHistory[0].Reason; got != "approval timeout" {
		t.Fatalf("approval reason = %q, want approval timeout", got)
	}
	if len(fakeHarness.StopCalls) != 1 || fakeHarness.StopCalls[0] != runID {
		t.Fatalf("stop calls = %+v, want [%s]", fakeHarness.StopCalls, runID)
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("run service: %v", err)
	}
}

func TestServiceRestoresApprovalHistoryAsStaleAfterRestart(t *testing.T) {
	root := t.TempDir()
	cfg := testConfigWithRoot(t, root)
	now := time.Now().UTC().Round(time.Second)
	store := state.NewStore(cfg.State.Dir)
	if err := store.Save(state.Snapshot{
		RetryQueue: map[string]state.RetryEntry{},
		Finished:   map[string]state.TerminalIssue{},
		ActiveRun: &state.PersistedRun{
			RunID:          "run-restore",
			IssueID:        "gitlab:team/project#57",
			Identifier:     "team/project#57",
			Status:         domain.RunStatusAwaiting,
			Attempt:        0,
			StartedAt:      now,
			LastActivityAt: now,
			IssueUpdatedAt: now,
		},
		PendingApprovals: []state.PersistedApprovalRequest{
			{
				RequestID:       "req-stale",
				RunID:           "run-restore",
				IssueID:         "gitlab:team/project#57",
				IssueIdentifier: "team/project#57",
				AgentName:       "coder",
				ToolName:        "shell",
				ToolInput:       "dangerous command",
				ApprovalPolicy:  "manual",
				RequestedAt:     now,
				Resolvable:      true,
			},
		},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	svc, err := orchestrator.NewServiceWithDeps(cfg, testLogger(), orchestrator.Dependencies{
		Tracker:    &testutil.FakeTracker{},
		Harness:    &testutil.FakeHarness{},
		Workspace:  workspace.NewManager(cfg.Workspace.Root),
		StateStore: store,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	snapshot := svc.Snapshot()
	if len(snapshot.PendingApprovals) != 0 {
		t.Fatalf("pending approvals = %d, want 0", len(snapshot.PendingApprovals))
	}
	if len(snapshot.ApprovalHistory) != 1 {
		t.Fatalf("approval history = %d, want 1", len(snapshot.ApprovalHistory))
	}
	if snapshot.ApprovalHistory[0].Outcome != "stale_restart" {
		t.Fatalf("approval outcome = %q", snapshot.ApprovalHistory[0].Outcome)
	}
}

func TestServiceFailsRecoveredRunWhenApprovalAlreadyTimedOut(t *testing.T) {
	root := t.TempDir()
	cfg := testConfigWithRoot(t, root)
	cfg.AgentTypes[0].ApprovalTimeout = config.Duration{Duration: time.Second}
	now := time.Now().UTC().Round(time.Second)
	store := state.NewStore(cfg.State.Dir)
	if err := store.Save(state.Snapshot{
		RetryQueue: map[string]state.RetryEntry{},
		Finished:   map[string]state.TerminalIssue{},
		ActiveRun: &state.PersistedRun{
			RunID:          "run-timeout",
			IssueID:        "gitlab:team/project#88",
			Identifier:     "team/project#88",
			Status:         domain.RunStatusAwaiting,
			Attempt:        1,
			StartedAt:      now.Add(-2 * time.Hour),
			LastActivityAt: now.Add(-2 * time.Hour),
			IssueUpdatedAt: now.Add(-2 * time.Hour),
		},
		PendingApprovals: []state.PersistedApprovalRequest{
			{
				RequestID:       "req-timeout",
				RunID:           "run-timeout",
				IssueID:         "gitlab:team/project#88",
				IssueIdentifier: "team/project#88",
				AgentName:       "coder",
				ToolName:        "shell",
				ToolInput:       "dangerous command",
				ApprovalPolicy:  "manual",
				RequestedAt:     now.Add(-2 * time.Minute),
				Resolvable:      true,
			},
		},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	svc, err := orchestrator.NewServiceWithDeps(cfg, testLogger(), orchestrator.Dependencies{
		Tracker:    &testutil.FakeTracker{},
		Harness:    &testutil.FakeHarness{},
		Workspace:  workspace.NewManager(cfg.Workspace.Root),
		StateStore: store,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	snapshot := svc.Snapshot()
	if snapshot.ActiveRun != nil {
		t.Fatalf("active run = %+v, want nil", snapshot.ActiveRun)
	}
	if len(snapshot.Retries) != 0 {
		t.Fatalf("retries = %d, want 0", len(snapshot.Retries))
	}
	if len(snapshot.PendingApprovals) != 0 {
		t.Fatalf("pending approvals = %d, want 0", len(snapshot.PendingApprovals))
	}
	if len(snapshot.ApprovalHistory) != 1 {
		t.Fatalf("approval history = %d, want 1", len(snapshot.ApprovalHistory))
	}
	if got := snapshot.ApprovalHistory[0].Outcome; got != "timed_out" {
		t.Fatalf("approval outcome = %q, want timed_out", got)
	}

	persisted, err := store.Load()
	if err != nil {
		t.Fatalf("load persisted state: %v", err)
	}
	finished, ok := persisted.Finished["gitlab:team/project#88"]
	if !ok {
		t.Fatal("expected finished issue after approval timeout recovery")
	}
	if finished.Status != domain.RunStatusFailed {
		t.Fatalf("finished status = %s, want %s", finished.Status, domain.RunStatusFailed)
	}
	if !strings.Contains(finished.Error, "approval timeout") {
		t.Fatalf("finished error = %q, want approval timeout", finished.Error)
	}
}

type blockingApproveHarness struct {
	*testutil.FakeHarness
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingApproveHarness) Approve(ctx context.Context, decision harness.ApprovalDecision) error {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return b.FakeHarness.Approve(ctx, decision)
}
