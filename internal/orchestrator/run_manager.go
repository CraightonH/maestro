package orchestrator

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/harness"
	trackerbase "github.com/tjohnson/maestro/internal/tracker"
	"github.com/tjohnson/maestro/internal/workspace"
)

type runManager struct {
	service *Service
}

func (r *runManager) dispatch(ctx context.Context, issue domain.Issue) error {
	s := r.service
	if s.limiter != nil && !s.limiter.TryAcquire() {
		return nil
	}

	refreshed, err := s.tracker.Get(ctx, issue.ID)
	if err != nil {
		s.recordSourceEvent("warn", s.source.Name, "dispatch guard refresh failed for %s: %v", issue.Identifier, err)
		if s.limiter != nil {
			s.limiter.Release()
		}
		return nil
	}
	if trackerbase.IsTerminal(refreshed) || !trackerbase.MatchesFilterWithPrefix(refreshed, s.source.EffectiveIssueFilter(), s.labelPrefix()) {
		s.recordSourceEvent("info", s.source.Name, "dispatch guard: %s no longer eligible", issue.Identifier)
		if s.limiter != nil {
			s.limiter.Release()
		}
		return nil
	}
	issue = refreshed

	attempt := s.stateMgr.takeAttempt(issue)
	startedAt := time.Now()
	run := &domain.AgentRun{
		ID:             newRunID(startedAt),
		AgentName:      s.agent.InstanceName,
		AgentType:      s.agent.Name,
		Issue:          issue,
		SourceName:     s.source.Name,
		HarnessKind:    s.harness.Kind(),
		Status:         domain.RunStatusPending,
		Attempt:        attempt,
		ApprovalPolicy: s.agent.ApprovalPolicy,
		ApprovalState:  s.stateMgr.approvalState(),
		StartedAt:      startedAt,
	}
	if run.AgentName == "" {
		run.AgentName = s.agent.Name
	}

	s.mu.Lock()
	s.claimed[issue.ID] = struct{}{}
	s.activeRun = run
	s.mu.Unlock()
	_ = s.stateMgr.saveStateBestEffort()

	prefix := s.labelPrefix()
	dispatchTransition := config.ResolveDispatchTransition(s.cfg.Defaults.OnDispatch, s.source.OnDispatch)
	s.recordRunEvent(run, "info", "dispatching %s to %s", issue.Identifier, run.AgentName)
	s.applyDispatchLifecycle(ctx, issue.ID, dispatchTransition, prefix, run)
	s.refreshActiveRunIssue(ctx, run.ID)

	s.runWG.Add(1)
	go r.executeRun(ctx, run)
	return nil
}

func newRunID(now time.Time) string {
	return fmt.Sprintf("run-%s-%06d", now.Format("20060102-150405"), now.Nanosecond()/1000)
}

func (r *runManager) executeRun(ctx context.Context, run *domain.AgentRun) {
	s := r.service
	defer s.runWG.Done()
	if err := r.prepareAndStart(ctx, run); err != nil {
		r.failRun(run.ID, sanitizeError(err))
	}
}

