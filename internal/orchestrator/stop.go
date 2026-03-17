package orchestrator

import (
	"context"
	"fmt"

	"github.com/tjohnson/maestro/internal/domain"
)

func (s *Service) StopRun(runID string, reason string) error {
	s.mu.Lock()
	if s.activeRun == nil || s.activeRun.ID != runID {
		s.mu.Unlock()
		return fmt.Errorf("run %q not found", runID)
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
	if err := s.harness.Stop(context.Background(), runID); err != nil {
		s.recordRunEventByFields("error", s.source.Name, runID, "", "stop run %s failed: %v", runID, err)
		return err
	}
	return nil
}
