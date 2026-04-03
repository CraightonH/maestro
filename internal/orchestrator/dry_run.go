package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/state"
	"github.com/tjohnson/maestro/internal/tracker"
	trackerbase "github.com/tjohnson/maestro/internal/tracker"
	"github.com/tjohnson/maestro/internal/workspace"
)

type DryRunReport struct {
	Sources []DryRunSourcePreview
}

type DryRunSourcePreview struct {
	Name              string
	Tracker           string
	PolledCount       int
	StateWarnings     []string
	MatchedIssue      bool
	IssueIdentifier   string
	IssueTitle        string
	IssueState        string
	CandidateSource   string
	Reason            string
	AgentType         string
	AgentName         string
	Attempt           int
	WorkspaceStrategy string
	WorkspacePath     string
	WorkspaceBranch   string
	Lifecycle         DryRunLifecyclePreview
	PromptPreview     string
	PromptWarning     string
	Evaluations       []DryRunEvaluation
}

type DryRunLifecyclePreview struct {
	AddLabels    []string
	RemoveLabels []string
	State        string
}

type DryRunEvaluation struct {
	Stage      string
	Identifier string
	Outcome    string
	Reason     string
}

func DryRun(ctx context.Context, cfg *config.Config, logger *slog.Logger) (DryRunReport, error) {
	agents := map[string]config.AgentTypeConfig{}
	for _, agent := range cfg.AgentTypes {
		agents[agent.Name] = agent
	}

	report := DryRunReport{
		Sources: make([]DryRunSourcePreview, 0, len(cfg.Sources)),
	}
	for _, source := range cfg.Sources {
		agent, ok := agents[source.AgentType]
		if !ok {
			return DryRunReport{}, fmt.Errorf("source %q references unknown agent type %q", source.Name, source.AgentType)
		}
		tr, err := newTracker(source)
		if err != nil {
			return DryRunReport{}, err
		}
		scoped := scopedConfig(cfg, source, agent)
		svc, warnings, err := newDryRunService(scoped, logger, tr)
		if err != nil {
			return DryRunReport{}, err
		}
		preview, err := svc.dryRun(ctx)
		if err != nil {
			return DryRunReport{}, err
		}
		preview.StateWarnings = append(preview.StateWarnings, warnings...)
		report.Sources = append(report.Sources, preview)
	}
	return report, nil
}

