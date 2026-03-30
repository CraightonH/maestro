package orchestrator

import (
	"context"
	"sort"
	"time"

	trackerbase "github.com/tjohnson/maestro/internal/tracker"
)

var pollRequestTimeout = 30 * time.Second

func (s *Service) Run(ctx context.Context) error {
	defer func() {
		if s.cleanup != nil {
			_ = s.cleanup()
		}
	}()
	s.approvalMgr.startWatcher(ctx)
	s.messageMgr.startWatcher(ctx)
	if err := s.runTick(ctx, false); err != nil {
		s.recordEvent("error", "initial poll failed: %v", err)
	}

	ticker := time.NewTicker(s.source.PollInterval.Duration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if err := s.shutdown(); err != nil {
				s.recordEvent("error", "shutdown failed: %v", err)
				return err
			}
			s.runWG.Wait()
			_ = s.stateMgr.saveStateBestEffort()
			s.recordEvent("info", "service shutting down")
			return nil
		case <-s.controlCh:
			if s.shouldExitAfterDrain() {
				_ = s.stateMgr.saveStateBestEffort()
				s.recordSourceEvent("info", s.source.Name, "service drained")
				return nil
			}
		case <-ticker.C:
			if err := s.runTick(ctx, false); err != nil {
				s.recordEvent("error", "poll failed: %v", err)
			}
		case <-s.forcePollCh:
			if err := s.runTick(ctx, true); err != nil {
				s.recordSourceEvent("error", s.source.Name, "force poll failed: %v", err)
			}
		}
	}
}

func (s *Service) runTick(ctx context.Context, clearForcePoll bool) error {
	s.mu.Lock()
	if clearForcePoll {
		s.forcePollPending = false
	}
	s.polling = true
	s.lastPollAttemptAt = time.Now()
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.polling = false
		s.mu.Unlock()
	}()

	return s.tick(ctx)
}

func (s *Service) tick(ctx context.Context) error {
	s.reconcileStalledRun(ctx)

	pollCtx, cancel := context.WithTimeout(ctx, pollRequestTimeout)
	defer cancel()

	issues, err := s.tracker.Poll(pollCtx)
	if err != nil {
		return err
	}

	sortIssuesStable(issues)

	s.mu.Lock()
	s.lastPollAt = time.Now()
	s.lastPollCount = len(issues)
	hasActive := s.activeRun != nil
	s.mu.Unlock()

	s.recordSourceEvent("info", s.source.Name, "polled %d candidate issues from %s", len(issues), s.source.Name)

	if hasActive {
		s.reconcileActiveRun(ctx, issues)
		return nil
	}

	if s.IsDraining() {
		return nil
	}

	if err := s.dispatchDueRetry(ctx); err != nil {
		return err
	}

	for _, issue := range issues {
		if s.isClaimed(issue.ID) || s.stateMgr.shouldSkipIssue(issue) {
			continue
		}
		guarded, status, reason, err := s.guardDispatchCandidate(ctx, issue)
		switch status {
		case dispatchGuardReady:
			return s.runMgr.dispatch(ctx, guarded)
		case dispatchGuardBlocked:
			s.recordSourceEvent("info", s.source.Name, "skipping %s because it is blocked by %s", guarded.Identifier, reason)
		case dispatchGuardSkipped:
			s.recordSourceEvent("info", s.source.Name, "dispatch guard: %s no longer eligible", guarded.Identifier)
		case dispatchGuardError:
			s.recordSourceEvent("warn", s.source.Name, "dispatch guard refresh failed for %s: %v", issue.Identifier, err)
		}
	}

	return nil
}

func (s *Service) dispatchDueRetry(ctx context.Context) error {
	type retryCandidate struct {
		issueID string
		dueAt   time.Time
		attempt int
	}

	s.mu.RLock()
	candidates := make([]retryCandidate, 0, len(s.retryQueue))
	for issueID, retry := range s.retryQueue {
		if time.Now().Before(retry.DueAt) {
			continue
		}
		candidates = append(candidates, retryCandidate{
			issueID: issueID,
			dueAt:   retry.DueAt,
			attempt: retry.Attempt,
		})
	}
	s.mu.RUnlock()

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].dueAt.Equal(candidates[j].dueAt) {
			return candidates[i].issueID < candidates[j].issueID
		}
		return candidates[i].dueAt.Before(candidates[j].dueAt)
	})

	for _, candidate := range candidates {
		if s.isClaimed(candidate.issueID) {
			continue
		}
		issue, err := s.tracker.Get(ctx, candidate.issueID)
		if err != nil {
			s.recordSourceEvent("warn", s.source.Name, "retry lookup failed for %s: %v", candidate.issueID, err)
			continue
		}
		if trackerbase.IsTerminal(issue) || !trackerbase.MatchesFilterWithPrefix(issue, s.source.EffectiveIssueFilter(), s.labelPrefix()) {
			s.mu.Lock()
			delete(s.retryQueue, candidate.issueID)
			s.mu.Unlock()
			_ = s.stateMgr.saveStateBestEffort()
			s.recordSourceEvent("info", s.source.Name, "discarded retry for %s (no longer eligible)", issue.Identifier)
			continue
		}
		guarded, status, reason, err := s.guardRetryCandidate(ctx, issue)
		switch status {
		case dispatchGuardReady:
			return s.runMgr.dispatchRetry(ctx, guarded, candidate.attempt)
		case dispatchGuardBlocked:
			s.recordSourceEvent("info", s.source.Name, "skipping retry for %s because it is blocked by %s", guarded.Identifier, reason)
		case dispatchGuardSkipped:
			s.mu.Lock()
			delete(s.retryQueue, candidate.issueID)
			s.mu.Unlock()
			_ = s.stateMgr.saveStateBestEffort()
			s.recordSourceEvent("info", s.source.Name, "discarded retry for %s (no longer eligible)", guarded.Identifier)
		case dispatchGuardError:
			s.recordSourceEvent("warn", s.source.Name, "dispatch guard refresh failed for %s: %v", issue.Identifier, err)
		}
	}

	return nil
}

func (s *Service) shutdown() error {
	s.mu.RLock()
	activeRun := s.activeRun
	s.mu.RUnlock()

	if activeRun == nil {
		return nil
	}

	s.recordRunEvent(activeRun, "info", "stopping active run %s", activeRun.ID)
	stopCtx, cancel := withHarnessShutdownTimeout()
	defer cancel()
	return s.harness.Stop(stopCtx, activeRun.ID)
}
