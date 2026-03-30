package orchestrator

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/testutil"
	"github.com/tjohnson/maestro/internal/workspace"
)

type blockingPollTracker struct{}

func (blockingPollTracker) Kind() string { return "blocking" }

func (blockingPollTracker) Poll(ctx context.Context) ([]domain.Issue, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blockingPollTracker) Get(ctx context.Context, issueID string) (domain.Issue, error) {
	return domain.Issue{}, errors.New("not implemented")
}

func (blockingPollTracker) PostOperationalComment(ctx context.Context, issueID string, body string) error {
	return nil
}

func (blockingPollTracker) AddLifecycleLabel(ctx context.Context, issueID string, label string) error {
	return nil
}

func (blockingPollTracker) RemoveLifecycleLabel(ctx context.Context, issueID string, label string) error {
	return nil
}

func (blockingPollTracker) UpdateIssueState(ctx context.Context, issueID string, stateName string) error {
	return nil
}

type forcePollTracker struct {
	mu     sync.Mutex
	count  int
	blocks map[int]chan struct{}
}

func (t *forcePollTracker) Kind() string { return "force-poll" }

func (t *forcePollTracker) Poll(ctx context.Context) ([]domain.Issue, error) {
	t.mu.Lock()
	t.count++
	count := t.count
	block := t.blocks[count]
	t.mu.Unlock()

	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, nil
}

func (t *forcePollTracker) Get(ctx context.Context, issueID string) (domain.Issue, error) {
	return domain.Issue{}, errors.New("not implemented")
}

func (t *forcePollTracker) PostOperationalComment(ctx context.Context, issueID string, body string) error {
	return nil
}

func (t *forcePollTracker) AddLifecycleLabel(ctx context.Context, issueID string, label string) error {
	return nil
}

func (t *forcePollTracker) RemoveLifecycleLabel(ctx context.Context, issueID string, label string) error {
	return nil
}

func (t *forcePollTracker) UpdateIssueState(ctx context.Context, issueID string, stateName string) error {
	return nil
}

func (t *forcePollTracker) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.count
}