func newDryRunService(cfg *config.Config, logger *slog.Logger, tr tracker.Tracker) (*Service, []string, error) {
	if tr == nil {
		return nil, nil, fmt.Errorf("tracker dependency is required")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	svc := &Service{
		cfg:        cfg,
		logger:     logger,
		source:     cfg.Sources[0],
		agent:      cfg.AgentTypes[0],
		tracker:    tr,
		workspace:  workspace.NewManager(cfg.Workspace.Root).WithGitLabAuth(cfg.Sources[0].Connection.BaseURL(), cfg.Sources[0].Connection.Token),
		finished:   map[string]state.TerminalIssue{},
		retryQueue: map[string]state.RetryEntry{},
	}

	snapshot, warnings, err := loadDryRunSnapshot(state.NewStore(cfg.State.Dir))
	if err != nil {
		return nil, nil, err
	}
	svc.finished, svc.retryQueue = deriveDryRunState(snapshot, cfg.Sources[0], cfg.AgentTypes[0], cfg.State, time.Now())
	return svc, warnings, nil
}

func loadDryRunSnapshot(store *state.Store) (state.Snapshot, []string, error) {
	snapshot, err := store.LoadReadOnly()
	if err == nil {
		return snapshot, nil, nil
	}

	var corruptErr *state.CorruptStateError
	if errors.As(err, &corruptErr) {
		return snapshot, []string{fmt.Sprintf("ignoring unreadable state file %s during dry-run: %v", corruptErr.Path, corruptErr.Err)}, nil
	}
	return state.Snapshot{}, nil, err
}

func deriveDryRunState(snapshot state.Snapshot, source config.SourceConfig, agent config.AgentTypeConfig, stateCfg config.StateConfig, now time.Time) (map[string]state.TerminalIssue, map[string]state.RetryEntry) {
	finished := make(map[string]state.TerminalIssue, len(snapshot.Finished))
	for issueID, item := range snapshot.Finished {
		finished[issueID] = item
	}
	retryQueue := make(map[string]state.RetryEntry, len(snapshot.RetryQueue))
	for issueID, item := range snapshot.RetryQueue {
		retryQueue[issueID] = item
	}

	activeRuns := persistedActiveRuns(snapshot)
	if len(activeRuns) == 0 {
		return finished, retryQueue
	}

	for _, run := range activeRuns {
		nextAttempt := run.Attempt + 1
		if dryRunApprovalTimedOut(snapshot.PendingApprovals, run.RunID, agent.ApprovalTimeout.Duration, now) {
			finished[run.IssueID] = state.TerminalIssue{
				IssueID:        run.IssueID,
				Identifier:     run.Identifier,
				Status:         domain.RunStatusFailed,
				Attempt:        run.Attempt,
				IssueUpdatedAt: run.IssueUpdatedAt,
				FinishedAt:     now,
				Error:          "approval timeout",
			}
			continue
		}
		if nextAttempt >= source.EffectiveMaxAttempts(stateCfg) {
			finished[run.IssueID] = state.TerminalIssue{
				IssueID:        run.IssueID,
				Identifier:     run.Identifier,
				Status:         domain.RunStatusFailed,
				Attempt:        run.Attempt,
				IssueUpdatedAt: run.IssueUpdatedAt,
				FinishedAt:     now,
				Error:          "run interrupted during shutdown or restart",
			}
			continue
		}

		retryQueue[run.IssueID] = state.RetryEntry{
			IssueID:        run.IssueID,
			Identifier:     run.Identifier,
			Attempt:        nextAttempt,
			DueAt:          now,
			Error:          "recovered active run after restart",
			IssueUpdatedAt: run.IssueUpdatedAt,
			WorkspacePath:  run.WorkspacePath,
		}
	}
	return finished, retryQueue
}

func dryRunApprovalTimedOut(approvals []state.PersistedApprovalRequest, runID string, timeout time.Duration, now time.Time) bool {
	if timeout <= 0 {
		return false
	}
	for _, approval := range approvals {
		if approval.RunID != runID {
			continue
		}
		if approval.RequestedAt.IsZero() {
			continue
		}
		if now.Before(approval.RequestedAt.Add(timeout)) {
			continue
		}
		return true
	}
	return false
}

func (s *Service) dryRun(ctx context.Context) (DryRunSourcePreview, error) {
	preview := DryRunSourcePreview{
		Name:    s.source.Name,
		Tracker: s.source.Tracker,
	}

	pollCtx, cancel := context.WithTimeout(ctx, pollRequestTimeout)
	defer cancel()

	issues, err := s.tracker.Poll(pollCtx)
	if err != nil {
		return preview, err
	}
	sortIssuesStable(issues)
	preview.PolledCount = len(issues)

	finished := copyTerminalIssues(s.finished)
	retryQueue := copyRetryEntries(s.retryQueue)
	now := time.Now()

	chosen, chosenAttempt, retryEvals, ok, err := s.selectRetryCandidate(ctx, retryQueue)
	if err != nil {
		return preview, err
	}
	preview.Evaluations = append(preview.Evaluations, retryEvals...)
	if ok {
		return s.populateDryRunPreview(preview, chosen, chosenAttempt, "retry", "next due retry remains eligible for dispatch")
	}

	for _, issue := range issues {
		finishedEntry, hasFinished := finished[issue.ID]
		retryEntry, hasRetry := retryQueue[issue.ID]
		evaluation := evaluateIssueSuppression(issue, finishedEntry, hasFinished, retryEntry, hasRetry, now)
		if evaluation.FinishedStale {
			delete(finished, issue.ID)
		}
		if evaluation.RetryStale {
			delete(retryQueue, issue.ID)
		}
		if evaluation.Skip {
			preview.Evaluations = append(preview.Evaluations, DryRunEvaluation{
				Stage:      "poll",
				Identifier: issue.Identifier,
				Outcome:    "skipped",
				Reason:     dryRunSuppressionReason(evaluation),
			})
			continue
		}

		refreshed, status, reason, err := s.guardRetryCandidate(ctx, issue)
		switch status {
		case dispatchGuardReady:
		case dispatchGuardBlocked:
			preview.Evaluations = append(preview.Evaluations, DryRunEvaluation{
				Stage:      "poll",
				Identifier: refreshed.Identifier,
				Outcome:    "skipped",
				Reason:     fmt.Sprintf("issue is blocked by %s", reason),
			})
			continue
		case dispatchGuardSkipped:
			preview.Evaluations = append(preview.Evaluations, DryRunEvaluation{
				Stage:      "poll",
				Identifier: refreshed.Identifier,
				Outcome:    "skipped",
				Reason:     "dispatch guard refresh showed the issue is no longer eligible",
			})
			continue
		case dispatchGuardError:
			preview.Evaluations = append(preview.Evaluations, DryRunEvaluation{
				Stage:      "poll",
				Identifier: issue.Identifier,
				Outcome:    "blocked",
				Reason:     fmt.Sprintf("dispatch guard refresh failed: %v", err),
			})
			continue
		}

		attempt := 0
		if retry, ok := retryQueue[issue.ID]; ok {
			attempt = retry.Attempt
		}
		return s.populateDryRunPreview(preview, refreshed, attempt, "poll", "first unsuppressed polled issue remains eligible after dispatch guard refresh")
	}

	if len(issues) == 0 {
		preview.Reason = "tracker returned no candidate issues for this source"
		return preview, nil
	}
	if len(preview.Evaluations) == 0 {
		preview.Reason = "no issue was eligible for dispatch"
		return preview, nil
	}
	if dryRunEvaluationsIncludeGuardChecks(preview.Evaluations) {
		preview.Reason = "all polled issues were suppressed locally or skipped by dispatch guard checks"
		return preview, nil
	}
	preview.Reason = "all polled issues were suppressed by local recovery state"
	return preview, nil
}

func (s *Service) selectRetryCandidate(ctx context.Context, retryQueue map[string]state.RetryEntry) (domain.Issue, int, []DryRunEvaluation, bool, error) {
	type retryCandidate struct {
		issueID string
		dueAt   time.Time
	}

	now := time.Now()
	candidates := make([]retryCandidate, 0, len(retryQueue))
	for issueID, retry := range retryQueue {
		if now.Before(retry.DueAt) {
			continue
		}
		candidates = append(candidates, retryCandidate{issueID: issueID, dueAt: retry.DueAt})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].dueAt.Equal(candidates[j].dueAt) {
			return candidates[i].issueID < candidates[j].issueID
		}
		return candidates[i].dueAt.Before(candidates[j].dueAt)
	})

	evals := make([]DryRunEvaluation, 0, len(candidates))
	for _, candidate := range candidates {
		retry := retryQueue[candidate.issueID]
		issue, err := s.tracker.Get(ctx, candidate.issueID)
		if err != nil {
			evals = append(evals, DryRunEvaluation{
				Stage:      "retry",
				Identifier: retry.Identifier,
				Outcome:    "blocked",
				Reason:     fmt.Sprintf("retry lookup failed: %v", err),
			})
			continue
		}
		if trackerbase.IsTerminal(issue) || !trackerbase.MatchesFilterWithPrefix(issue, s.source.EffectiveIssueFilter(), s.labelPrefix()) {
			delete(retryQueue, candidate.issueID)
			evals = append(evals, DryRunEvaluation{
				Stage:      "retry",
				Identifier: issue.Identifier,
				Outcome:    "skipped",
				Reason:     "stored retry is stale because the issue is no longer eligible",
			})
			continue
		}
		refreshed, status, reason, err := s.guardDispatchCandidate(ctx, issue)
		switch status {
		case dispatchGuardReady:
			evals = append(evals, DryRunEvaluation{
				Stage:      "retry",
				Identifier: refreshed.Identifier,
				Outcome:    "selected",
				Reason:     fmt.Sprintf("retry is due now at attempt %d", retry.Attempt),
			})
			return refreshed, retry.Attempt, evals, true, nil
		case dispatchGuardBlocked:
			evals = append(evals, DryRunEvaluation{
				Stage:      "retry",
				Identifier: refreshed.Identifier,
				Outcome:    "skipped",
				Reason:     fmt.Sprintf("issue is blocked by %s", reason),
			})
		case dispatchGuardSkipped:
			delete(retryQueue, candidate.issueID)
			evals = append(evals, DryRunEvaluation{
				Stage:      "retry",
				Identifier: refreshed.Identifier,
				Outcome:    "skipped",
				Reason:     "stored retry is stale because the issue is no longer eligible",
			})
		case dispatchGuardError:
			evals = append(evals, DryRunEvaluation{
				Stage:      "retry",
				Identifier: issue.Identifier,
				Outcome:    "blocked",
				Reason:     fmt.Sprintf("dispatch guard refresh failed: %v", err),
			})
		}
	}

	return domain.Issue{}, 0, evals, false, nil
}

