package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/harness"
)

type approvalRouter struct {
	service *Service
}

func (s *Service) ResolveApproval(requestID string, decision string) error {
	return s.approvalMgr.resolveApproval(requestID, decision)
}

func (r *approvalRouter) startWatcher(ctx context.Context) {
	s := r.service
	approvals := s.harness.Approvals()
	if approvals == nil {
		return
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case request, ok := <-approvals:
				if !ok {
					return
				}
				r.recordApprovalRequest(request)
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(approvalTimeoutPollInterval(s.agent.ApprovalTimeout.Duration))
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				expired := r.expireTimedOutApprovals(time.Now())
				if len(expired) == 0 {
					continue
				}
				for _, approval := range expired {
					s.recordRunEventByFields("warn", s.source.Name, approval.RunID, approval.IssueIdentifier, "approval timed out for %s (%s)", approval.RunID, approval.ToolName)
					r.stopRunForTimedOutApproval(approval)
				}
				_ = s.stateMgr.saveStateBestEffort()
			}
		}
	}()
}

func (r *approvalRouter) recordApprovalRequest(request harness.ApprovalRequest) {
	s := r.service
	view := ApprovalView{
		RequestID:      request.RequestID,
		RunID:          request.RunID,
		ToolName:       request.ToolName,
		ToolInput:      request.ToolInput,
		ApprovalPolicy: request.ApprovalPolicy,
		RequestedAt:    request.RequestedAt,
		Resolvable:     true,
	}

	s.mu.Lock()
	if run := s.activeRunByIDLocked(request.RunID); run != nil {
		view.IssueID = run.Issue.ID
		view.IssueIdentifier = run.Issue.Identifier
		view.AgentName = run.AgentName
		run.LastActivityAt = time.Now()
	}
	s.approvals[request.RequestID] = view
	s.approvalOrder = append(s.approvalOrder, request.RequestID)
	if run := s.activeRunByIDLocked(request.RunID); run != nil {
		run.Status = domain.RunStatusAwaiting
		run.ApprovalState = domain.ApprovalStateAwaiting
	}
	s.mu.Unlock()

	s.recordRunEventByFields("warn", s.source.Name, request.RunID, view.IssueIdentifier, "approval requested for %s (%s)", request.RunID, request.ToolName)
	_ = s.stateMgr.saveStateBestEffort()
}

func (r *approvalRouter) resolveApproval(requestID string, decision string) error {
	s := r.service
	request, err := r.claimApprovalResolution(requestID)
	if err != nil {
		return err
	}

	controlCtx, cancel := withHarnessControlTimeout()
	defer cancel()

	if err := s.harness.Approve(controlCtx, harness.ApprovalDecision{
		RequestID: requestID,
		Decision:  decision,
	}); err != nil {
		r.restoreApprovalResolution(requestID)
		s.recordRunEventByFields("error", s.source.Name, request.RunID, request.IssueIdentifier, "approval %s for %s failed: %v", decision, request.RunID, err)
		return err
	}

	now := time.Now()
	history := ApprovalHistoryEntry{
		RequestID:       request.RequestID,
		RunID:           request.RunID,
		IssueID:         request.IssueID,
		IssueIdentifier: request.IssueIdentifier,
		AgentName:       request.AgentName,
		ToolName:        request.ToolName,
		ApprovalPolicy:  request.ApprovalPolicy,
		Decision:        decision,
		RequestedAt:     request.RequestedAt,
		DecidedAt:       now,
		Outcome:         "resolved",
	}

	s.mu.Lock()
	delete(s.approvals, requestID)
	s.approvalOrder = removeFromOrder(s.approvalOrder, requestID)
	if run := s.activeRunByIDLocked(request.RunID); run != nil {
		if decision == harness.DecisionApprove {
			run.Status = domain.RunStatusActive
			run.ApprovalState = domain.ApprovalStateApproved
		} else {
			run.Status = domain.RunStatusAwaiting
			run.ApprovalState = domain.ApprovalStateRejected
		}
		run.LastActivityAt = now
	}
	r.appendApprovalHistory(history)
	s.mu.Unlock()

	s.recordRunEventByFields("info", s.source.Name, request.RunID, request.IssueIdentifier, "approval %s for %s (%s)", decision, request.RunID, request.ToolName)
	_ = s.stateMgr.saveStateBestEffort()
	return nil
}

