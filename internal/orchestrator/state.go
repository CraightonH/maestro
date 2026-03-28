package orchestrator

import (
	"errors"
	"fmt"
	"time"

	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/state"
)

type stateManager struct {
	service *Service
}

func (m *stateManager) restoreState() error {
	s := m.service
	snapshot, err := s.stateStore.Load()
	if err != nil {
		var corruptErr *state.CorruptStateError
		if !errors.As(err, &corruptErr) {
			return err
		}
		if corruptErr.ArchivedPath != "" {
			s.logger.Warn("state file unreadable; archived and continuing with empty recovery metadata", "path", corruptErr.Path, "archived_path", corruptErr.ArchivedPath, "error", corruptErr.Err)
		} else {
			s.logger.Warn("state file unreadable; continuing with empty recovery metadata", "path", corruptErr.Path, "error", corruptErr.Err)
		}
	}
	now := time.Now()

	s.mu.Lock()
	s.finished = snapshot.Finished
	s.retryQueue = snapshot.RetryQueue
	s.approvals = make(map[string]ApprovalView, len(snapshot.PendingApprovals))
	s.approvalOrder = s.approvalOrder[:0]
	for _, approval := range snapshot.PendingApprovals {
		view := approvalViewFromPersisted(approval)
		if !view.Resolvable {
			view.Resolvable = true
		}
		s.approvals[view.RequestID] = view
		s.approvalOrder = append(s.approvalOrder, view.RequestID)
	}
	s.approvalHistory = s.approvalHistory[:0]
	for _, approval := range snapshot.ApprovalHistory {
		s.approvalHistory = append(s.approvalHistory, approvalHistoryEntryFromPersisted(approval))
	}
	s.messages = make(map[string]MessageView, len(snapshot.PendingMessages))
	s.messageOrder = s.messageOrder[:0]
	for _, message := range snapshot.PendingMessages {
		view := messageViewFromPersisted(message)
		s.messages[view.RequestID] = view
		s.messageOrder = append(s.messageOrder, view.RequestID)
	}
	s.messageHistory = s.messageHistory[:0]
	for _, message := range snapshot.MessageHistory {
		s.messageHistory = append(s.messageHistory, messageHistoryEntryFromPersisted(message))
	}
	s.mu.Unlock()

	expiredApprovals := s.approvalMgr.expireTimedOutApprovals(now)

	if snapshot.ActiveRun == nil {
		if len(expiredApprovals) > 0 || m.expirePendingApprovals("restart without active run") || m.expirePendingMessages("restart without active run") {
			return m.saveStateBestEffort()
		}
		return nil
	}

	if hasTimedOutApprovalForRun(expiredApprovals, snapshot.ActiveRun.RunID) {
		s.mu.Lock()
		s.finished[snapshot.ActiveRun.IssueID] = state.TerminalIssue{
			IssueID:        snapshot.ActiveRun.IssueID,
			Identifier:     snapshot.ActiveRun.Identifier,
			Status:         domain.RunStatusFailed,
			Attempt:        snapshot.ActiveRun.Attempt,
			IssueUpdatedAt: snapshot.ActiveRun.IssueUpdatedAt,
			FinishedAt:     now,
			Error:          "approval timeout",
		}
		s.mu.Unlock()
		_ = m.expirePendingApprovals("restart after approval timeout")
		_ = m.expirePendingMessages("restart after approval timeout")
		s.recordEvent("warn", "recovered active run %s after approval timeout", snapshot.ActiveRun.RunID)
		return m.saveStateBestEffort()
	}

	nextAttempt := snapshot.ActiveRun.Attempt + 1
	if nextAttempt >= s.cfg.State.MaxAttempts {
		s.mu.Lock()
		s.finished[snapshot.ActiveRun.IssueID] = state.TerminalIssue{
			IssueID:        snapshot.ActiveRun.IssueID,
			Identifier:     snapshot.ActiveRun.Identifier,
			Status:         domain.RunStatusFailed,
			Attempt:        snapshot.ActiveRun.Attempt,
			IssueUpdatedAt: snapshot.ActiveRun.IssueUpdatedAt,
			FinishedAt:     time.Now(),
			Error:          "run interrupted during shutdown or restart",
		}
		s.mu.Unlock()
		_ = m.expirePendingApprovals("restart after interrupted run")
		_ = m.expirePendingMessages("restart after interrupted run")
		s.recordEvent("warn", "recovered active run %s but max attempts reached", snapshot.ActiveRun.RunID)
		return m.saveStateBestEffort()
	}

	s.mu.Lock()
	s.retryQueue[snapshot.ActiveRun.IssueID] = state.RetryEntry{
		IssueID:        snapshot.ActiveRun.IssueID,
		Identifier:     snapshot.ActiveRun.Identifier,
		Attempt:        nextAttempt,
		DueAt:          time.Now(),
		Error:          "recovered active run after restart",
		IssueUpdatedAt: snapshot.ActiveRun.IssueUpdatedAt,
	}
	s.mu.Unlock()

	_ = m.expirePendingApprovals("restart after interrupted run")
	_ = m.expirePendingMessages("restart after interrupted run")
	s.recordEvent("warn", "recovered active run %s as retry attempt %d", snapshot.ActiveRun.RunID, nextAttempt)
	return m.saveStateBestEffort()
}

