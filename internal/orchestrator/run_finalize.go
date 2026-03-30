package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/state"
	trackerbase "github.com/tjohnson/maestro/internal/tracker"
)

func (r *runManager) completeRun(runID string) {
	s := r.service
	var issueID string
	var issueIdentifier string
	status := domain.RunStatusDone
	comment := "Maestro completed the run successfully."
	var retry state.RetryEntry
	scheduledRetry := false
	s.mu.Lock()
	if run := s.activeRunByIDLocked(runID); run != nil {
		if stop, ok := s.pendingStops[runID]; ok {
			if stop.Retry {
				scheduledRetry = s.stateMgr.scheduleRetry(run, fmt.Errorf("%s", stop.Reason))
				if scheduledRetry {
					retry = s.retryQueue[run.Issue.ID]
				} else {
					status = stop.Status
					comment = stop.Reason
				}
			} else {
				status = stop.Status
				comment = stop.Reason
			}
			delete(s.pendingStops, runID)
		}
		run.Status = status
		run.CompletedAt = time.Now()
		run.Metrics = domain.DeriveRunMetrics(run.Metrics, run.StartedAt, run.CompletedAt, run.CompletedAt)
		s.metricsStore.addCompleted(s.source.Name, run.HarnessKind, run.Metrics)
		issueID = run.Issue.ID
		issueIdentifier = run.Issue.Identifier
		if scheduledRetry {
			delete(s.finished, issueID)
		} else {
			s.finished[issueID] = state.TerminalIssue{
				IssueID:        run.Issue.ID,
				Identifier:     run.Issue.Identifier,
				Status:         status,
				Attempt:        run.Attempt,
				IssueUpdatedAt: run.Issue.UpdatedAt,
				FinishedAt:     run.CompletedAt,
				Metrics:        run.Metrics,
				Error:          comment,
			}
		}
		if !scheduledRetry {
			delete(s.retryQueue, issueID)
		}
		s.removeActiveRunLocked(runID)
	}
	s.mu.Unlock()

	prefix := s.labelPrefix()
	onComplete := config.ResolveLifecycleTransition(s.cfg.Defaults.OnComplete, s.source.OnComplete)
	onFailure := config.ResolveLifecycleTransition(s.cfg.Defaults.OnFailure, s.source.OnFailure)
	activeLabel := trackerbase.LifecycleLabel(prefix, trackerbase.LifecycleSuffixActive)
	retryLabel := trackerbase.LifecycleLabel(prefix, trackerbase.LifecycleSuffixRetry)
	doneLabel := trackerbase.LifecycleLabel(prefix, trackerbase.LifecycleSuffixDone)
	failedLabel := trackerbase.LifecycleLabel(prefix, trackerbase.LifecycleSuffixFailed)

	if scheduledRetry {
		s.recordRunEventByFields("warn", s.source.Name, runID, issueIdentifier, "run %s stopped: %s; retry %d scheduled for %s", runID, comment, retry.Attempt, retry.DueAt.Format(time.RFC3339))
		if issueID != "" {
			s.applyTrackerLifecycle(context.Background(), issueID, []string{retryLabel}, []string{
				activeLabel, doneLabel, failedLabel,
			}, fmt.Sprintf("Maestro run %s stopped and retry %d is scheduled: %s", runID, retry.Attempt, comment))
			s.refreshStoredIssueTimestamp(context.Background(), issueID)
		}
		r.finalizeRun(issueID)
		return
	}

	s.recordRunEventByFields("info", s.source.Name, runID, issueIdentifier, "run %s completed", runID)
	if issueID != "" {
		if status == domain.RunStatusFailed {
			s.applyTerminalLifecycle(context.Background(), issueID, onFailure, prefix, failedLabel, comment)
		} else {
			s.applyTerminalLifecycle(context.Background(), issueID, onComplete, prefix, doneLabel, comment)
		}
		s.refreshStoredIssueTimestamp(context.Background(), issueID)
		s.recordRunEventByFields("info", s.source.Name, runID, issueIdentifier, "tracker state updated for %s", issueIdentifier)
	}
	r.finalizeRun(issueID)
}

