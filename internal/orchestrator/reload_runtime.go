package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/ops"
)

const hotReloadPollInterval = 2 * time.Second

type reloadRuntimeSlot struct {
	spec     reloadServiceSpec
	service  *Service
	draining bool
}

type reloadServiceExit struct {
	sourceName string
	service    *Service
	err        error
}

type ReloadableRuntime struct {
	logger       *slog.Logger
	buildService func(*config.Config, config.SourceConfig, *slog.Logger, *runtimeSharedDeps) (*Service, error)
	shared       *runtimeSharedDeps

	mu           sync.RWMutex
	currentCfg   *config.Config
	desired      map[string]reloadServiceSpec
	slots        map[string]*reloadRuntimeSlot
	events       []Event
	rootCtx      context.Context
	rootCancel   context.CancelFunc
	running      bool
	shuttingDown bool
	watchPaths   []string
	watchDigest  string
	exitCh       chan reloadServiceExit
}

func NewReloadableRuntime(cfg *config.Config, logger *slog.Logger) (*ReloadableRuntime, error) {
	shared := newRuntimeSharedDeps(cfg)
	desired, err := buildReloadServiceSpecs(cfg)
	if err != nil {
		return nil, err
	}

	runtime := &ReloadableRuntime{
		logger:       logger,
		buildService: buildScopedService,
		shared:       shared,
		currentCfg:   cfg,
		desired:      desired,
		slots:        map[string]*reloadRuntimeSlot{},
		exitCh:       make(chan reloadServiceExit, len(cfg.Sources)+4),
		watchPaths:   reloadWatchPaths(cfg),
	}
	runtime.watchDigest, err = fingerprintPaths(runtime.watchPaths)
	if err != nil {
		return nil, err
	}

	for _, source := range cfg.Sources {
		spec := desired[source.Name]
		service, err := runtime.buildService(cfg, source, logger, shared)
		if err != nil {
			return nil, err
		}
		runtime.slots[source.Name] = &reloadRuntimeSlot{
			spec:    spec,
			service: service,
		}
	}
	return runtime, nil
}

func (r *ReloadableRuntime) Run(ctx context.Context) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return fmt.Errorf("runtime already running")
	}
	r.rootCtx, r.rootCancel = context.WithCancel(ctx)
	r.running = true
	r.shuttingDown = false
	rootCtx := r.rootCtx
	for name, slot := range r.slots {
		r.startSlotLocked(rootCtx, name, slot)
	}
	r.mu.Unlock()

	ticker := time.NewTicker(hotReloadPollInterval)
	defer ticker.Stop()

	var firstErr error
	shutdownRequested := false
	for {
		select {
		case <-ctx.Done():
			shutdownRequested = true
			r.beginShutdown()
		case exit := <-r.exitCh:
			if err := r.handleServiceExit(exit); err != nil && firstErr == nil {
				firstErr = err
				shutdownRequested = true
				r.beginShutdown()
			}
		case <-ticker.C:
			if shutdownRequested {
				continue
			}
			if err := r.checkForReload(); err != nil {
				r.recordReloadEvent("warn", "config reload check failed: %v", err)
			}
		}

		if shutdownRequested && r.slotCount() == 0 {
			return firstErr
		}
	}
}

func (r *ReloadableRuntime) Snapshot() Snapshot {
	r.mu.RLock()
	snapshots := make([]Snapshot, 0, len(r.slots)+1)
	for _, slot := range r.slots {
		snapshots = append(snapshots, slot.service.Snapshot())
	}
	if len(r.events) > 0 {
		snapshots = append(snapshots, Snapshot{RecentEvents: append([]Event(nil), r.events...)})
	}
	r.mu.RUnlock()
	return mergeSnapshots(snapshots)
}

func (r *ReloadableRuntime) ResolveApproval(requestID string, decision string) error {
	r.mu.RLock()
	services := r.servicesLocked()
	r.mu.RUnlock()
	return (&Supervisor{services: services}).ResolveApproval(requestID, decision)
}