func TestTickBoundsPollWithTimeout(t *testing.T) {
	oldTimeout := pollRequestTimeout
	pollRequestTimeout = 50 * time.Millisecond
	defer func() { pollRequestTimeout = oldTimeout }()

	root := t.TempDir()
	promptPath := filepath.Join(root, "prompt.md")
	cfg := &config.Config{
		Defaults: config.DefaultsConfig{
			PollInterval:        config.Duration{Duration: 20 * time.Millisecond},
			MaxConcurrentGlobal: 1,
			StallTimeout:        config.Duration{Duration: time.Minute},
		},
		Sources: []config.SourceConfig{{
			Name:         "platform-dev",
			Tracker:      "gitlab",
			AgentType:    "code-pr",
			PollInterval: config.Duration{Duration: 20 * time.Millisecond},
		}},
		AgentTypes: []config.AgentTypeConfig{{
			Name:            "code-pr",
			Harness:         "claude-code",
			Workspace:       "git-clone",
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

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := NewServiceWithDeps(cfg, logger, Dependencies{
		Tracker:   blockingPollTracker{},
		Harness:   &testutil.FakeHarness{},
		Workspace: workspace.NewManager(cfg.Workspace.Root),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	start := time.Now()
	err = svc.tick(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("tick error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("tick took %s, want bounded timeout", elapsed)
	}
}

func forcePollTestConfig(t *testing.T) *config.Config {
	t.Helper()

	root := t.TempDir()
	promptPath := filepath.Join(root, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("Issue {{.Issue.Identifier}}"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	return &config.Config{
		Defaults: config.DefaultsConfig{
			PollInterval:        config.Duration{Duration: 20 * time.Millisecond},
			MaxConcurrentGlobal: 1,
			StallTimeout:        config.Duration{Duration: time.Minute},
		},
		Sources: []config.SourceConfig{{
			Name:         "platform-dev",
			Tracker:      "gitlab",
			AgentType:    "code-pr",
			PollInterval: config.Duration{Duration: 20 * time.Millisecond},
		}},
		AgentTypes: []config.AgentTypeConfig{{
			Name:            "code-pr",
			Harness:         "claude-code",
			Workspace:       "git-clone",
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

func forcePollTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitForCondition(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestServiceForcePollTriggersImmediatePoll(t *testing.T) {
	oldDebounce := forcePollDebounce
	forcePollDebounce = 20 * time.Millisecond
	defer func() { forcePollDebounce = oldDebounce }()

	cfg := forcePollTestConfig(t)
	cfg.Sources[0].PollInterval = config.Duration{Duration: time.Hour}
	tracker := &forcePollTracker{}
	svc, err := NewServiceWithDeps(cfg, forcePollTestLogger(), Dependencies{
		Tracker:   tracker,
		Harness:   &testutil.FakeHarness{},
		Workspace: workspace.NewManager(cfg.Workspace.Root),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- svc.Run(ctx) }()

	waitForCondition(t, 2*time.Second, func() bool { return tracker.Count() == 1 })
	time.Sleep(forcePollDebounce + 10*time.Millisecond)

	result, err := svc.RequestForcePoll("")
	if err != nil {
		t.Fatalf("request force poll: %v", err)
	}
	if len(result.Results) != 1 || result.Results[0].Status != ForcePollCompleted {
		t.Fatalf("force poll result = %#v, want completed", result)
	}

	waitForCondition(t, 2*time.Second, func() bool { return tracker.Count() == 2 })
	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("service run: %v", err)
	}
}

func TestServiceForcePollDebouncesRecentPolls(t *testing.T) {
	oldDebounce := forcePollDebounce
	forcePollDebounce = 50 * time.Millisecond
	defer func() { forcePollDebounce = oldDebounce }()

	cfg := forcePollTestConfig(t)
	cfg.Sources[0].PollInterval = config.Duration{Duration: time.Hour}
	tracker := &forcePollTracker{}
	svc, err := NewServiceWithDeps(cfg, forcePollTestLogger(), Dependencies{
		Tracker:   tracker,
		Harness:   &testutil.FakeHarness{},
		Workspace: workspace.NewManager(cfg.Workspace.Root),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- svc.Run(ctx) }()

	waitForCondition(t, 2*time.Second, func() bool { return tracker.Count() == 1 })

	result, err := svc.RequestForcePoll("")
	if err != nil {
		t.Fatalf("request force poll: %v", err)
	}
	if len(result.Results) != 1 || result.Results[0].Status != ForcePollDebounced {
		t.Fatalf("force poll result = %#v, want debounced", result)
	}

	time.Sleep(forcePollDebounce + 20*time.Millisecond)
	if tracker.Count() != 1 {
		t.Fatalf("poll count = %d, want no extra poll", tracker.Count())
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("service run: %v", err)
	}
}

func TestServiceForcePollDoesNotOverlapInFlightPolls(t *testing.T) {
	oldDebounce := forcePollDebounce
	oldTimeout := forcePollCompletionTimeout
	oldWait := forcePollWaitInterval
	forcePollDebounce = 20 * time.Millisecond
	forcePollCompletionTimeout = 40 * time.Millisecond
	forcePollWaitInterval = 5 * time.Millisecond
	defer func() {
		forcePollDebounce = oldDebounce
		forcePollCompletionTimeout = oldTimeout
		forcePollWaitInterval = oldWait
	}()

	cfg := forcePollTestConfig(t)
	cfg.Sources[0].PollInterval = config.Duration{Duration: time.Hour}
	secondPollBlock := make(chan struct{})
	tracker := &forcePollTracker{
		blocks: map[int]chan struct{}{
			2: secondPollBlock,
		},
	}
	svc, err := NewServiceWithDeps(cfg, forcePollTestLogger(), Dependencies{
		Tracker:   tracker,
		Harness:   &testutil.FakeHarness{},
		Workspace: workspace.NewManager(cfg.Workspace.Root),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- svc.Run(ctx) }()

	waitForCondition(t, 2*time.Second, func() bool { return tracker.Count() == 1 })
	time.Sleep(forcePollDebounce + 10*time.Millisecond)

	first, err := svc.RequestForcePoll("")
	if err != nil {
		t.Fatalf("first request force poll: %v", err)
	}
	if len(first.Results) != 1 || first.Results[0].Status != ForcePollTimedOut {
		t.Fatalf("first force poll result = %#v, want timed out", first)
	}

	waitForCondition(t, 2*time.Second, func() bool { return tracker.Count() == 2 })

	second, err := svc.RequestForcePoll("")
	if err != nil {
		t.Fatalf("second request force poll: %v", err)
	}
	if len(second.Results) != 1 || second.Results[0].Status != ForcePollAlreadyQueued {
		t.Fatalf("second force poll result = %#v, want already queued", second)
	}
	if tracker.Count() != 2 {
		t.Fatalf("poll count = %d, want 2 while second poll in flight", tracker.Count())
	}

	close(secondPollBlock)
	waitForCondition(t, 2*time.Second, func() bool { return !svc.Snapshot().LastPollAt.IsZero() && tracker.Count() == 2 })

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("service run: %v", err)
	}
}

func TestServiceForcePollTimesOutWhenPollDoesNotStart(t *testing.T) {
	oldDebounce := forcePollDebounce
	oldTimeout := forcePollCompletionTimeout
	oldWait := forcePollWaitInterval
	forcePollDebounce = 0
	forcePollCompletionTimeout = 40 * time.Millisecond
	forcePollWaitInterval = 5 * time.Millisecond
	defer func() {
		forcePollDebounce = oldDebounce
		forcePollCompletionTimeout = oldTimeout
		forcePollWaitInterval = oldWait
	}()

	cfg := forcePollTestConfig(t)
	tracker := &forcePollTracker{}
	svc, err := NewServiceWithDeps(cfg, forcePollTestLogger(), Dependencies{
		Tracker:   tracker,
		Harness:   &testutil.FakeHarness{},
		Workspace: workspace.NewManager(cfg.Workspace.Root),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := svc.RequestForcePoll("")
	if err != nil {
		t.Fatalf("request force poll: %v", err)
	}
	if len(result.Results) != 1 || result.Results[0].Status != ForcePollTimedOut {
		t.Fatalf("force poll result = %#v, want timed out", result)
	}
	if tracker.Count() != 0 {
		t.Fatalf("poll count = %d, want 0", tracker.Count())
	}
}
