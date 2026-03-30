package api

import (
	"strings"

	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/orchestrator"
)

func apiSnapshot(snapshot orchestrator.Snapshot) snapshotJSON {
	executions := executionBySource(snapshot.SourceSummaries)
	runs, outputs := apiRunsAndOutputs(snapshot.ActiveRuns, snapshot.RunOutputs, executions)
	outputMap := outputsByRunID(outputs)

	var activeRun *runJSON
	if len(snapshot.ActiveRuns) > 0 {
		first := snapshot.ActiveRuns[0]
		encoded := apiRun(first, outputMap[first.ID], executions[first.SourceName])
		activeRun = &encoded
	} else if snapshot.ActiveRun != nil {
		encoded := apiRun(*snapshot.ActiveRun, outputMap[snapshot.ActiveRun.ID], executions[snapshot.ActiveRun.SourceName])
		activeRun = &encoded
	}

	return snapshotJSON{
		SourceName:       snapshot.SourceName,
		SourceTracker:    snapshot.SourceTracker,
		LastPollAt:       snapshot.LastPollAt,
		LastPollCount:    snapshot.LastPollCount,
		ClaimedCount:     snapshot.ClaimedCount,
		RetryCount:       snapshot.RetryCount,
		InstanceMetrics:  apiRunMetrics(snapshot.InstanceMetrics),
		HarnessMetrics:   apiMetricBreakdowns(snapshot.HarnessMetrics),
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
			Name:                   item.Name,
			DisplayGroup:           item.DisplayGroup,
			Tags:                   append([]string(nil), item.Tags...),
			Tracker:                item.Tracker,
			RateLimit:              apiTrackerRateLimit(item.RateLimit),
			Execution:              apiExecution(item.Execution),
			LastPollAt:             item.LastPollAt,
			LastPollCount:          item.LastPollCount,
			ClaimedCount:           item.ClaimedCount,
			RetryCount:             item.RetryCount,
			ActiveRunCount:         item.ActiveRunCount,
			MaxActiveRuns:          item.MaxActiveRuns,
			AgentMaxConcurrent:     item.AgentMaxConcurrent,
			GlobalMaxConcurrent:    item.GlobalMaxConcurrent,
			EffectiveMaxConcurrent: item.EffectiveMaxConcurrent,
			Metrics:                apiRunMetrics(item.Metrics),
			PendingApprovals:       item.PendingApprovals,
			PendingMessages:        item.PendingMessages,
		})
	}
	return out
}

func apiMetricBreakdowns(items []orchestrator.MetricBreakdown) []metricBreakdownJSON {
	out := make([]metricBreakdownJSON, 0, len(items))
	for _, item := range items {
		out = append(out, metricBreakdownJSON{
			Name:    item.Name,
			Metrics: apiRunMetrics(item.Metrics),
		})
	}
	return out
}

func apiRunsAndOutputs(runs []domain.AgentRun, outputs []orchestrator.RunOutputView, executions map[string]*executionJSON) ([]runJSON, []runOutputJSON) {
	encodedOutputs := apiRunOutputs(outputs)
	outputMap := outputsByRunID(encodedOutputs)

	out := make([]runJSON, 0, len(runs))
	for _, run := range runs {
		out = append(out, apiRun(run, outputMap[run.ID], executions[run.SourceName]))
	}
	return out, encodedOutputs
}

func apiRun(run domain.AgentRun, output *runOutputJSON, execution *executionJSON) runJSON {
	runExecution := mergeRunExecution(execution, run.Execution)
	return runJSON{
		ID:             run.ID,
		AgentName:      run.AgentName,
		AgentType:      run.AgentType,
		Issue:          apiIssue(run.Issue),
		SourceName:     run.SourceName,
		HarnessKind:    run.HarnessKind,
		Execution:      runExecution,
		WorkspacePath:  run.WorkspacePath,
		Status:         string(run.Status),
		CurrentTurn:    run.CurrentTurn,
		MaxTurns:       run.MaxTurns,
		Attempt:        run.Attempt,
		ApprovalPolicy: run.ApprovalPolicy,
		ApprovalState:  string(run.ApprovalState),
		StartedAt:      run.StartedAt,
		LastActivityAt: run.LastActivityAt,
		CompletedAt:    run.CompletedAt,
		Metrics:        apiRunMetrics(run.Metrics),
		Error:          run.Error,
		Output:         output,
	}
}

