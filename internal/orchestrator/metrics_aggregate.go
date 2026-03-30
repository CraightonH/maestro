package orchestrator

import (
	"sync"

	"github.com/tjohnson/maestro/internal/domain"
)

type runtimeMetricsStore struct {
	mu        sync.RWMutex
	bySource  map[string]domain.RunMetrics
	byHarness map[string]domain.RunMetrics
}

func newRuntimeMetricsStore() *runtimeMetricsStore {
	return &runtimeMetricsStore{
		bySource:  map[string]domain.RunMetrics{},
		byHarness: map[string]domain.RunMetrics{},
	}
}

func (s *runtimeMetricsStore) addCompleted(sourceName string, harnessKind string, metrics domain.RunMetrics) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sourceName != "" {
		s.bySource[sourceName] = sumAggregateRunMetrics(s.bySource[sourceName], metrics)
	}
	if harnessKind != "" {
		s.byHarness[harnessKind] = sumAggregateRunMetrics(s.byHarness[harnessKind], metrics)
	}
}

func (s *runtimeMetricsStore) snapshotForSource(sourceName string) domain.RunMetrics {
	if s == nil || sourceName == "" {
		return domain.RunMetrics{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneAggregateRunMetrics(s.bySource[sourceName])
}

func (s *runtimeMetricsStore) snapshotForHarness(harnessKind string) domain.RunMetrics {
	if s == nil || harnessKind == "" {
		return domain.RunMetrics{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneAggregateRunMetrics(s.byHarness[harnessKind])
}

func sumAggregateRunMetrics(base domain.RunMetrics, update domain.RunMetrics) domain.RunMetrics {
	if update.TokensIn != nil {
		base.TokensIn = addInt64Ptr(base.TokensIn, *update.TokensIn)
	}
	if update.TokensOut != nil {
		base.TokensOut = addInt64Ptr(base.TokensOut, *update.TokensOut)
	}
	if update.TotalTokens != nil {
		base.TotalTokens = addInt64Ptr(base.TotalTokens, *update.TotalTokens)
	}
	if update.CostUSD != nil {
		base.CostUSD = addFloat64Ptr(base.CostUSD, *update.CostUSD)
	}
	if base.TokensIn != nil && base.TokensOut != nil {
		total := *base.TokensIn + *base.TokensOut
		base.TotalTokens = &total
	}
	if update.UpdatedAt.After(base.UpdatedAt) {
		base.UpdatedAt = update.UpdatedAt
	}
	base.DurationMS = nil
	base.ThroughputTokensPerSecond = nil
	return base
}

func cloneAggregateRunMetrics(metrics domain.RunMetrics) domain.RunMetrics {
	cloned := domain.RunMetrics{
		UpdatedAt: metrics.UpdatedAt,
	}
	if metrics.TokensIn != nil {
		value := *metrics.TokensIn
		cloned.TokensIn = &value
	}
	if metrics.TokensOut != nil {
		value := *metrics.TokensOut
		cloned.TokensOut = &value
	}
	if metrics.TotalTokens != nil {
		value := *metrics.TotalTokens
		cloned.TotalTokens = &value
	}
	if metrics.CostUSD != nil {
		value := *metrics.CostUSD
		cloned.CostUSD = &value
	}
	return cloned
}

func addInt64Ptr(base *int64, delta int64) *int64 {
	if base == nil {
		value := delta
		return &value
	}
	value := *base + delta
	return &value
}

func addFloat64Ptr(base *float64, delta float64) *float64 {
	if base == nil {
		value := delta
		return &value
	}
	value := *base + delta
	return &value
}

func aggregateMetricsForSource(completed domain.RunMetrics, activeRuns []domain.AgentRun) domain.RunMetrics {
	aggregate := cloneAggregateRunMetrics(completed)
	for _, run := range activeRuns {
		aggregate = sumAggregateRunMetrics(aggregate, run.Metrics)
	}
	aggregate.DurationMS = nil
	aggregate.ThroughputTokensPerSecond = nil
	return aggregate
}
