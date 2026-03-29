package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"github.com/tjohnson/maestro/internal/domain"
	trackerbase "github.com/tjohnson/maestro/internal/tracker"
)

type dispatchGuardStatus int

const (
	dispatchGuardReady dispatchGuardStatus = iota
	dispatchGuardSkipped
	dispatchGuardBlocked
	dispatchGuardError
)

func (s *Service) guardDispatchCandidate(ctx context.Context, issue domain.Issue) (domain.Issue, dispatchGuardStatus, string, error) {
	return s.guardDispatchCandidateWithOptions(ctx, issue, false)
}

func (s *Service) guardRetryCandidate(ctx context.Context, issue domain.Issue) (domain.Issue, dispatchGuardStatus, string, error) {
	return s.guardDispatchCandidateWithOptions(ctx, issue, true)
}

func (s *Service) guardDispatchCandidateWithOptions(ctx context.Context, issue domain.Issue, allowRetryLabel bool) (domain.Issue, dispatchGuardStatus, string, error) {
	refreshed, err := s.tracker.Get(ctx, issue.ID)
	if err != nil {
		return domain.Issue{}, dispatchGuardError, "", err
	}
	if trackerbase.IsTerminal(refreshed) || !trackerbase.MatchesFilterWithPrefix(refreshed, s.source.EffectiveIssueFilter(), s.labelPrefix()) {
		return refreshed, dispatchGuardSkipped, "issue is no longer eligible after refresh", nil
	}
	if lifecycleState := trackerbase.LifecycleLabelStateWithPrefix(refreshed.Labels, s.labelPrefix()); lifecycleState != "" {
		retryLabel := trackerbase.LifecycleLabel(s.labelPrefix(), trackerbase.LifecycleSuffixRetry)
		if !(allowRetryLabel && lifecycleState == retryLabel) {
			return refreshed, dispatchGuardSkipped, "issue is no longer eligible after refresh", nil
		}
	}
	if !s.source.EffectiveRespectBlockers() {
		return refreshed, dispatchGuardReady, "", nil
	}

	blockers := trackerbase.NonTerminalBlockers(refreshed)
	if len(blockers) > 0 {
		return refreshed, dispatchGuardBlocked, formatBlockerSummary(blockers), nil
	}
	return refreshed, dispatchGuardReady, "", nil
}

func formatBlockerSummary(blockers []domain.Issue) string {
	if len(blockers) == 0 {
		return ""
	}

	parts := make([]string, 0, len(blockers))
	for _, blocker := range blockers {
		identifier := strings.TrimSpace(blocker.Identifier)
		if identifier == "" {
			identifier = strings.TrimSpace(blocker.ID)
		}
		if identifier == "" {
			identifier = "unknown blocker"
		}
		state := strings.TrimSpace(blocker.State)
		if state != "" {
			parts = append(parts, fmt.Sprintf("%s (%s)", identifier, state))
			continue
		}
		parts = append(parts, identifier)
	}
	return strings.Join(parts, ", ")
}
