package orchestrator

import "fmt"

func (s *Service) SourceName() string {
	return s.source.Name
}

func (s *Service) Drain(reason string) {
	s.mu.Lock()
	if s.draining {
		s.mu.Unlock()
		return
	}
	s.draining = true
	s.mu.Unlock()

	if reason != "" {
		s.recordSourceEvent("info", s.source.Name, "service draining: %s", reason)
	} else {
		s.recordSourceEvent("info", s.source.Name, "service draining")
	}
	s.signalControl()
}

func (s *Service) IsDraining() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.draining
}

func (s *Service) HasActiveRun() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeRun != nil
}

func (s *Service) shouldExitAfterDrain() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.draining && s.activeRun == nil
}

func (s *Service) signalControl() {
	select {
	case s.controlCh <- struct{}{}:
	default:
	}
}

func (s *Service) drainStatus() string {
	if !s.IsDraining() {
		return "active"
	}
	if s.HasActiveRun() {
		return "draining"
	}
	return "drained"
}

func formatDrainReason(sourceName string, currentSignature string, desiredSignature string) string {
	switch {
	case desiredSignature == "":
		return fmt.Sprintf("source %s removed from config", sourceName)
	case currentSignature == desiredSignature:
		return fmt.Sprintf("source %s refreshed", sourceName)
	default:
		return fmt.Sprintf("source %s reloading after config change", sourceName)
	}
}
