package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/harness"
	"github.com/tjohnson/maestro/internal/orchestrator"
	"github.com/tjohnson/maestro/internal/testutil"
	"github.com/tjohnson/maestro/internal/workspace"
)

type blockingReplyHarness struct {
	*testutil.FakeHarness
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *blockingReplyHarness) Reply(ctx context.Context, reply harness.MessageReply) error {
	h.once.Do(func() {
		close(h.started)
	})
	<-h.release
	return h.FakeHarness.Reply(ctx, reply)
}

func TestServiceTracksAndResolvesMessageRequests(t *testing.T) {
	cfg := testConfig(t)
	repoURL := createGitRepo(t)

	fakeTracker := &testutil.FakeTracker{
		Issues: singleIssue(cfg, repoURL, "gitlab:team/project#57", "team/project#57"),
	}
	fakeHarness := &testutil.FakeHarness{
		WaitBlock: make(chan struct{}),
		MessageCh: make(chan harness.MessageRequest, 1),
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
	fakeHarness.MessageCh <- harness.MessageRequest{
		RequestID:   "msg-1",
		RunID:       runID,
		Summary:     "Need clarification",
		Body:        "Should I update the API contract or only the UI copy?",
		RequestedAt: time.Now().Add(-time.Minute),
	}

	waitFor(t, 2*time.Second, func() bool {
		snapshot := svc.Snapshot()
		return len(snapshot.PendingMessages) == 1 && snapshot.ActiveRun != nil
	})

	if err := svc.ResolveMessage("msg-1", "Update the API contract too.", "test"); err != nil {
		t.Fatalf("resolve message: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		snapshot := svc.Snapshot()
		return len(snapshot.PendingMessages) == 0 && len(snapshot.MessageHistory) == 1 && snapshot.ActiveRun != nil && snapshot.ActiveRun.Status == domain.RunStatusActive
	})

	close(fakeHarness.WaitBlock)
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("run service: %v", err)
	}
	if len(fakeHarness.Replies) != 1 || fakeHarness.Replies[0].Reply != "Update the API contract too." {
		t.Fatalf("message replies = %+v", fakeHarness.Replies)
	}
}

func TestServiceRestoresFailedMessageResolve(t *testing.T) {
	cfg := testConfig(t)
	repoURL := createGitRepo(t)

	fakeTracker := &testutil.FakeTracker{
		Issues: singleIssue(cfg, repoURL, "gitlab:team/project#56m", "team/project#56m"),
	}
	fakeHarness := &testutil.FakeHarness{
		WaitBlock: make(chan struct{}),
		MessageCh: make(chan harness.MessageRequest, 1),
		ReplyErr:  errors.New("message transport failed"),
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
	fakeHarness.MessageCh <- harness.MessageRequest{
		RequestID:   "msg-fail",
		RunID:       runID,
		Summary:     "Need clarification",
		Body:        "Question?",
		RequestedAt: time.Now(),
	}

	waitFor(t, 2*time.Second, func() bool {
		return len(svc.Snapshot().PendingMessages) == 1
	})

	if err := svc.ResolveMessage("msg-fail", "answer", "test"); err == nil {
		t.Fatal("expected message reply failure")
	}

	snapshot := svc.Snapshot()
	if len(snapshot.PendingMessages) != 1 {
		t.Fatalf("pending messages = %d, want 1", len(snapshot.PendingMessages))
	}
	if !snapshot.PendingMessages[0].Resolvable {
		t.Fatal("expected failed message request to remain resolvable")
	}
	if len(snapshot.MessageHistory) != 0 {
		t.Fatalf("message history = %d, want 0", len(snapshot.MessageHistory))
	}

	close(fakeHarness.WaitBlock)
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("run service: %v", err)
	}
}

func TestServiceRejectsConcurrentMessageResolve(t *testing.T) {
	cfg := testConfig(t)
	repoURL := createGitRepo(t)

	fakeTracker := &testutil.FakeTracker{
		Issues: singleIssue(cfg, repoURL, "gitlab:team/project#56n", "team/project#56n"),
	}
	fakeHarness := &blockingReplyHarness{
		FakeHarness: &testutil.FakeHarness{
			WaitBlock: make(chan struct{}),
			MessageCh: make(chan harness.MessageRequest, 1),
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
	fakeHarness.MessageCh <- harness.MessageRequest{
		RequestID:   "msg-race",
		RunID:       runID,
		Summary:     "Need clarification",
		Body:        "Question?",
		RequestedAt: time.Now(),
	}

	waitFor(t, 2*time.Second, func() bool {
		return len(svc.Snapshot().PendingMessages) == 1
	})

	firstErrCh := make(chan error, 1)
	go func() {
		firstErrCh <- svc.ResolveMessage("msg-race", "answer", "test")
	}()

	<-fakeHarness.started

	secondErr := svc.ResolveMessage("msg-race", "answer", "test")
	if secondErr == nil || !strings.Contains(secondErr.Error(), "already being resolved") {
		t.Fatalf("second resolve error = %v, want already being resolved", secondErr)
	}

	snapshot := svc.Snapshot()
	if len(snapshot.PendingMessages) != 1 {
		t.Fatalf("pending messages = %d, want 1", len(snapshot.PendingMessages))
	}
	if snapshot.PendingMessages[0].Resolvable {
		t.Fatal("expected message to be marked non-resolvable while first resolve is in progress")
	}

	close(fakeHarness.release)
	if err := <-firstErrCh; err != nil {
		t.Fatalf("first resolve message: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		snapshot := svc.Snapshot()
		return len(snapshot.PendingMessages) == 0 && len(snapshot.MessageHistory) == 1
	})

	close(fakeHarness.WaitBlock)
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("run service: %v", err)
	}
	if len(fakeHarness.Replies) != 1 {
		t.Fatalf("message replies = %+v, want exactly 1", fakeHarness.Replies)
	}
}
