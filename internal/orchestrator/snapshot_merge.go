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
		merged.PendingApprovals = append(merged.PendingApprovals, snap.PendingApprovals...)
		merged.PendingMessages = append(merged.PendingMessages, snap.PendingMessages...)
		merged.ApprovalHistory = append(merged.ApprovalHistory, snap.ApprovalHistory...)
		merged.MessageHistory = append(merged.MessageHistory, snap.MessageHistory...)
		merged.RecentEvents = append(merged.RecentEvents, snap.RecentEvents...)
		merged.ActiveRuns = append(merged.ActiveRuns, snap.ActiveRuns...)
		merged.RunOutputs = append(merged.RunOutputs, snap.RunOutputs...)
		merged.SourceSummaries = append(merged.SourceSummaries, snap.SourceSummaries...)
		if merged.ActiveRun == nil && snap.ActiveRun != nil {
			merged.ActiveRun = snap.ActiveRun
		}
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
	sort.Slice(merged.RunOutputs, func(i, j int) bool {
		if merged.RunOutputs[i].UpdatedAt.Equal(merged.RunOutputs[j].UpdatedAt) {
			return merged.RunOutputs[i].RunID < merged.RunOutputs[j].RunID
		}
		return merged.RunOutputs[i].UpdatedAt.After(merged.RunOutputs[j].UpdatedAt)
	})
	sort.Slice(merged.SourceSummaries, func(i, j int) bool {
		return merged.SourceSummaries[i].Name < merged.SourceSummaries[j].Name
	})
	return merged
}
