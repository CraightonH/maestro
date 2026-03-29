package orchestrator

import (
	"fmt"
	"time"
)

var forcePollDebounce = 2 * time.Second

type ForcePollStatus string

const (
	ForcePollQueued        ForcePollStatus = "queued"
	ForcePollDebounced     ForcePollStatus = "debounced"
	ForcePollAlreadyQueued ForcePollStatus = "already_queued"
)

type ForcePollSourceResult struct {
	Source string          `json:"source"`
	Status ForcePollStatus `json:"status"`
}

type ForcePollResult struct {
	Scope   string                  `json:"scope"`
	Results []ForcePollSourceResult `json:"results"`
}

func (s *Service) RequestForcePoll(sourceName string) (ForcePollResult, error) {
	if sourceName != "" && sourceName != s.source.Name {
		return ForcePollResult{}, fmt.Errorf("source %q: %w", sourceName, ErrSourceNotFound)
	}

	status := s.queueForcePoll()
	if status == ForcePollQueued {
		s.recordSourceEvent("info", s.source.Name, "force poll queued")
	}
	return ForcePollResult{
		Scope: "source",
		Results: []ForcePollSourceResult{{
			Source: s.source.Name,
			Status: status,
		}},
	}, nil
}

func (s *Service) queueForcePoll() ForcePollStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.polling || s.forcePollPending {
		return ForcePollAlreadyQueued
	}
	if !s.lastPollAttemptAt.IsZero() && time.Since(s.lastPollAttemptAt) < forcePollDebounce {
		return ForcePollDebounced
	}

	select {
	case s.forcePollCh <- struct{}{}:
		s.forcePollPending = true
		return ForcePollQueued
	default:
		s.forcePollPending = true
		return ForcePollAlreadyQueued
	}
}