func (s *Service) populateDryRunPreview(preview DryRunSourcePreview, issue domain.Issue, attempt int, candidateSource string, reason string) (DryRunSourcePreview, error) {
	agentName := s.agent.InstanceName
	if agentName == "" {
		agentName = s.agent.Name
	}

	prepared, err := previewWorkspace(s.workspace, s.agent, issue, agentName)
	if err != nil {
		return DryRunSourcePreview{}, err
	}

	runtimeAgent := s.agent
	resolvedAgent, resolveErr := resolveRuntimeAgent(s.agent, prepared.Path)
	if resolveErr == nil {
		runtimeAgent = resolvedAgent
	} else {
		preview.PromptWarning = fmt.Sprintf("runtime repo pack preview unavailable: %v", resolveErr)
	}

	if strings.TrimSpace(runtimeAgent.Prompt) != "" {
		renderedPrompt, err := s.renderPrompt(runtimeAgent, issue, agentName, attempt, "")
		if err != nil {
			preview.PromptWarning = fmt.Sprintf("prompt preview unavailable: %v", err)
		} else {
			preview.PromptPreview = renderedPrompt
		}
	} else if preview.PromptWarning == "" {
		preview.PromptWarning = "prompt preview unavailable because the runtime prompt path is not yet resolved"
	}

	transition := config.ResolveDispatchTransition(s.cfg.Defaults.OnDispatch, s.source.OnDispatch)
	lifecycle := planDispatchLifecycle(transition, s.labelPrefix())

	preview.MatchedIssue = true
	preview.IssueIdentifier = issue.Identifier
	preview.IssueTitle = issue.Title
	preview.IssueState = issue.State
	preview.CandidateSource = candidateSource
	preview.Reason = reason
	preview.AgentType = s.agent.Name
	preview.AgentName = agentName
	preview.Attempt = attempt
	preview.WorkspaceStrategy = s.agent.Workspace
	preview.WorkspacePath = prepared.Path
	preview.WorkspaceBranch = prepared.Branch
	preview.Lifecycle = DryRunLifecyclePreview{
		AddLabels:    lifecycle.AddLabels,
		RemoveLabels: lifecycle.RemoveLabels,
		State:        lifecycle.State,
	}
	return preview, nil
}