func (r *ReloadableRuntime) ResolveMessage(requestID string, reply string, resolvedVia string) error {
	r.mu.RLock()
	services := r.servicesLocked()
	r.mu.RUnlock()
	return (&Supervisor{services: services}).ResolveMessage(requestID, reply, resolvedVia)
}

func (r *ReloadableRuntime) StopRun(runID string, reason string) error {
	r.mu.RLock()
	services := r.servicesLocked()
	r.mu.RUnlock()
	return (&Supervisor{services: services}).StopRun(runID, reason)
}

func (r *ReloadableRuntime) RequestForcePoll(sourceName string) (ForcePollResult, error) {
	r.mu.RLock()
	services := r.servicesLocked()
	r.mu.RUnlock()
	return (&Supervisor{services: services}).RequestForcePoll(sourceName)
}

func (r *ReloadableRuntime) ConfigSummary() ops.ConfigSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return ops.SummarizeConfig(r.currentCfg)
}

func (r *ReloadableRuntime) servicesLocked() []*Service {
	services := make([]*Service, 0, len(r.slots))
	for _, slot := range r.slots {
		services = append(services, slot.service)
	}
	return services
}

func (r *ReloadableRuntime) startSlotLocked(ctx context.Context, sourceName string, slot *reloadRuntimeSlot) {
	service := slot.service
	go func() {
		r.exitCh <- reloadServiceExit{
			sourceName: sourceName,
			service:    service,
			err:        service.Run(ctx),
		}
	}()
}

func (r *ReloadableRuntime) beginShutdown() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.shuttingDown {
		return
	}
	r.shuttingDown = true
	if r.rootCancel != nil {
		r.rootCancel()
	}
}

func (r *ReloadableRuntime) slotCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.slots)
}

func (r *ReloadableRuntime) handleServiceExit(exit reloadServiceExit) error {
	r.mu.Lock()
	slot, ok := r.slots[exit.sourceName]
	if !ok || slot.service != exit.service {
		r.mu.Unlock()
		return nil
	}
	delete(r.slots, exit.sourceName)
	shuttingDown := r.shuttingDown
	cfg := r.currentCfg
	spec, shouldRestart := r.desired[exit.sourceName]
	rootCtx := r.rootCtx
	r.mu.Unlock()

	if exit.err != nil {
		r.recordReloadEvent("error", "source %s stopped with error: %v", exit.sourceName, exit.err)
		if shuttingDown {
			return nil
		}
		return exit.err
	}
	if shuttingDown {
		return nil
	}
	if !shouldRestart {
		return nil
	}

	service, err := r.buildService(cfg, spec.Source, r.logger, r.shared)
	if err != nil {
		r.recordReloadEvent("error", "reload for source %s failed to start replacement: %v", exit.sourceName, err)
		return nil
	}

	r.mu.Lock()
	if r.shuttingDown {
		r.mu.Unlock()
		return nil
	}
	if _, exists := r.slots[exit.sourceName]; exists {
		r.mu.Unlock()
		return nil
	}
	replacement := &reloadRuntimeSlot{
		spec:    spec,
		service: service,
	}
	r.slots[exit.sourceName] = replacement
	if r.running && !r.shuttingDown {
		r.startSlotLocked(rootCtx, exit.sourceName, replacement)
	}
	r.mu.Unlock()

	r.recordReloadEvent("info", "source %s restarted with updated config", exit.sourceName)
	return nil
}

func (r *ReloadableRuntime) checkForReload() error {
	r.mu.RLock()
	paths := append([]string(nil), r.watchPaths...)
	lastDigest := r.watchDigest
	r.mu.RUnlock()

	digest, err := fingerprintPaths(paths)
	if err != nil {
		return err
	}
	if digest == lastDigest {
		return nil
	}

	r.mu.Lock()
	r.watchDigest = digest
	r.mu.Unlock()
	return r.reloadFromDisk()
}

