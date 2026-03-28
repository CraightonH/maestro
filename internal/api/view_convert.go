package api

import (
	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/orchestrator"
)

func apiSnapshot(snapshot orchestrator.Snapshot) snapshotJSON {
	runs, outputs := apiRunsAndOutputs(snapshot.ActiveRuns, snapshot.RunOutputs)
	outputMap := outputsByRunID(outputs)

	var activeRun *runJSON
	if snapshot.ActiveRun != nil {
		encoded := apiRun(*snapshot.ActiveRun, outputMap[snapshot.ActiveRun.ID])
		activeRun = &encoded
	}

	return snapshotJSON{
		SourceName:       snapshot.SourceName,
		SourceTracker:    snapshot.SourceTracker,
		LastPollAt:       snapshot.LastPollAt,
		LastPollCount:    snapshot.LastPollCount,
		ClaimedCount:     snapshot.ClaimedCount,
		RetryCount:       snapshot.RetryCount,
		PendingApprovals: apiApprovals(snapshot.PendingApprovals),
		PendingMessages:  apiMessages(snapshot.PendingMessages),
		Retries:          apiRetries(snapshot.Retries),
		ApprovalHistory:  apiApprovalHistory(snapshot.ApprovalHistory),
		MessageHistory:   apiMessageHistory(snapshot.MessageHistory),
		ActiveRun:        activeRun,
		ActiveRuns:       runs,
		RunOutputs:       outputs,
		SourceSummaries:  apiSourceSummaries(snapshot.SourceSummaries),
		RecentEvents:     apiEvents(snapshot.RecentEvents),
	}
}

func apiSourceSummaries(items []orchestrator.SourceSummary) []sourceSummaryJSON {
	out := make([]sourceSummaryJSON, 0, len(items))
	for _, item := range items {
		out = append(out, sourceSummaryJSON{
			Name:             item.Name,
			DisplayGroup:     item.DisplayGroup,
			Tags:             append([]string(nil), item.Tags...),
			Tracker:          item.Tracker,
			LastPollAt:       item.LastPollAt,
			LastPollCount:    item.LastPollCount,
			ClaimedCount:     item.ClaimedCount,
			RetryCount:       item.RetryCount,
			ActiveRunCount:   item.ActiveRunCount,
			PendingApprovals: item.PendingApprovals,
			PendingMessages:  item.PendingMessages,
		})
	}
	return out
}

func apiRunsAndOutputs(runs []domain.AgentRun, outputs []orchestrator.RunOutputView) ([]runJSON, []runOutputJSON) {
	encodedOutputs := apiRunOutputs(outputs)
	outputMap := outputsByRunID(encodedOutputs)

	out := make([]runJSON, 0, len(runs))
	for _, run := range runs {
		out = append(out, apiRun(run, outputMap[run.ID]))
	}
	return out, encodedOutputs
}

func apiRun(run domain.AgentRun, output *runOutputJSON) runJSON {
	return runJSON{
		ID:             run.ID,
		AgentName:      run.AgentName,
		AgentType:      run.AgentType,
		Issue:          apiIssue(run.Issue),
		SourceName:     run.SourceName,
		HarnessKind:    run.HarnessKind,
		WorkspacePath:  run.WorkspacePath,
		Status:         string(run.Status),
		Attempt:        run.Attempt,
		ApprovalPolicy: run.ApprovalPolicy,
		ApprovalState:  string(run.ApprovalState),
		StartedAt:      run.StartedAt,
		LastActivityAt: run.LastActivityAt,
		CompletedAt:    run.CompletedAt,
		Error:          run.Error,
		Output:         output,
	}
}

func apiIssue(issue domain.Issue) issueJSON {
	return issueJSON{
		ID:          issue.ID,
		Identifier:  issue.Identifier,
		Title:       issue.Title,
		Description: issue.Description,
		URL:         issue.URL,
		State:       issue.State,
		Labels:      append([]string(nil), issue.Labels...),
		UpdatedAt:   issue.UpdatedAt,
	}
}

func apiRunOutputs(items []orchestrator.RunOutputView) []runOutputJSON {
	out := make([]runOutputJSON, 0, len(items))
	for _, item := range items {
		out = append(out, runOutputJSON{
			RunID:           item.RunID,
			SourceName:      item.SourceName,
			IssueIdentifier: item.IssueIdentifier,
			StdoutTail:      item.StdoutTail,
			StderrTail:      item.StderrTail,
			UpdatedAt:       item.UpdatedAt,
		})
	}
	return out
}

func apiRetries(items []orchestrator.RetryView) []retryJSON {
	out := make([]retryJSON, 0, len(items))
	for _, item := range items {
		out = append(out, retryJSON{
			IssueID:         item.IssueID,
			IssueIdentifier: item.IssueIdentifier,
			SourceName:      item.SourceName,
			Attempt:         item.Attempt,
			DueAt:           item.DueAt,
			Error:           item.Error,
		})
	}
	return out
}

func apiApprovals(items []orchestrator.ApprovalView) []approvalJSON {
	out := make([]approvalJSON, 0, len(items))
	for _, item := range items {
		out = append(out, approvalJSON{
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
		})
	}
	return out
}

func apiApprovalHistory(items []orchestrator.ApprovalHistoryEntry) []approvalHistoryJSON {
	out := make([]approvalHistoryJSON, 0, len(items))
	for _, item := range items {
		out = append(out, approvalHistoryJSON{
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
		})
	}
	return out
}

func apiMessages(items []orchestrator.MessageView) []messageJSON {
	out := make([]messageJSON, 0, len(items))
	for _, item := range items {
		out = append(out, messageJSON{
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
		})
	}
	return out
}

func apiMessageHistory(items []orchestrator.MessageHistoryEntry) []messageHistoryJSON {
	out := make([]messageHistoryJSON, 0, len(items))
	for _, item := range items {
		out = append(out, messageHistoryJSON{
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
		})
	}
	return out
}

func apiEvents(items []orchestrator.Event) []eventJSON {
	out := make([]eventJSON, 0, len(items))
	for _, item := range items {
		out = append(out, eventJSON{
			Time:    item.Time,
			Level:   item.Level,
			Source:  item.Source,
			RunID:   item.RunID,
			Issue:   item.Issue,
			Message: item.Message,
		})
	}
	return out
}