func (r *approvalRouter) claimApprovalResolution(requestID string) (ApprovalView, error) {
	s := r.service
	s.mu.Lock()
	defer s.mu.Unlock()

	request, ok := s.approvals[requestID]
	if !ok {
		return ApprovalView{}, fmt.Errorf("approval request %q: %w", requestID, ErrApprovalNotFound)
	}
	if !request.Resolvable {
		return ApprovalView{}, fmt.Errorf("approval request %q is already being resolved", requestID)
	}
	request.Resolvable = false
	s.approvals[requestID] = request
	return request, nil
}

func (r *approvalRouter) restoreApprovalResolution(requestID string) {
	s := r.service
	s.mu.Lock()
	defer s.mu.Unlock()

	request, ok := s.approvals[requestID]
	if !ok {
		return
	}
	request.Resolvable = true
	s.approvals[requestID] = request
}

func (r *approvalRouter) expireTimedOutApprovals(now time.Time) []ApprovalView {
	s := r.service
	timeout := s.agent.ApprovalTimeout.Duration
	if timeout <= 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.approvalOrder) == 0 {
		return nil
	}

	expired := make([]ApprovalView, 0, len(s.approvalOrder))
	kept := make([]string, 0, len(s.approvalOrder))
	for _, requestID := range s.approvalOrder {
		approval, ok := s.approvals[requestID]
		if !ok {
			continue
		}
		if !approval.Resolvable {
			kept = append(kept, requestID)
			continue
		}
		if approval.RequestedAt.IsZero() || now.Before(approval.RequestedAt.Add(timeout)) {
			kept = append(kept, requestID)
			continue
		}

		expired = append(expired, approval)
		r.appendApprovalHistory(ApprovalHistoryEntry{
			RequestID:       approval.RequestID,
			RunID:           approval.RunID,
			IssueID:         approval.IssueID,
			IssueIdentifier: approval.IssueIdentifier,
			AgentName:       approval.AgentName,
			ToolName:        approval.ToolName,
			ApprovalPolicy:  approval.ApprovalPolicy,
			Decision:        harness.DecisionReject,
			Reason:          "approval timeout",
			RequestedAt:     approval.RequestedAt,
			DecidedAt:       now,
			Outcome:         "timed_out",
		})
		if run := s.activeRunByIDLocked(approval.RunID); run != nil {
			run.ApprovalState = domain.ApprovalStateRejected
			run.LastActivityAt = now
		}
		delete(s.approvals, requestID)
	}
	s.approvalOrder = kept
	return expired
}

func approvalTimeoutPollInterval(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return time.Second
	}

	interval := timeout / 4
	if interval < 50*time.Millisecond {
		return 50 * time.Millisecond
	}
	if interval > time.Second {
		return time.Second
	}
	return interval
}

func (r *approvalRouter) stopRunForTimedOutApproval(approval ApprovalView) {
	s := r.service
	s.mu.RLock()
	active := s.activeRunByIDLocked(approval.RunID) != nil
	s.mu.RUnlock()
	if !active {
		return
	}

	if err := s.StopRun(approval.RunID, approvalTimeoutFailureReason(approval)); err != nil {
		s.recordRunEventByFields("error", s.source.Name, approval.RunID, approval.IssueIdentifier, "stop run %s after approval timeout failed: %v", approval.RunID, err)
	}
}

func approvalTimeoutFailureReason(approval ApprovalView) string {
	if approval.ToolName == "" {
		return "approval timeout"
	}
	return fmt.Sprintf("approval timeout while waiting on %s", approval.ToolName)
}

func (r *approvalRouter) appendApprovalHistory(entry ApprovalHistoryEntry) {
	s := r.service
	s.approvalHistory = append(s.approvalHistory, entry)
	if len(s.approvalHistory) > maxApprovalHistory {
		s.approvalHistory = s.approvalHistory[len(s.approvalHistory)-maxApprovalHistory:]
	}
}