func (r *ReloadableRuntime) reloadFromDisk() error {
	r.mu.RLock()
	cfgPath := r.currentCfg.ConfigPath
	currentSpecs := make(map[string]reloadServiceSpec, len(r.desired))
	for name, spec := range r.desired {
		currentSpecs[name] = spec
	}
	r.mu.RUnlock()

	nextCfg, err := config.Load(cfgPath)
	if err != nil {
		r.recordReloadEvent("warn", "config reload rejected: %v", err)
		return nil
	}
	if err := config.ValidateMVP(nextCfg); err != nil {
		r.recordReloadEvent("warn", "config reload rejected: %v", err)
		return nil
	}
	warnings := config.DiagnoseConfig(nextCfg)
	for _, warning := range warnings {
		r.recordReloadEvent("warn", "config reload warning: %s", warning)
	}

	plan, err := planReloadWithCurrentSpecs(currentSpecs, nextCfg)
	if err != nil {
		r.recordReloadEvent("warn", "config reload rejected: %v", err)
		return nil
	}

	starts := map[string]*Service{}
	for _, transition := range plan.Transitions {
		if transition.Action != reloadActionStart {
			continue
		}
		service, err := r.buildService(nextCfg, transition.Desired.Source, r.logger, r.shared)
		if err != nil {
			r.recordReloadEvent("warn", "config reload rejected: %v", err)
			return nil
		}
		starts[transition.SourceName] = service
	}

	r.shared.applyConfig(nextCfg)

	nextWatchPaths := reloadWatchPaths(nextCfg)
	nextWatchDigest, err := fingerprintPaths(nextWatchPaths)
	if err != nil {
		r.recordReloadEvent("warn", "config reload rejected: %v", err)
		return nil
	}

	r.mu.Lock()
	r.currentCfg = nextCfg
	r.desired = plan.Desired
	r.watchPaths = nextWatchPaths
	r.watchDigest = nextWatchDigest
	rootCtx := r.rootCtx
	r.mu.Unlock()

	restarted := 0
	started := 0
	stopped := 0
	for _, transition := range plan.Transitions {
		switch transition.Action {
		case reloadActionKeep:
			continue
		case reloadActionStart:
			if err := r.installStartedSource(rootCtx, transition.SourceName, plan.Desired[transition.SourceName], starts[transition.SourceName]); err == nil {
				started++
			}
		case reloadActionStop:
			if r.drainSource(transition.SourceName, transition.Current.Signature, "") {
				stopped++
			}
		case reloadActionRestart:
			if r.drainSource(transition.SourceName, transition.Current.Signature, transition.Desired.Signature) {
				restarted++
			}
		}
	}

	r.recordReloadEvent("info", "config reloaded: %d started, %d draining restarts, %d draining removals", started, restarted, stopped)
	return nil
}

func (r *ReloadableRuntime) installStartedSource(ctx context.Context, sourceName string, spec reloadServiceSpec, service *Service) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.slots[sourceName]; ok {
		existing.draining = true
		existing.service.Drain(formatDrainReason(sourceName, existing.spec.Signature, spec.Signature))
		return nil
	}
	slot := &reloadRuntimeSlot{
		spec:    spec,
		service: service,
	}
	r.slots[sourceName] = slot
	if r.running && !r.shuttingDown {
		r.startSlotLocked(ctx, sourceName, slot)
	}
	return nil
}

func (r *ReloadableRuntime) drainSource(sourceName string, currentSignature string, desiredSignature string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	slot, ok := r.slots[sourceName]
	if !ok {
		return false
	}
	if slot.draining {
		return true
	}
	slot.draining = true
	slot.service.Drain(formatDrainReason(sourceName, currentSignature, desiredSignature))
	return true
}

func (r *ReloadableRuntime) recordReloadEvent(level string, format string, args ...any) {
	r.logger.Log(context.Background(), parseLevel(level), fmt.Sprintf(format, args...))

	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, Event{
		Time:    time.Now(),
		Level:   strings.ToUpper(level),
		Message: fmt.Sprintf(format, args...),
	})
	if len(r.events) > maxRecentEvents {
		r.events = r.events[len(r.events)-maxRecentEvents:]
	}
}
