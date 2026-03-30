package orchestrator

import (
	"fmt"
	"log/slog"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/harness"
	"github.com/tjohnson/maestro/internal/state"
	"github.com/tjohnson/maestro/internal/workspace"
)

type runtimeSharedDeps struct {
	globalLimiter *semaphoreLimiter
	agentLimiters map[string]*semaphoreLimiter
	dockerReuse   *harness.DockerReuseManager
}

func newRuntimeSharedDeps(cfg *config.Config) *runtimeSharedDeps {
	deps := &runtimeSharedDeps{
		globalLimiter: newSemaphoreLimiter(cfg.Defaults.MaxConcurrentGlobal),
		agentLimiters: map[string]*semaphoreLimiter{},
	}
	if manager, err := harness.NewDockerReuseManager(); err == nil {
		deps.dockerReuse = manager
	}
	deps.applyConfig(cfg)
	return deps
}

func (d *runtimeSharedDeps) applyConfig(cfg *config.Config) {
	if d == nil {
		return
	}
	d.globalLimiter.SetCapacity(cfg.Defaults.MaxConcurrentGlobal)
	if d.agentLimiters == nil {
		d.agentLimiters = map[string]*semaphoreLimiter{}
	}
	for _, agent := range cfg.AgentTypes {
		limiter, ok := d.agentLimiters[agent.Name]
		if !ok {
			limiter = newSemaphoreLimiter(agent.MaxConcurrent)
			d.agentLimiters[agent.Name] = limiter
		}
		limiter.SetCapacity(agent.MaxConcurrent)
	}
}

func (d *runtimeSharedDeps) limiterFor(agent config.AgentTypeConfig) dispatchLimiter {
	if d == nil {
		return nil
	}
	agentLimiter, ok := d.agentLimiters[agent.Name]
	if !ok {
		agentLimiter = newSemaphoreLimiter(agent.MaxConcurrent)
		d.agentLimiters[agent.Name] = agentLimiter
	}
	agentLimiter.SetCapacity(agent.MaxConcurrent)
	return newCompositeLimiter(d.globalLimiter, agentLimiter)
}

func agentMap(cfg *config.Config) map[string]config.AgentTypeConfig {
	agents := map[string]config.AgentTypeConfig{}
	for _, agent := range cfg.AgentTypes {
		agents[agent.Name] = agent
	}
	return agents
}

func buildScopedService(cfg *config.Config, source config.SourceConfig, logger *slog.Logger, shared *runtimeSharedDeps) (*Service, error) {
	agent, ok := agentMap(cfg)[source.AgentType]
	if !ok {
		return nil, fmt.Errorf("source %q references unknown agent_type %q", source.Name, source.AgentType)
	}
	scoped := scopedConfig(cfg, source, agent)
	tr, err := newTracker(source)
	if err != nil {
		return nil, err
	}
	var runnerOpts []harness.ProcessRunnerOption
	if shared != nil && shared.dockerReuse != nil {
		runnerOpts = append(runnerOpts, harness.WithDockerReuseManager(shared.dockerReuse))
	}
	runner, err := harness.NewProcessRunner(agent.Docker, runnerOpts...)
	if err != nil {
		return nil, err
	}
	hr, err := newHarness(agent, runner)
	if err != nil {
		return nil, err
	}
	return NewServiceWithDeps(scoped, logger, Dependencies{
		Tracker:       tr,
		Harness:       hr,
		ProcessRunner: runner,
		Workspace:     workspace.NewManager(cfg.Workspace.Root).WithGitLabAuth(source.Connection.BaseURL, source.Connection.Token),
		StateStore:    state.NewStore(config.ScopedStateDir(cfg, source)),
		Limiter:       shared.limiterFor(agent),
	})
}

func (d *runtimeSharedDeps) Close() error {
	if d == nil || d.dockerReuse == nil {
		return nil
	}
	return d.dockerReuse.Close()
}
