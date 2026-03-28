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
	if s.activeRun != nil && s.activeRun.ID == runID {
		if stop, ok := s.pendingStops[runID]; ok {
			if stop.Retry {
				scheduledRetry = s.stateMgr.scheduleRetry(s.activeRun, fmt.Errorf("%s", stop.Reason))
				if scheduledRetry {
					retry = s.retryQueue[s.activeRun.Issue.ID]
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
		s.activeRun.Status = status
		s.activeRun.CompletedAt = time.Now()
		issueID = s.activeRun.Issue.ID
		issueIdentifier = s.activeRun.Issue.Identifier
		if scheduledRetry {
			delete(s.finished, issueID)
		} else {
			s.finished[issueID] = state.TerminalIssue{
				IssueID:        s.activeRun.Issue.ID,
				Identifier:     s.activeRun.Issue.Identifier,
				Status:         status,
				Attempt:        s.activeRun.Attempt,
				IssueUpdatedAt: s.activeRun.Issue.UpdatedAt,
				FinishedAt:     s.activeRun.CompletedAt,
				Error:          comment,
			}
		}
		if !scheduledRetry {
			delete(s.retryQueue, issueID)
		}
		s.activeRun = nil
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
	if s.activeRun != nil && s.activeRun.ID == runID {
		s.activeRun.Status = domain.RunStatusFailed
		s.activeRun.CompletedAt = time.Now()
		s.activeRun.Error = err.Error()
		issueID = s.activeRun.Issue.ID
		issueIdentifier = s.activeRun.Issue.Identifier
		stop, plannedStop = s.pendingStops[runID]
		delete(s.pendingStops, runID)
		if plannedStop {
			if stop.Retry {
				scheduledRetry = s.stateMgr.scheduleRetry(s.activeRun, fmt.Errorf("%s: %w", stop.Reason, err))
				if scheduledRetry {
					retry = s.retryQueue[issueID]
				}
			} else {
				s.finished[issueID] = state.TerminalIssue{
					IssueID:        s.activeRun.Issue.ID,
					Identifier:     s.activeRun.Issue.Identifier,
					Status:         stop.Status,
					Attempt:        s.activeRun.Attempt,
					IssueUpdatedAt: s.activeRun.Issue.UpdatedAt,
					FinishedAt:     s.activeRun.CompletedAt,
					Error:          stop.Reason,
				}
			}
		} else {
			scheduledRetry = s.stateMgr.scheduleRetry(s.activeRun, err)
			if scheduledRetry {
				retry = s.retryQueue[issueID]
			} else {
				s.finished[issueID] = state.TerminalIssue{
					IssueID:        s.activeRun.Issue.ID,
					Identifier:     s.activeRun.Issue.Identifier,
					Status:         domain.RunStatusFailed,
					Attempt:        s.activeRun.Attempt,
					IssueUpdatedAt: s.activeRun.Issue.UpdatedAt,
					FinishedAt:     s.activeRun.CompletedAt,
					Error:          err.Error(),
				}
			}
		}
		s.activeRun = nil
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
	if s.activeRun == nil || s.activeRun.ID != runID {
		s.mu.Unlock()
		return
	}
	mutate(s.activeRun)
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