func apiExecution(item *orchestrator.ExecutionSummary) *executionJSON {
	if item == nil {
		return nil
	}
	return &executionJSON{
		Mode:              item.Mode,
		ReuseMode:         item.ReuseMode,
		Reused:            item.Reused,
		ContainerID:       item.ContainerID,
		ContainerName:     item.ContainerName,
		ProfileKey:        item.ProfileKey,
		LineageKey:        item.LineageKey,
		Image:             item.Image,
		Network:           item.Network,
		NetworkPolicyMode: item.NetworkPolicyMode,
		NetworkAllow:      append([]string(nil), item.NetworkAllow...),
		CPUs:              item.CPUs,
		Memory:            item.Memory,
		PIDsLimit:         item.PIDsLimit,
		AuthSource:        item.AuthSource,
		SecurityPreset:    item.SecurityPreset,
		EnvCount:          item.EnvCount,
		SecretMountCount:  item.SecretMountCount,
		ToolMountCount:    item.ToolMountCount,
	}
}

func mergeRunExecution(base *executionJSON, metadata *domain.RunExecutionMetadata) *executionJSON {
	if metadata == nil {
		return base
	}
	var merged executionJSON
	if base != nil {
		merged = *base
		merged.NetworkAllow = append([]string(nil), base.NetworkAllow...)
	}
	if strings.TrimSpace(metadata.Mode) != "" {
		merged.Mode = metadata.Mode
	}
	if metadata.ContainerReuse != nil {
		merged.ReuseMode = metadata.ContainerReuse.Mode
		merged.Reused = metadata.ContainerReuse.Reused
		merged.ContainerID = metadata.ContainerReuse.ContainerID
		merged.ContainerName = metadata.ContainerReuse.ContainerName
		merged.ProfileKey = metadata.ContainerReuse.ProfileKey
		merged.LineageKey = metadata.ContainerReuse.LineageKey
	}
	return &merged
}

func executionBySource(items []orchestrator.SourceSummary) map[string]*executionJSON {
	out := make(map[string]*executionJSON, len(items))
	for _, item := range items {
		if item.Execution == nil {
			continue
		}
		out[item.Name] = apiExecution(item.Execution)
	}
	return out
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

func apiRunMetrics(metrics domain.RunMetrics) *runMetricsJSON {
	if metrics.TokensIn == nil && metrics.TokensOut == nil && metrics.TotalTokens == nil && metrics.CostUSD == nil && metrics.DurationMS == nil && metrics.ThroughputTokensPerSecond == nil && metrics.UpdatedAt.IsZero() {
		return nil
	}
	return &runMetricsJSON{
		TokensIn:                  cloneInt64Ptr(metrics.TokensIn),
		TokensOut:                 cloneInt64Ptr(metrics.TokensOut),
		TotalTokens:               cloneInt64Ptr(metrics.TotalTokens),
		CostUSD:                   cloneFloat64Ptr(metrics.CostUSD),
		DurationMS:                cloneInt64Ptr(metrics.DurationMS),
		ThroughputTokensPerSecond: cloneFloat64Ptr(metrics.ThroughputTokensPerSecond),
		UpdatedAt:                 metrics.UpdatedAt,
	}
}

func apiTrackerRateLimit(rateLimit *domain.TrackerRateLimit) *trackerRateLimitJSON {
	if rateLimit == nil {
		return nil
	}
	return &trackerRateLimitJSON{
		Limit:             cloneInt64Ptr(rateLimit.Limit),
		Remaining:         cloneInt64Ptr(rateLimit.Remaining),
		ResetAt:           rateLimit.ResetAt,
		RetryAfterSeconds: cloneInt64Ptr(rateLimit.RetryAfterSeconds),
		UpdatedAt:         rateLimit.UpdatedAt,
	}
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