func hasTimedOutApprovalForRun(expired []ApprovalView, runID string) bool {
	for _, approval := range expired {
		if approval.RunID == runID {
			return true
		}
	}
	return false
}

func (m *stateManager) saveStateBestEffort() error {
	s := m.service
	if err := s.stateStore.Save(m.snapshotState()); err != nil {
		s.logger.Warn("persist state failed", "path", s.stateStore.Path(), "error", err)
		return err
	}
	return nil
}

func (m *stateManager) snapshotState() state.Snapshot {
	s := m.service
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := state.Snapshot{
		Finished:   make(map[string]state.TerminalIssue, len(s.finished)),
		RetryQueue: make(map[string]state.RetryEntry, len(s.retryQueue)),
	}
	for issueID, finished := range s.finished {
		snapshot.Finished[issueID] = finished
	}
	for issueID, retry := range s.retryQueue {
		snapshot.RetryQueue[issueID] = retry
	}
	for _, requestID := range s.approvalOrder {
		if approval, ok := s.approvals[requestID]; ok {
			persisted := persistedApprovalRequestFromView(approval)
			persisted.Resolvable = true
			snapshot.PendingApprovals = append(snapshot.PendingApprovals, persisted)
		}
	}
	for _, entry := range s.approvalHistory {
		snapshot.ApprovalHistory = append(snapshot.ApprovalHistory, persistedApprovalDecisionFromHistory(entry))
	}
	for _, requestID := range s.messageOrder {
		if message, ok := s.messages[requestID]; ok {
			snapshot.PendingMessages = append(snapshot.PendingMessages, persistedMessageRequestFromView(message))
		}
	}
	for _, entry := range s.messageHistory {
		snapshot.MessageHistory = append(snapshot.MessageHistory, persistedMessageReplyFromHistory(entry))
	}
	snapshot.ActiveRun = persistedRunFromAgentRun(s.activeRun)
	return snapshot
}

func (m *stateManager) expirePendingApprovals(reason string) bool {
	s := m.service
	s.mu.Lock()
	if len(s.approvalOrder) == 0 {
		s.mu.Unlock()
		return false
	}
	for _, requestID := range s.approvalOrder {
		approval, ok := s.approvals[requestID]
		if !ok {
			continue
		}
		s.approvalMgr.appendApprovalHistory(ApprovalHistoryEntry{
			RequestID:       approval.RequestID,
			RunID:           approval.RunID,
			IssueID:         approval.IssueID,
			IssueIdentifier: approval.IssueIdentifier,
			AgentName:       approval.AgentName,
			ToolName:        approval.ToolName,
			ApprovalPolicy:  approval.ApprovalPolicy,
			Decision:        "stale",
			Reason:          reason,
			RequestedAt:     approval.RequestedAt,
			DecidedAt:       time.Now(),
			Outcome:         "stale_restart",
		})
	}
	s.approvals = map[string]ApprovalView{}
	s.approvalOrder = nil
	s.mu.Unlock()
	return true
}

