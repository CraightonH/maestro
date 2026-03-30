package orchestrator

import (
	"errors"
	"fmt"

	"github.com/tjohnson/maestro/internal/domain"
)

func (s *Service) StopRun(runID string, reason string) error {
	s.mu.Lock()
	if s.activeRunByIDLocked(runID) == nil {
		s.mu.Unlock()
		return fmt.Errorf("run %q: %w", runID, ErrRunNotFound)
	}
	if _, exists := s.pendingStops[runID]; exists {
		s.mu.Unlock()
		return nil
	}
	s.pendingStops[runID] = pendingStop{
		Status: domain.RunStatusFailed,
		Reason: reason,
		Retry:  false,
	}
	s.mu.Unlock()

	s.recordRunEventByFields("warn", s.source.Name, runID, "", "stopping run %s: %s", runID, reason)
	stopCtx, cancel := withHarnessControlTimeout()
	defer cancel()

	if err := s.harness.Stop(stopCtx, runID); err != nil {
		if errors.Is(err, ErrRunNotFound) {
			return err
		}
		s.recordRunEventByFields("error", s.source.Name, runID, "", "stop run %s failed: %v", runID, err)
		return err
	}
	return nil
}
