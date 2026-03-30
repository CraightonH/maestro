package orchestrator

import (
	"fmt"
	"time"
)

var forcePollDebounce = 2 * time.Second
var forcePollCompletionTimeout = 5 * time.Second
var forcePollWaitInterval = 25 * time.Millisecond

type ForcePollStatus string

const (
	ForcePollCompleted     ForcePollStatus = "completed"
	ForcePollQueued        ForcePollStatus = "queued"
	ForcePollDebounced     ForcePollStatus = "debounced"
	ForcePollAlreadyQueued ForcePollStatus = "already_queued"
	ForcePollTimedOut      ForcePollStatus = "timed_out"
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

	requestedAt := time.Now()
	status := s.queueForcePoll()
	if status == ForcePollQueued {
		s.recordSourceEvent("info", s.source.Name, "force poll requested")
		status = s.waitForForcePollCompletion(requestedAt)
		if status == ForcePollCompleted {
			s.recordSourceEvent("info", s.source.Name, "force poll completed")
		} else if status == ForcePollTimedOut {
			s.recordSourceEvent("warn", s.source.Name, "force poll did not complete within %s", forcePollCompletionTimeout)
		}
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

func (s *Service) waitForForcePollCompletion(requestedAt time.Time) ForcePollStatus {
	deadline := time.Now().Add(forcePollCompletionTimeout)
	for {
		s.mu.RLock()
		lastPollAt := s.lastPollAt
		polling := s.polling
		s.mu.RUnlock()
		if !lastPollAt.IsZero() && !lastPollAt.Before(requestedAt) && !polling {
			return ForcePollCompleted
		}
		if time.Now().After(deadline) {
			return ForcePollTimedOut
		}
		time.Sleep(forcePollWaitInterval)
	}
}
