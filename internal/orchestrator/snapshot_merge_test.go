package orchestrator

import (
	"testing"
	"time"

	"github.com/tjohnson/maestro/internal/domain"
)

func TestMergeSnapshotsSuppressesZeroCandidatePollEvents(t *testing.T) {
	now := time.Date(2026, 3, 29, 22, 0, 0, 0, time.UTC)
	snapshot := mergeSnapshots([]Snapshot{{
		RecentEvents: []Event{
			{Time: now, Level: "INFO", Source: "review", Message: "polled 0 candidate issues from review"},
			{Time: now.Add(-time.Second), Level: "INFO", Source: "review", Message: "dispatching TAN-188 to review-claude"},
			{Time: now.Add(-2 * time.Second), Level: "INFO", Source: "review", Message: "polled 2 candidate issues from review"},
		},
	}})

	if len(snapshot.RecentEvents) != 2 {
		t.Fatalf("recent events = %+v, want 2 filtered items", snapshot.RecentEvents)
	}
	for _, event := range snapshot.RecentEvents {
		if suppressRecentEvent(event) {
			t.Fatalf("recent events still contain suppressed poll noise: %+v", snapshot.RecentEvents)
		}
	}
}

func TestMergeSnapshotsAggregatesInstanceMetrics(t *testing.T) {
	snapshot := mergeSnapshots([]Snapshot{
		{
			InstanceMetrics: domain.RunMetrics{
				TokensIn:  int64Ptr(120),
				TokensOut: int64Ptr(30),
			},
		},
		{
			InstanceMetrics: domain.RunMetrics{
				TokensIn:    int64Ptr(80),
				TokensOut:   int64Ptr(20),
				TotalTokens: int64Ptr(100),
			},
		},
	})

	if snapshot.InstanceMetrics.TokensIn == nil || *snapshot.InstanceMetrics.TokensIn != 200 {
		t.Fatalf("tokens_in = %#v, want 200", snapshot.InstanceMetrics.TokensIn)
	}
	if snapshot.InstanceMetrics.TokensOut == nil || *snapshot.InstanceMetrics.TokensOut != 50 {
		t.Fatalf("tokens_out = %#v, want 50", snapshot.InstanceMetrics.TokensOut)
	}
	if snapshot.InstanceMetrics.TotalTokens == nil || *snapshot.InstanceMetrics.TotalTokens != 250 {
		t.Fatalf("total_tokens = %#v, want 250", snapshot.InstanceMetrics.TotalTokens)
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}
