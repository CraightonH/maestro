package domain

import "time"

type RunMetrics struct {
	TokensIn                  *int64    `json:"tokens_in,omitempty"`
	TokensOut                 *int64    `json:"tokens_out,omitempty"`
	TotalTokens               *int64    `json:"total_tokens,omitempty"`
	CostUSD                   *float64  `json:"cost_usd,omitempty"`
	DurationMS                *int64    `json:"duration_ms,omitempty"`
	ThroughputTokensPerSecond *float64  `json:"throughput_tokens_per_second,omitempty"`
	UpdatedAt                 time.Time `json:"updated_at,omitempty"`
}

type TrackerRateLimit struct {
	Limit             *int64    `json:"limit,omitempty"`
	Remaining         *int64    `json:"remaining,omitempty"`
	ResetAt           time.Time `json:"reset_at,omitempty"`
	RetryAfterSeconds *int64    `json:"retry_after_seconds,omitempty"`
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
}

func MergeRunMetrics(base RunMetrics, update RunMetrics) RunMetrics {
	if update.TokensIn != nil {
		base.TokensIn = cloneInt64Ptr(update.TokensIn)
	}
	if update.TokensOut != nil {
		base.TokensOut = cloneInt64Ptr(update.TokensOut)
	}
	if update.TotalTokens != nil {
		base.TotalTokens = cloneInt64Ptr(update.TotalTokens)
	}
	if update.CostUSD != nil {
		base.CostUSD = cloneFloat64Ptr(update.CostUSD)
	}
	if update.DurationMS != nil {
		base.DurationMS = cloneInt64Ptr(update.DurationMS)
	}
	if update.ThroughputTokensPerSecond != nil {
		base.ThroughputTokensPerSecond = cloneFloat64Ptr(update.ThroughputTokensPerSecond)
	}
	if !update.UpdatedAt.IsZero() {
		base.UpdatedAt = update.UpdatedAt
	}
	return base
}

func DeriveRunMetrics(metrics RunMetrics, startedAt time.Time, completedAt time.Time, now time.Time) RunMetrics {
	if metrics.TotalTokens == nil && metrics.TokensIn != nil && metrics.TokensOut != nil {
		total := *metrics.TokensIn + *metrics.TokensOut
		metrics.TotalTokens = &total
	}
	if metrics.DurationMS == nil {
		end := completedAt
		if end.IsZero() {
			end = now
		}
		if !startedAt.IsZero() && !end.IsZero() && !end.Before(startedAt) {
			duration := end.Sub(startedAt).Milliseconds()
			metrics.DurationMS = &duration
		}
	}
	if metrics.ThroughputTokensPerSecond == nil && metrics.TotalTokens != nil && metrics.DurationMS != nil && *metrics.DurationMS > 0 {
		throughput := float64(*metrics.TotalTokens) / (float64(*metrics.DurationMS) / 1000.0)
		metrics.ThroughputTokensPerSecond = &throughput
	}
	return metrics
}

func CloneTrackerRateLimit(rateLimit *TrackerRateLimit) *TrackerRateLimit {
	if rateLimit == nil {
		return nil
	}
	cloned := *rateLimit
	cloned.Limit = cloneInt64Ptr(rateLimit.Limit)
	cloned.Remaining = cloneInt64Ptr(rateLimit.Remaining)
	cloned.RetryAfterSeconds = cloneInt64Ptr(rateLimit.RetryAfterSeconds)
	return &cloned
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