func (r *runManager) prepareAndStart(ctx context.Context, run *domain.AgentRun) error {
	s := r.service
	r.updateRun(run.ID, func(r *domain.AgentRun) {
		r.Status = domain.RunStatusPreparing
		r.LastActivityAt = time.Now()
	})

	s.mu.RLock()
	issue := snapshotIssue(run.Issue)
	s.mu.RUnlock()

	prepared, err := r.prepareWorkspaceForIssue(ctx, issue, run.AgentName)
	if err != nil {
		return fmt.Errorf("prepare workspace: %w", err)
	}
	runtimeAgent, err := s.resolveRuntimeAgent(prepared.Path)
	if err != nil {
		return fmt.Errorf("resolve runtime agent: %w", err)
	}
	if err := s.workspace.PopulateHarnessConfig(prepared.Path, runtimeAgent.PackClaudeDir, runtimeAgent.PackCodexDir); err != nil {
		return fmt.Errorf("populate harness config: %w", err)
	}

	r.updateRun(run.ID, func(r *domain.AgentRun) {
		r.WorkspacePath = prepared.Path
	})
	s.runHookBestEffort(ctx, s.cfg.Hooks.AfterCreate, prepared.Path, run, "after_create")
	operatorInstruction, err := r.runBeforeWorkGate(ctx, run)
	if err != nil {
		return err
	}
	if err := s.runHook(ctx, s.cfg.Hooks.BeforeRun, prepared.Path, run, "before_run"); err != nil {
		return err
	}

	renderedPrompt, err := s.renderPrompt(runtimeAgent, issue, run.AgentName, run.Attempt, operatorInstruction)
	if err != nil {
		return fmt.Errorf("render prompt: %w", err)
	}

	var model, reasoning, threadSandbox string
	var maxTurns int
	var turnSandboxPolicy map[string]any
	var extraArgs []string

	switch runtimeAgent.Harness {
	case "codex":
		resolved := config.ResolveCodexConfig(s.cfg.CodexDefaults, runtimeAgent.Codex)
		model = resolved.Model
		reasoning = resolved.Reasoning
		maxTurns = resolved.MaxTurns
		threadSandbox = resolved.ThreadSandbox
		turnSandboxPolicy = resolved.TurnSandboxPolicy
		extraArgs = resolved.ExtraArgs
	case "claude-code":
		resolved := config.ResolveClaudeConfig(s.cfg.ClaudeDefaults, runtimeAgent.Claude)
		model = resolved.Model
		reasoning = resolved.Reasoning
		maxTurns = resolved.MaxTurns
		extraArgs = resolved.ExtraArgs
	}

	var continuationFunc func(ctx context.Context, turnNumber int) (string, bool, error)
	if runtimeAgent.Harness == "codex" && maxTurns > 1 {
		issueID := issue.ID
		prefix := s.labelPrefix()
		activeLabel := trackerbase.LifecycleLabel(prefix, trackerbase.LifecycleSuffixActive)
		sourceFilter := s.source.Filter
		continuationFunc = func(ctx context.Context, turnNumber int) (string, bool, error) {
			issue, err := s.tracker.Get(ctx, issueID)
			if err != nil {
				return "", false, err
			}
			if trackerbase.IsTerminal(issue) {
				return "", false, nil
			}
			if trackerbase.LifecycleLabelStateWithPrefix(issue.Labels, prefix) != activeLabel {
				return "", false, nil
			}
			if !trackerbase.MatchesFilterWithPrefix(issue, sourceFilter, prefix) {
				return "", false, nil
			}
			prompt := fmt.Sprintf(
				"Continuation turn %d of %d. Issue is still in active state %q.\nResume from current workspace state. Do not restate prior instructions.",
				turnNumber+1, maxTurns, issue.State,
			)
			return prompt, true, nil
		}
	}

	s.initRunOutput(run.ID)
	defer s.clearRunOutput(run.ID)

	var stdout, stderr bytes.Buffer
	defer func() {
		s.saveRunLogs(run.ID, stdout.Bytes(), stderr.Bytes())
	}()
	stdoutWriter := &runOutputWriter{
		target:  &stdout,
		onWrite: func() { s.markRunActivity(run.ID) },
		append:  func(p []byte) { s.appendRunOutput(run.ID, "stdout", p) },
	}
	stderrWriter := &runOutputWriter{
		target:  &stderr,
		onWrite: func() { s.markRunActivity(run.ID) },
		append:  func(p []byte) { s.appendRunOutput(run.ID, "stderr", p) },
	}
	active, err := s.harness.Start(ctx, harness.RunConfig{
		RunID:             run.ID,
		Prompt:            renderedPrompt,
		Workdir:           prepared.Path,
		ApprovalPolicy:    run.ApprovalPolicy,
		Env:               runtimeAgent.Env,
		Stdout:            stdoutWriter,
		Stderr:            stderrWriter,
		Model:             model,
		Reasoning:         reasoning,
		MaxTurns:          maxTurns,
		ExtraArgs:         extraArgs,
		ThreadSandbox:     threadSandbox,
		TurnSandboxPolicy: turnSandboxPolicy,
		ContinuationFunc:  continuationFunc,
	})
	if err != nil {
		return fmt.Errorf("start harness: %w", sanitizeError(err))
	}

	r.updateRun(run.ID, func(r *domain.AgentRun) {
		r.Status = domain.RunStatusActive
		r.LastActivityAt = time.Now()
	})
	s.recordRunEvent(run, "info", "agent %s started for %s", run.AgentName, issue.Identifier)

	if err := active.Wait(); err != nil {
		s.runHookBestEffort(context.Background(), s.cfg.Hooks.AfterRun, prepared.Path, run, "after_run")
		return fmt.Errorf(
			"agent exited with error: %w stderr=%s stdout=%s",
			sanitizeError(err),
			sanitizeOutput(stderr.String()),
			sanitizeOutput(stdout.String()),
		)
	}

	s.runHookBestEffort(context.Background(), s.cfg.Hooks.AfterRun, prepared.Path, run, "after_run")
	r.completeRun(run.ID)
	return nil
}

