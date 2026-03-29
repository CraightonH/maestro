package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/tjohnson/maestro/internal/config"
)

type Runtime interface {
	Run(ctx context.Context) error
	Snapshot() Snapshot
	ResolveApproval(requestID string, decision string) error
	ResolveMessage(requestID string, reply string, resolvedVia string) error
	StopRun(runID string, reason string) error
	RequestForcePoll(sourceName string) (ForcePollResult, error)
}

type Supervisor struct {
	services []*Service
}

func NewRuntime(cfg *config.Config, logger *slog.Logger) (Runtime, error) {
	if len(cfg.Sources) == 1 {
		return NewService(cfg, logger)
	}
	return NewSupervisor(cfg, logger)
}

func NewSupervisor(cfg *config.Config, logger *slog.Logger) (*Supervisor, error) {
	shared := newRuntimeSharedDeps(cfg)
	services := make([]*Service, 0, len(cfg.Sources))
	for _, source := range cfg.Sources {
		svc, err := buildScopedService(cfg, source, logger, shared)
		if err != nil {
			return nil, err
		}
		services = append(services, svc)
	}
	return &Supervisor{services: services}, nil
}

func scopedConfig(cfg *config.Config, source config.SourceConfig, agent config.AgentTypeConfig) *config.Config {
	clone := *cfg
	clone.Sources = []config.SourceConfig{source}
	clone.AgentTypes = []config.AgentTypeConfig{agent}
	clone.State = cfg.State
	clone.State.Dir = config.ScopedStateDir(cfg, source)
	return &clone
}

func (s *Supervisor) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, len(s.services))
	var wg sync.WaitGroup
	for _, svc := range s.services {
		wg.Add(1)
		go func(svc *Service) {
			defer wg.Done()
			errCh <- svc.Run(runCtx)
		}(svc)
	}

	var firstErr error
	for i := 0; i < len(s.services); i++ {
		err := <-errCh
		if err != nil && firstErr == nil {
			firstErr = err
			cancel()
		}
	}
	wg.Wait()
	return firstErr
}

func (s *Supervisor) Snapshot() Snapshot {
	if len(s.services) == 0 {
		return Snapshot{}
	}

	snapshots := make([]Snapshot, 0, len(s.services))
	for _, svc := range s.services {
		snapshots = append(snapshots, svc.Snapshot())
	}
	return mergeSnapshots(snapshots)
}

func (s *Supervisor) ResolveApproval(requestID string, decision string) error {
	var errs []string
	for _, svc := range s.services {
		if err := svc.ResolveApproval(requestID, decision); err == nil {
			return nil
		} else if !errors.Is(err, ErrApprovalNotFound) {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return fmt.Errorf("approval request %q: %w", requestID, ErrApprovalNotFound)
}

func (s *Supervisor) ResolveMessage(requestID string, reply string, resolvedVia string) error {
	var errs []string
	for _, svc := range s.services {
		if err := svc.ResolveMessage(requestID, reply, resolvedVia); err == nil {
			return nil
		} else if !errors.Is(err, ErrMessageNotFound) {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return fmt.Errorf("message request %q: %w", requestID, ErrMessageNotFound)
}

func (s *Supervisor) StopRun(runID string, reason string) error {
	var errs []string
	for _, svc := range s.services {
		if err := svc.StopRun(runID, reason); err == nil {
			return nil
		} else if !errors.Is(err, ErrRunNotFound) {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return fmt.Errorf("run %q: %w", runID, ErrRunNotFound)
}

func (s *Supervisor) RequestForcePoll(sourceName string) (ForcePollResult, error) {
	if sourceName != "" {
		for _, svc := range s.services {
			if svc.source.Name != sourceName {
				continue
			}
			return svc.RequestForcePoll(sourceName)
		}
		return ForcePollResult{}, fmt.Errorf("source %q: %w", sourceName, ErrSourceNotFound)
	}

	result := ForcePollResult{
		Scope:   "all",
		Results: make([]ForcePollSourceResult, 0, len(s.services)),
	}
	for _, svc := range s.services {
		serviceResult, err := svc.RequestForcePoll("")
		if err != nil {
			return ForcePollResult{}, err
		}
		result.Results = append(result.Results, serviceResult.Results...)
	}
	return result, nil
}

func (s *Supervisor) Services() []*Service {
	return slices.Clone(s.services)
}
