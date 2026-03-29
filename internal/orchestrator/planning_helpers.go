package orchestrator

import (
	"sort"
	"time"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/state"
	trackerbase "github.com/tjohnson/maestro/internal/tracker"
)

type dispatchLifecyclePlan struct {
	AddLabels    []string
	RemoveLabels []string
	State        string
}

type issueSuppression struct {
	Skip          bool
	FinishedStale bool
	RetryStale    bool
	RetryPending  bool
	RetryEntry    state.RetryEntry
	FinishedEntry state.TerminalIssue
	Attempt       int
	Reason        string
}

func sortIssuesStable(issues []domain.Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].CreatedAt.Equal(issues[j].CreatedAt) {
			return issues[i].Identifier < issues[j].Identifier
		}
		if issues[i].CreatedAt.IsZero() {
			return false
		}
		if issues[j].CreatedAt.IsZero() {
			return true
		}
		return issues[i].CreatedAt.Before(issues[j].CreatedAt)
	})
}

func planDispatchLifecycle(transition *config.DispatchTransition, prefix string) dispatchLifecyclePlan {
	if transition != nil && (transition.AddLabels != nil || transition.RemoveLabels != nil) {
		return dispatchLifecyclePlan{
			AddLabels:    append([]string(nil), transition.AddLabels...),
			RemoveLabels: append([]string(nil), transition.RemoveLabels...),
			State:        transition.State,
		}
	}

	return dispatchLifecyclePlan{
		AddLabels: []string{trackerbase.LifecycleLabel(prefix, trackerbase.LifecycleSuffixActive)},
		RemoveLabels: []string{
			trackerbase.LifecycleLabel(prefix, trackerbase.LifecycleSuffixRetry),
			trackerbase.LifecycleLabel(prefix, trackerbase.LifecycleSuffixDone),
			trackerbase.LifecycleLabel(prefix, trackerbase.LifecycleSuffixFailed),
		},
		State: transitionState(transition),
	}
}

func transitionState(transition *config.DispatchTransition) string {
	if transition == nil {
		return ""
	}
	return transition.State
}

func evaluateIssueSuppression(issue domain.Issue, finished state.TerminalIssue, hasFinished bool, retry state.RetryEntry, hasRetry bool, now time.Time) issueSuppression {
	if hasFinished {
		if issueRecordStale(issue.UpdatedAt, finished.IssueUpdatedAt) {
			return issueSuppression{
				FinishedStale: true,
				FinishedEntry: finished,
			}
		}
		return issueSuppression{
			Skip:          true,
			FinishedEntry: finished,
			Reason:        "already recorded as finished in persisted state",
		}
	}

	if !hasRetry {
		return issueSuppression{}
	}
	if issueRecordStale(issue.UpdatedAt, retry.IssueUpdatedAt) {
		return issueSuppression{
			RetryStale: true,
			RetryEntry: retry,
		}
	}
	if now.Before(retry.DueAt) {
		return issueSuppression{
			Skip:         true,
			RetryPending: true,
			RetryEntry:   retry,
			Attempt:      retry.Attempt,
			Reason:       "retry is scheduled for later",
		}
	}
	return issueSuppression{
		RetryEntry: retry,
		Attempt:    retry.Attempt,
	}
}