func (r *runManager) failRun(runID string, err error) {
	s := r.service
	var issueID string
	var issueIdentifier string
	var retry state.RetryEntry
	scheduledRetry := false
	var stop pendingStop
	plannedStop := false
	s.mu.Lock()
	if run := s.activeRunByIDLocked(runID); run != nil {
		run.Status = domain.RunStatusFailed
		run.CompletedAt = time.Now()
		run.Metrics = domain.DeriveRunMetrics(run.Metrics, run.StartedAt, run.CompletedAt, run.CompletedAt)
		s.metricsStore.addCompleted(s.source.Name, run.HarnessKind, run.Metrics)
		run.Error = err.Error()
		issueID = run.Issue.ID
		issueIdentifier = run.Issue.Identifier
		stop, plannedStop = s.pendingStops[runID]
		delete(s.pendingStops, runID)
		if plannedStop {
			if stop.Retry {
				scheduledRetry = s.stateMgr.scheduleRetry(run, fmt.Errorf("%s: %w", stop.Reason, err))
				if scheduledRetry {
					retry = s.retryQueue[issueID]
				}
			} else {
				s.finished[issueID] = state.TerminalIssue{
					IssueID:        run.Issue.ID,
					Identifier:     run.Issue.Identifier,
					Status:         stop.Status,
					Attempt:        run.Attempt,
					IssueUpdatedAt: run.Issue.UpdatedAt,
					FinishedAt:     run.CompletedAt,
					Metrics:        run.Metrics,
					Error:          stop.Reason,
				}
			}
		} else {
			scheduledRetry = s.stateMgr.scheduleRetry(run, err)
			if scheduledRetry {
				retry = s.retryQueue[issueID]
			} else {
				s.finished[issueID] = state.TerminalIssue{
					IssueID:        run.Issue.ID,
					Identifier:     run.Issue.Identifier,
					Status:         domain.RunStatusFailed,
					Attempt:        run.Attempt,
					IssueUpdatedAt: run.Issue.UpdatedAt,
					FinishedAt:     run.CompletedAt,
					Metrics:        run.Metrics,
					Error:          err.Error(),
				}
			}
		}
		s.removeActiveRunLocked(runID)
	}
	s.mu.Unlock()

	prefix := s.labelPrefix()
	onComplete := config.ResolveLifecycleTransition(s.cfg.Defaults.OnComplete, s.source.OnComplete)
	onFailure := config.ResolveLifecycleTransition(s.cfg.Defaults.OnFailure, s.source.OnFailure)
	activeLabel := trackerbase.LifecycleLabel(prefix, trackerbase.LifecycleSuffixActive)
	retryLabel := trackerbase.LifecycleLabel(prefix, trackerbase.LifecycleSuffixRetry)
	doneLabel := trackerbase.LifecycleLabel(prefix, trackerbase.LifecycleSuffixDone)
	failedLabel := trackerbase.LifecycleLabel(prefix, trackerbase.LifecycleSuffixFailed)

	if plannedStop {
		if stop.Retry {
			s.recordRunEventByFields("warn", s.source.Name, runID, issueIdentifier, "run %s stopped: %s; retry %d scheduled for %s", runID, stop.Reason, retry.Attempt, retry.DueAt.Format(time.RFC3339))
			s.applyTrackerLifecycle(context.Background(), issueID, []string{retryLabel}, []string{
				activeLabel, doneLabel, failedLabel,
			}, fmt.Sprintf("Maestro run %s stopped and retry %d is scheduled: %s", runID, retry.Attempt, stop.Reason))
			s.refreshStoredIssueTimestamp(context.Background(), issueID)
			r.finalizeRun(issueID)
			return
		}
		s.recordRunEventByFields("warn", s.source.Name, runID, issueIdentifier, "run %s stopped: %s", runID, stop.Reason)
		if stop.Status == domain.RunStatusFailed {
			s.applyTerminalLifecycle(context.Background(), issueID, onFailure, prefix, failedLabel, stop.Reason)
		} else {
			s.applyTerminalLifecycle(context.Background(), issueID, onComplete, prefix, doneLabel, stop.Reason)
		}
		s.refreshStoredIssueTimestamp(context.Background(), issueID)
		r.finalizeRun(issueID)
		return
	}
	if scheduledRetry {
		s.recordRunEventByFields("warn", s.source.Name, runID, issueIdentifier, "run %s failed: %v; retry %d scheduled for %s", runID, err, retry.Attempt, retry.DueAt.Format(time.RFC3339))
		s.applyTrackerLifecycle(context.Background(), issueID, []string{retryLabel}, []string{
			activeLabel, doneLabel, failedLabel,
		}, fmt.Sprintf("Maestro run %s failed and retry %d is scheduled: %v", runID, retry.Attempt, err))
		s.refreshStoredIssueTimestamp(context.Background(), issueID)
		r.finalizeRun(issueID)
		return
	}
	s.recordRunEventByFields("error", s.source.Name, runID, issueIdentifier, "run %s failed: %v", runID, err)
	if issueID != "" {
		s.applyTerminalLifecycle(context.Background(), issueID, onFailure, prefix, failedLabel, fmt.Sprintf("Maestro run %s failed for %s: %v", runID, issueIdentifier, err))
		s.refreshStoredIssueTimestamp(context.Background(), issueID)
	}
	r.finalizeRun(issueID)
}

func (s *Service) isClaimed(issueID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.claimed[issueID]
	return ok
}

func (r *runManager) updateRun(runID string, mutate func(*domain.AgentRun)) {
	s := r.service
	s.mu.Lock()
	run := s.activeRunByIDLocked(runID)
	if run == nil {
		s.mu.Unlock()
		return
	}
	mutate(run)
	s.mu.Unlock()
	_ = s.stateMgr.saveStateBestEffort()
}

func (r *runManager) finalizeRun(issueID string) {
	s := r.service
	r.releaseClaim(issueID)
	if s.limiter != nil {
		s.limiter.Release()
	}
	_ = s.stateMgr.saveStateBestEffort()
	s.signalControl()
}

func (r *runManager) releaseClaim(issueID string) {
	s := r.service
	if issueID == "" {
		return
	}

	s.mu.Lock()
	delete(s.claimed, issueID)
	s.mu.Unlock()
}