func (m *stateManager) expirePendingMessages(reason string) bool {
	s := m.service
	s.mu.Lock()
	if len(s.messageOrder) == 0 {
		s.mu.Unlock()
		return false
	}
	for _, requestID := range s.messageOrder {
		message, ok := s.messages[requestID]
		if !ok {
			continue
		}
		s.messageMgr.appendMessageHistory(MessageHistoryEntry{
			RequestID:       message.RequestID,
			RunID:           message.RunID,
			IssueID:         message.IssueID,
			IssueIdentifier: message.IssueIdentifier,
			AgentName:       message.AgentName,
			Kind:            message.Kind,
			Summary:         message.Summary,
			Body:            message.Body,
			Reply:           "",
			ResolvedVia:     "maestro",
			RequestedAt:     message.RequestedAt,
			RepliedAt:       time.Now(),
			Outcome:         "stale_restart",
		})
	}
	s.messages = map[string]MessageView{}
	s.messageOrder = nil
	s.mu.Unlock()
	return true
}

func (m *stateManager) shouldSkipIssue(issue domain.Issue) bool {
	s := m.service
	changed := false
	skip := false

	s.mu.Lock()
	if finished, ok := s.finished[issue.ID]; ok {
		if issueRecordStale(issue.UpdatedAt, finished.IssueUpdatedAt) {
			delete(s.finished, issue.ID)
			changed = true
		} else {
			skip = true
		}
	}
	if !skip {
		if retry, ok := s.retryQueue[issue.ID]; ok {
			if issueRecordStale(issue.UpdatedAt, retry.IssueUpdatedAt) {
				delete(s.retryQueue, issue.ID)
				changed = true
			} else if time.Now().Before(retry.DueAt) {
				skip = true
			}
		}
	}
	s.mu.Unlock()

	if changed {
		_ = m.saveStateBestEffort()
	}
	return skip
}

func (m *stateManager) takeAttempt(issue domain.Issue) int {
	s := m.service
	s.mu.Lock()
	defer s.mu.Unlock()

	retry, ok := s.retryQueue[issue.ID]
	if !ok {
		return 0
	}
	delete(s.retryQueue, issue.ID)
	return retry.Attempt
}

func (m *stateManager) scheduleRetry(run *domain.AgentRun, err error) bool {
	s := m.service
	nextAttempt := run.Attempt + 1
	if nextAttempt >= s.source.EffectiveMaxAttempts(s.cfg.State) {
		return false
	}

	s.retryQueue[run.Issue.ID] = state.RetryEntry{
		IssueID:        run.Issue.ID,
		Identifier:     run.Issue.Identifier,
		Attempt:        nextAttempt,
		DueAt:          time.Now().Add(m.retryBackoff(nextAttempt)),
		Error:          sanitizeOutput(err.Error()),
		IssueUpdatedAt: run.Issue.UpdatedAt,
		WorkspacePath:  run.WorkspacePath,
	}
	return true
}

func (m *stateManager) retryBackoff(attempt int) time.Duration {
	s := m.service
	if attempt <= 0 {
		return 0
	}

	backoff := s.source.EffectiveRetryBase(s.cfg.State)
	for i := 1; i < attempt; i++ {
		backoff *= 2
		if backoff >= s.source.EffectiveMaxRetryBackoff(s.cfg.State) {
			return s.source.EffectiveMaxRetryBackoff(s.cfg.State)
		}
	}
	return backoff
}

func (m *stateManager) approvalState() domain.ApprovalState {
	s := m.service
	switch s.agent.ApprovalPolicy {
	case "auto":
		return domain.ApprovalStateApproved
	default:
		return domain.ApprovalStateAwaiting
	}
}

func issueRecordStale(issueUpdatedAt time.Time, recordUpdatedAt time.Time) bool {
	return !issueUpdatedAt.IsZero() && !recordUpdatedAt.IsZero() && issueUpdatedAt.After(recordUpdatedAt)
}

func (m *stateManager) statusSummary() string {
	snapshot := m.service.Snapshot()
	return fmt.Sprintf("claimed=%d retry=%d", snapshot.ClaimedCount, snapshot.RetryCount)
}
