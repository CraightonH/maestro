package orchestrator

import (
	"sort"
	"strings"
)

func mergeSnapshots(snapshots []Snapshot) Snapshot {
	if len(snapshots) == 0 {
		return Snapshot{}
	}

	sourceNames := make([]string, 0, len(snapshots))
	merged := Snapshot{}
	for _, snap := range snapshots {
		if snap.SourceName != "" {
			sourceNames = append(sourceNames, snap.SourceName)
		}
		if snap.LastPollAt.After(merged.LastPollAt) {
			merged.LastPollAt = snap.LastPollAt
		}
		merged.LastPollCount += snap.LastPollCount
		merged.ClaimedCount += snap.ClaimedCount
		merged.RetryCount += snap.RetryCount
		merged.InstanceMetrics = sumAggregateRunMetrics(merged.InstanceMetrics, snap.InstanceMetrics)
		merged.HarnessMetrics = mergeMetricBreakdowns(merged.HarnessMetrics, snap.HarnessMetrics)
		merged.PendingApprovals = append(merged.PendingApprovals, snap.PendingApprovals...)
		merged.PendingMessages = append(merged.PendingMessages, snap.PendingMessages...)
		merged.ApprovalHistory = append(merged.ApprovalHistory, snap.ApprovalHistory...)
		merged.MessageHistory = append(merged.MessageHistory, snap.MessageHistory...)
		merged.RecentEvents = append(merged.RecentEvents, filterRecentEvents(snap.RecentEvents)...)
		merged.ActiveRuns = append(merged.ActiveRuns, snap.ActiveRuns...)
		merged.RunOutputs = append(merged.RunOutputs, snap.RunOutputs...)
		merged.SourceSummaries = append(merged.SourceSummaries, snap.SourceSummaries...)
	}

	sort.Strings(sourceNames)
	merged.SourceName = strings.Join(sourceNames, ", ")

	sort.Slice(merged.PendingApprovals, func(i, j int) bool {
		return merged.PendingApprovals[i].RequestedAt.Before(merged.PendingApprovals[j].RequestedAt)
	})
	sort.Slice(merged.ApprovalHistory, func(i, j int) bool {
		return merged.ApprovalHistory[i].DecidedAt.After(merged.ApprovalHistory[j].DecidedAt)
	})
	if len(merged.ApprovalHistory) > maxApprovalHistory {
		merged.ApprovalHistory = merged.ApprovalHistory[:maxApprovalHistory]
	}
	sort.Slice(merged.PendingMessages, func(i, j int) bool {
		return merged.PendingMessages[i].RequestedAt.Before(merged.PendingMessages[j].RequestedAt)
	})
	sort.Slice(merged.MessageHistory, func(i, j int) bool {
		return merged.MessageHistory[i].RepliedAt.After(merged.MessageHistory[j].RepliedAt)
	})
	if len(merged.MessageHistory) > maxMessageHistory {
		merged.MessageHistory = merged.MessageHistory[:maxMessageHistory]
	}
	sort.Slice(merged.RecentEvents, func(i, j int) bool {
		return merged.RecentEvents[i].Time.After(merged.RecentEvents[j].Time)
	})
	if len(merged.RecentEvents) > maxRecentEvents {
		merged.RecentEvents = merged.RecentEvents[:maxRecentEvents]
	}
	sort.Slice(merged.ActiveRuns, func(i, j int) bool {
		return merged.ActiveRuns[i].StartedAt.Before(merged.ActiveRuns[j].StartedAt)
	})
	if len(merged.ActiveRuns) > 0 {
		first := merged.ActiveRuns[0]
		merged.ActiveRun = &first
	} else {
		merged.ActiveRun = nil
	}
	sort.Slice(merged.RunOutputs, func(i, j int) bool {
		if merged.RunOutputs[i].UpdatedAt.Equal(merged.RunOutputs[j].UpdatedAt) {
			return merged.RunOutputs[i].RunID < merged.RunOutputs[j].RunID
		}
		return merged.RunOutputs[i].UpdatedAt.After(merged.RunOutputs[j].UpdatedAt)
	})
	sort.Slice(merged.SourceSummaries, func(i, j int) bool {
		return merged.SourceSummaries[i].Name < merged.SourceSummaries[j].Name
	})
	sort.Slice(merged.HarnessMetrics, func(i, j int) bool {
		return merged.HarnessMetrics[i].Name < merged.HarnessMetrics[j].Name
	})
	return merged
}

func filterRecentEvents(events []Event) []Event {
	if len(events) == 0 {
		return nil
	}
	filtered := make([]Event, 0, len(events))
	for _, event := range events {
		if suppressRecentEvent(event) {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func suppressRecentEvent(event Event) bool {
	message := strings.TrimSpace(event.Message)
	return strings.HasPrefix(message, "polled 0 candidate issues from ")
}

func mergeMetricBreakdowns(base []MetricBreakdown, updates []MetricBreakdown) []MetricBreakdown {
	if len(updates) == 0 {
		return base
	}
	index := make(map[string]int, len(base))
	for i, item := range base {
		index[item.Name] = i
	}
	for _, item := range updates {
		if pos, ok := index[item.Name]; ok {
			base[pos].Metrics = sumAggregateRunMetrics(base[pos].Metrics, item.Metrics)
			continue
		}
		index[item.Name] = len(base)
		base = append(base, item)
	}
	return base
}
