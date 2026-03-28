package orchestrator

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/harness"
	"github.com/tjohnson/maestro/internal/state"
	"github.com/tjohnson/maestro/internal/testutil"
)

func TestResolveApprovalTimesOutHarnessCallAndRestoresPendingApproval(t *testing.T) {
	prev := harnessControlTimeout
	harnessControlTimeout = 20 * time.Millisecond
	defer func() { harnessControlTimeout = prev }()

	fakeHarness := &testutil.FakeHarness{
		ApprovalCh:   make(chan harness.ApprovalRequest),
		ApproveBlock: make(chan struct{}),
	}
	svc := newControlTestService(t, fakeHarness)
	svc.approvalMgr = &approvalRouter{service: svc}
	svc.approvals["req-1"] = ApprovalView{
		RequestID:      "req-1",
		RunID:          "run-1",
		ToolName:       "write_file",
		ApprovalPolicy: "manual",
		RequestedAt:    time.Now().UTC(),
		Resolvable:     true,
	}
	svc.approvalOrder = []string{"req-1"}

	err := svc.ResolveApproval("req-1", "approve")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("resolve approval err = %v, want deadline exceeded", err)
	}

	request := svc.approvals["req-1"]
	if !request.Resolvable {
		t.Fatal("expected timed out approval to be restored as resolvable")
	}
	if len(fakeHarness.Decisions) != 0 {
		t.Fatalf("approval decisions = %+v, want none", fakeHarness.Decisions)
	}
}

func TestResolveMessageTimesOutHarnessCallAndRestoresPendingMessage(t *testing.T) {
	prev := harnessControlTimeout
	harnessControlTimeout = 20 * time.Millisecond
	defer func() { harnessControlTimeout = prev }()

	fakeHarness := &testutil.FakeHarness{
		MessageCh:  make(chan harness.MessageRequest),
		ReplyBlock: make(chan struct{}),
	}
	svc := newControlTestService(t, fakeHarness)
	svc.messageMgr = &messageRouter{service: svc}
	svc.messages["msg-1"] = MessageView{
		RequestID:   "msg-1",
		RunID:       "run-1",
		Kind:        "agent_message",
		Summary:     "Need input",
		RequestedAt: time.Now().UTC(),
		Resolvable:  true,
	}
	svc.messageOrder = []string{"msg-1"}

	err := svc.ResolveMessage("msg-1", "answer", "test")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("resolve message err = %v, want deadline exceeded", err)
	}

	request := svc.messages["msg-1"]
	if !request.Resolvable {
		t.Fatal("expected timed out message to be restored as resolvable")
	}
	if len(fakeHarness.Replies) != 0 {
		t.Fatalf("message replies = %+v, want none", fakeHarness.Replies)
	}
}

func TestShutdownTimesOutStuckHarnessStop(t *testing.T) {
	prev := harnessShutdownTimeout
	harnessShutdownTimeout = 20 * time.Millisecond
	defer func() { harnessShutdownTimeout = prev }()

	fakeHarness := &testutil.FakeHarness{
		StopBlock: make(chan struct{}),
	}
	svc := newControlTestService(t, fakeHarness)
	svc.activeRun = &domain.AgentRun{ID: "run-1"}

	start := time.Now()
	err := svc.shutdown()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown err = %v, want deadline exceeded", err)
	}
	if time.Since(start) > 250*time.Millisecond {
		t.Fatalf("shutdown took too long: %v", time.Since(start))
	}
	if len(fakeHarness.StopCalls) != 1 || fakeHarness.StopCalls[0] != "run-1" {
		t.Fatalf("stop calls = %+v, want [run-1]", fakeHarness.StopCalls)
	}
}

func newControlTestService(t *testing.T, fakeHarness *testutil.FakeHarness) *Service {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &Service{
		logger:         logger,
		source:         configSourceForControlTests(),
		harness:        fakeHarness,
		stateStore:     state.NewStore(t.TempDir()),
		finished:       map[string]state.TerminalIssue{},
		retryQueue:     map[string]state.RetryEntry{},
		pendingStops:   map[string]pendingStop{},
		approvals:      map[string]ApprovalView{},
		messages:       map[string]MessageView{},
		messageWaiters: map[string]chan string{},
		runOutputs:     map[string]*runOutputBuffer{},
	}
	svc.stateMgr = &stateManager{service: svc}
	return svc
}

func configSourceForControlTests() config.SourceConfig {
	return config.SourceConfig{Name: "test-source"}
}
