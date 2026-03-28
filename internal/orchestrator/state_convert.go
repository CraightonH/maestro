package orchestrator

import (
	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/state"
)

func approvalViewFromPersisted(item state.PersistedApprovalRequest) ApprovalView {
	return ApprovalView{
		RequestID:       item.RequestID,
		RunID:           item.RunID,
		IssueID:         item.IssueID,
		IssueIdentifier: item.IssueIdentifier,
		AgentName:       item.AgentName,
		ToolName:        item.ToolName,
		ToolInput:       item.ToolInput,
		ApprovalPolicy:  item.ApprovalPolicy,
		RequestedAt:     item.RequestedAt,
		Resolvable:      item.Resolvable,
	}
}

func persistedApprovalRequestFromView(item ApprovalView) state.PersistedApprovalRequest {
	return state.PersistedApprovalRequest{
		RequestID:       item.RequestID,
		RunID:           item.RunID,
		IssueID:         item.IssueID,
		IssueIdentifier: item.IssueIdentifier,
		AgentName:       item.AgentName,
		ToolName:        item.ToolName,
		ToolInput:       item.ToolInput,
		ApprovalPolicy:  item.ApprovalPolicy,
		RequestedAt:     item.RequestedAt,
		Resolvable:      item.Resolvable,
	}
}

func approvalHistoryEntryFromPersisted(item state.PersistedApprovalDecision) ApprovalHistoryEntry {
	return ApprovalHistoryEntry{
		RequestID:       item.RequestID,
		RunID:           item.RunID,
		IssueID:         item.IssueID,
		IssueIdentifier: item.IssueIdentifier,
		AgentName:       item.AgentName,
		ToolName:        item.ToolName,
		ApprovalPolicy:  item.ApprovalPolicy,
		Decision:        item.Decision,
		Reason:          item.Reason,
		RequestedAt:     item.RequestedAt,
		DecidedAt:       item.DecidedAt,
		Outcome:         item.Outcome,
	}
}

func persistedApprovalDecisionFromHistory(item ApprovalHistoryEntry) state.PersistedApprovalDecision {
	return state.PersistedApprovalDecision{
		RequestID:       item.RequestID,
		RunID:           item.RunID,
		IssueID:         item.IssueID,
		IssueIdentifier: item.IssueIdentifier,
		AgentName:       item.AgentName,
		ToolName:        item.ToolName,
		ApprovalPolicy:  item.ApprovalPolicy,
		Decision:        item.Decision,
		Reason:          item.Reason,
		RequestedAt:     item.RequestedAt,
		DecidedAt:       item.DecidedAt,
		Outcome:         item.Outcome,
	}
}

func messageViewFromPersisted(item state.PersistedMessageRequest) MessageView {
	return MessageView{
		RequestID:       item.RequestID,
		RunID:           item.RunID,
		IssueID:         item.IssueID,
		IssueIdentifier: item.IssueIdentifier,
		SourceName:      item.SourceName,
		AgentName:       item.AgentName,
		Kind:            item.Kind,
		Summary:         item.Summary,
		Body:            item.Body,
		RequestedAt:     item.RequestedAt,
		Resolvable:      item.Resolvable,
	}
}

func persistedMessageRequestFromView(item MessageView) state.PersistedMessageRequest {
	return state.PersistedMessageRequest{
		RequestID:       item.RequestID,
		RunID:           item.RunID,
		IssueID:         item.IssueID,
		IssueIdentifier: item.IssueIdentifier,
		SourceName:      item.SourceName,
		AgentName:       item.AgentName,
		Kind:            item.Kind,
		Summary:         item.Summary,
		Body:            item.Body,
		RequestedAt:     item.RequestedAt,
		Resolvable:      item.Resolvable,
	}
}

func messageHistoryEntryFromPersisted(item state.PersistedMessageReply) MessageHistoryEntry {
	return MessageHistoryEntry{
		RequestID:       item.RequestID,
		RunID:           item.RunID,
		IssueID:         item.IssueID,
		IssueIdentifier: item.IssueIdentifier,
		SourceName:      item.SourceName,
		AgentName:       item.AgentName,
		Kind:            item.Kind,
		Summary:         item.Summary,
		Body:            item.Body,
		Reply:           item.Reply,
		ResolvedVia:     item.ResolvedVia,
		RequestedAt:     item.RequestedAt,
		RepliedAt:       item.RepliedAt,
		Outcome:         item.Outcome,
	}
}

func persistedMessageReplyFromHistory(item MessageHistoryEntry) state.PersistedMessageReply {
	return state.PersistedMessageReply{
		RequestID:       item.RequestID,
		RunID:           item.RunID,
		IssueID:         item.IssueID,
		IssueIdentifier: item.IssueIdentifier,
		SourceName:      item.SourceName,
		AgentName:       item.AgentName,
		Kind:            item.Kind,
		Summary:         item.Summary,
		Body:            item.Body,
		Reply:           item.Reply,
		ResolvedVia:     item.ResolvedVia,
		RequestedAt:     item.RequestedAt,
		RepliedAt:       item.RepliedAt,
		Outcome:         item.Outcome,
	}
}

func persistedRunFromAgentRun(run *domain.AgentRun) *state.PersistedRun {
	if run == nil {
		return nil
	}
	return &state.PersistedRun{
		RunID:          run.ID,
		IssueID:        run.Issue.ID,
		Identifier:     run.Issue.Identifier,
		Status:         run.Status,
		Attempt:        run.Attempt,
		WorkspacePath:  run.WorkspacePath,
		StartedAt:      run.StartedAt,
		LastActivityAt: run.LastActivityAt,
		IssueUpdatedAt: run.Issue.UpdatedAt,
	}
}