func dryRunEvaluationsIncludeGuardChecks(evals []DryRunEvaluation) bool {
	for _, eval := range evals {
		if strings.Contains(eval.Reason, "dispatch guard") || strings.Contains(eval.Reason, "blocked by") {
			return true
		}
	}
	return false
}

func previewWorkspace(manager *workspace.Manager, agent config.AgentTypeConfig, issue domain.Issue, agentName string) (workspace.Prepared, error) {
	switch agent.Workspace {
	case "git-clone":
		return manager.PreviewClone(issue, agentName)
	case "none":
		return manager.PreviewEmpty(issue)
	default:
		return workspace.Prepared{}, fmt.Errorf("unsupported workspace strategy %q", agent.Workspace)
	}
}

func copyTerminalIssues(src map[string]state.TerminalIssue) map[string]state.TerminalIssue {
	dst := make(map[string]state.TerminalIssue, len(src))
	for issueID, item := range src {
		dst[issueID] = item
	}
	return dst
}

func copyRetryEntries(src map[string]state.RetryEntry) map[string]state.RetryEntry {
	dst := make(map[string]state.RetryEntry, len(src))
	for issueID, item := range src {
		dst[issueID] = item
	}
	return dst
}

func dryRunSuppressionReason(evaluation issueSuppression) string {
	if evaluation.RetryPending {
		return fmt.Sprintf("retry is scheduled for %s (attempt %d)", evaluation.RetryEntry.DueAt.Format(time.RFC3339), evaluation.Attempt)
	}
	return evaluation.Reason
}