func snapshotIssue(issue domain.Issue) domain.Issue {
	issue.Labels = append([]string(nil), issue.Labels...)
	if issue.Meta != nil {
		meta := make(map[string]string, len(issue.Meta))
		for k, v := range issue.Meta {
			meta[k] = v
		}
		issue.Meta = meta
	}
	return issue
}

func (s *Service) resolveRuntimeAgent(workspacePath string) (config.AgentTypeConfig, error) {
	return resolveRuntimeAgent(s.agent, workspacePath)
}

func resolveRuntimeAgent(agent config.AgentTypeConfig, workspacePath string) (config.AgentTypeConfig, error) {
	if strings.TrimSpace(agent.RepoPackPath) == "" {
		if repoPackPath, ok := config.ParseRepoPackRef(agent.AgentPack); ok {
			agent.RepoPackPath = repoPackPath
		}
	}
	if strings.TrimSpace(agent.RepoPackPath) == "" {
		return agent, nil
	}
	pack, err := config.ResolveRepoPack(workspacePath, agent.RepoPackPath)
	if err != nil {
		return config.AgentTypeConfig{}, err
	}
	agent.Prompt = pack.Prompt
	agent.ContextFiles = append([]string(nil), pack.ContextFiles...)
	agent.Context = pack.Context
	agent.PackClaudeDir = pack.ClaudeDir
	agent.PackCodexDir = pack.CodexDir
	return agent, nil
}

func (r *runManager) prepareWorkspaceForIssue(ctx context.Context, issue domain.Issue, agentName string) (workspace.Prepared, error) {
	s := r.service
	switch s.agent.Workspace {
	case "git-clone":
		return s.workspace.PrepareClone(ctx, issue, agentName)
	case "none":
		return s.workspace.PrepareEmpty(issue)
	default:
		return workspace.Prepared{}, fmt.Errorf("unsupported workspace strategy %q", s.agent.Workspace)
	}
}

func (r *runManager) runBeforeWorkGate(ctx context.Context, run *domain.AgentRun) (string, error) {
	s := r.service
	if !s.cfg.Controls.BeforeWork.Enabled {
		return "", nil
	}

	body := strings.TrimSpace(s.cfg.Controls.BeforeWork.Prompt)
	if body == "" {
		body = fmt.Sprintf("Review %s before work begins. Reply with any operator instructions or simply say start.", run.Issue.Identifier)
	}
	summary := fmt.Sprintf("Before work: %s", run.Issue.Identifier)
	kind := "before_work_review"
	if strings.EqualFold(strings.TrimSpace(s.cfg.Controls.BeforeWork.Mode), "reply") {
		kind = "before_work_reply"
	}
	view, replyCh := s.createControlMessage(run, kind, summary, body)
	defer s.cancelControlMessage(view.RequestID, "cancelled")

	s.recordRunEvent(run, "info", "waiting for before_work confirmation for %s", run.Issue.Identifier)

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case reply, ok := <-replyCh:
			if !ok {
				return "", fmt.Errorf("before_work gate for %s was closed", run.ID)
			}
			return strings.TrimSpace(reply), nil
		case <-ticker.C:
			s.mu.RLock()
			_, stopped := s.pendingStops[run.ID]
			s.mu.RUnlock()
			if stopped {
				return "", fmt.Errorf("run stopped before work began")
			}
		}
	}
}
