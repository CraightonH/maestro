package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/tjohnson/maestro/internal/prompt"
)

// ValidateMVP enforces the intentionally narrow Phase 1 configuration contract.
func ValidateMVP(cfg *Config) error {
	if len(cfg.Sources) == 0 {
		return fmt.Errorf("config requires at least one source")
	}
	if len(cfg.AgentTypes) == 0 {
		return fmt.Errorf("config requires at least one agent type")
	}
	if cfg.Defaults.MaxConcurrentGlobal < 1 {
		return fmt.Errorf("defaults.max_concurrent_global must be at least 1")
	}
	if cfg.Defaults.StallTimeout.Duration <= 0 {
		return fmt.Errorf("defaults.stall_timeout must be greater than zero")
	}

	agentsByName := make(map[string]AgentTypeConfig, len(cfg.AgentTypes))
	channelKinds := make(map[string]string, len(cfg.Channels))
	for i, channel := range cfg.Channels {
		name := strings.TrimSpace(channel.Name)
		if name == "" {
			return fmt.Errorf("channels[%d].name is required", i)
		}
		if _, exists := channelKinds[name]; exists {
			return fmt.Errorf("channels[%d].name %q is duplicated", i, name)
		}
		kind := strings.TrimSpace(channel.Kind)
		if kind == "" {
			return fmt.Errorf("channel %q kind is required", name)
		}
		if !slices.Contains([]string{"slack", "teams", "gitlab", "tui"}, kind) {
			return fmt.Errorf("channel %q kind must be one of slack, teams, gitlab, tui", name)
		}
		channelKinds[name] = kind
	}

	for i, agent := range cfg.AgentTypes {
		repoPackPath, hasRepoPack := ParseRepoPackRef(agent.AgentPack)
		if strings.TrimSpace(agent.Name) == "" {
			return fmt.Errorf("agent_types[%d].name is required", i)
		}
		if _, exists := agentsByName[agent.Name]; exists {
			return fmt.Errorf("agent_types[%d].name %q is duplicated", i, agent.Name)
		}
		agentsByName[agent.Name] = agent

		if agent.Harness != "claude-code" && agent.Harness != "codex" {
			return fmt.Errorf("agent %q requires harness claude-code or codex", agent.Name)
		}
		if agent.Harness == "codex" && agent.Claude != nil {
			return fmt.Errorf("agent %q has harness codex but includes claude config", agent.Name)
		}
		if agent.Harness == "claude-code" && agent.Codex != nil {
			return fmt.Errorf("agent %q has harness claude-code but includes codex config", agent.Name)
		}
		if err := validateDockerConfig(agent.Name, agent.Docker); err != nil {
			return err
		}
		if cfg.CodexDefaults != nil && cfg.CodexDefaults.MaxTurns < 0 {
			return fmt.Errorf("codex_defaults max_turns must be at least 1")
		}
		if cfg.ClaudeDefaults != nil && cfg.ClaudeDefaults.MaxTurns < 0 {
			return fmt.Errorf("claude_defaults max_turns must be at least 1")
		}
		if agent.Codex != nil && agent.Codex.MaxTurns < 0 {
			return fmt.Errorf("agent %q codex max_turns must be at least 1", agent.Name)
		}
		if agent.Claude != nil && agent.Claude.MaxTurns < 0 {
			return fmt.Errorf("agent %q claude max_turns must be at least 1", agent.Name)
		}
		if !slices.Contains([]string{"git-clone", "none"}, agent.Workspace) {
			return fmt.Errorf("agent %q requires workspace git-clone or none", agent.Name)
		}
		if hasRepoPack && agent.Workspace != "git-clone" {
			return fmt.Errorf("agent %q requires workspace git-clone for repo pack %q", agent.Name, repoPackPath)
		}
		if !slices.Contains([]string{"auto", "manual"}, agent.ApprovalPolicy) {
			return fmt.Errorf("agent %q requires approval_policy to be one of auto, manual", agent.Name)
		}
		if agent.MaxConcurrent < 1 {
			return fmt.Errorf("agent %q max_concurrent must be at least 1", agent.Name)
		}
		if agent.StallTimeout.Duration <= 0 {
			return fmt.Errorf("agent %q stall_timeout must be greater than zero", agent.Name)
		}
		if agent.ApprovalTimeout.Duration <= 0 {
			return fmt.Errorf("agent %q approval_timeout must be greater than zero", agent.Name)
		}
		if !hasRepoPack {
			if strings.TrimSpace(agent.Prompt) == "" {
				return fmt.Errorf("agent %q prompt path is required", agent.Name)
			}
			if _, err := os.Stat(agent.Prompt); err != nil {
				return fmt.Errorf("agent %q prompt %q: %w", agent.Name, agent.Prompt, err)
			}
			if _, err := prompt.ParseFile(agent.Prompt); err != nil {
				return fmt.Errorf("agent %q prompt %q: %w", agent.Name, agent.Prompt, err)
			}
		}
		if strings.TrimSpace(agent.Communication) != "" {
			kind, ok := channelKinds[agent.Communication]
			if !ok {
				return fmt.Errorf("agent %q references unknown communication channel %q", agent.Name, agent.Communication)
			}
			if kind == "slack" {
				tokenEnv := strings.TrimSpace(channelConfigString(channelByName(cfg.Channels, agent.Communication).Config, "token_env"))
				appTokenEnv := strings.TrimSpace(channelConfigString(channelByName(cfg.Channels, agent.Communication).Config, "app_token_env"))
				if tokenEnv == "" {
					return fmt.Errorf("slack channel %q requires config.token_env", agent.Communication)
				}
				if appTokenEnv == "" {
					return fmt.Errorf("slack channel %q requires config.app_token_env", agent.Communication)
				}
			}
		}
	}

	if strings.TrimSpace(cfg.Defaults.LabelPrefix) == "" {
		return fmt.Errorf("defaults.label_prefix must be non-empty")
	}
	reservedLabels := reservedLifecycleLabels(cfg.Defaults.LabelPrefix)
	if err := validateLifecycleTransition("defaults.on_complete", cfg.Defaults.OnComplete, reservedLabels); err != nil {
		return err
	}
	if err := validateLifecycleTransition("defaults.on_failure", cfg.Defaults.OnFailure, reservedLabels); err != nil {
		return err
	}

	sourceNames := map[string]struct{}{}
	for i, source := range cfg.Sources {
		if strings.TrimSpace(source.Name) == "" {
			return fmt.Errorf("sources[%d].name is required", i)
		}
		if _, exists := sourceNames[source.Name]; exists {
			return fmt.Errorf("sources[%d].name %q is duplicated", i, source.Name)
		}
		sourceNames[source.Name] = struct{}{}

		if source.Tracker != "gitlab" && source.Tracker != "gitlab-epic" && source.Tracker != "linear" {
			return fmt.Errorf("source %q requires tracker=gitlab, gitlab-epic, or linear", source.Name)
		}
		if source.Tracker != "linear" && strings.TrimSpace(source.Connection.BaseURL) == "" {
			return fmt.Errorf("source %q connection.base_url is required", source.Name)
		}
		if source.Tracker == "gitlab" && strings.TrimSpace(source.Connection.Project) == "" {
			return fmt.Errorf("source %q connection.project is required", source.Name)
		}
		if source.Tracker == "gitlab-epic" && strings.TrimSpace(source.Connection.GroupPath()) == "" {
			return fmt.Errorf("source %q gitlab epic sources require connection.group", source.Name)
		}
		if IsZeroFilter(source.EffectiveIssueFilter()) && IsZeroFilter(source.EffectiveEpicFilter()) {
			return fmt.Errorf("source %q filter must include labels, states, or assignee", source.Name)
		}
		if source.Tracker == "gitlab-epic" && strings.TrimSpace(source.EffectiveEpicFilter().Assignee) != "" {
			return fmt.Errorf("source %q epic_filter.assignee is unsupported; use issue_filter.assignee for linked issues", source.Name)
		}
		if strings.TrimSpace(source.AgentType) == "" {
			return fmt.Errorf("source %q agent_type is required", source.Name)
		}
		agent, ok := agentsByName[source.AgentType]
		if !ok {
			return fmt.Errorf("source %q references unknown agent_type %q", source.Name, source.AgentType)
		}
		if requiresSourceRepo(agent.Workspace, source.Tracker) && strings.TrimSpace(source.Repo) == "" {
			return fmt.Errorf("source %q requires repo for git-clone workspace", source.Name)
		}
		if strings.TrimSpace(source.Repo) != "" {
			if err := validateRepoURL(source.Repo); err != nil {
				return fmt.Errorf("source %q: %w", source.Name, err)
			}
		}
		if source.EffectiveRetryBase(cfg.State) <= 0 {
			return fmt.Errorf("source %q retry_base must be greater than zero", source.Name)
		}
		if source.EffectiveMaxRetryBackoff(cfg.State) < source.EffectiveRetryBase(cfg.State) {
			return fmt.Errorf("source %q max_retry_backoff must be at least retry_base", source.Name)
		}
		if source.EffectiveMaxAttempts(cfg.State) < 1 {
			return fmt.Errorf("source %q max_attempts must be at least 1", source.Name)
		}
		if err := validateLifecycleLabels(source, cfg.Defaults.OnComplete, cfg.Defaults.OnFailure, reservedLabels); err != nil {
			return err
		}
	}
	if strings.TrimSpace(cfg.State.Dir) == "" {
		return fmt.Errorf("state dir is required")
	}
	if cfg.State.RetryBase.Duration <= 0 {
		return fmt.Errorf("state.retry_base must be greater than zero")
	}
	if cfg.State.MaxRetryBackoff.Duration < cfg.State.RetryBase.Duration {
		return fmt.Errorf("state.max_retry_backoff must be at least retry_base")
	}
	if cfg.State.MaxAttempts < 1 {
		return fmt.Errorf("state.max_attempts must be at least 1")
	}
	if cfg.Hooks.Timeout.Duration <= 0 {
		return fmt.Errorf("hooks.timeout must be greater than zero")
	}
	switch strings.TrimSpace(cfg.Hooks.Execution) {
	case "", "host", "container":
	default:
		return fmt.Errorf("hooks.execution must be one of: host, container")
	}
	if strings.Contains(cfg.Controls.BeforeWork.Prompt, "{{") {
		return fmt.Errorf("controls.before_work.prompt must be plain text for v0.1")
	}
	switch strings.TrimSpace(cfg.Controls.BeforeWork.Mode) {
	case "", "review", "reply":
	default:
		return fmt.Errorf("controls.before_work.mode must be one of: review, reply")
	}
	if cfg.Logging.MaxFiles < 0 {
		return fmt.Errorf("logging.max_files must be zero or greater")
	}
	if cfg.Server.Enabled {
		if strings.TrimSpace(cfg.Server.Host) == "" {
			return fmt.Errorf("server.host is required when server.enabled is true")
		}
		if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
			return fmt.Errorf("server.port must be between 1 and 65535 when server.enabled is true")
		}
	}
	return nil
}

func channelByName(channels []ChannelConfig, name string) ChannelConfig {
	for _, channel := range channels {
		if channel.Name == name {
			return channel
		}
	}
	return ChannelConfig{}
}

func channelConfigString(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func validateRepoURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if isScpStyleRepoURL(raw) {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid repo url %q: %w", raw, err)
	}
	if parsed.User != nil {
		return fmt.Errorf("repo urls must not embed credentials; use connection.token_env instead")
	}
	return nil
}

func validateDockerConfig(agentName string, docker *DockerConfig) error {
	if docker == nil {
		return nil
	}
	if strings.TrimSpace(docker.Image) == "" {
		return fmt.Errorf("agent %q docker.image is required", agentName)
	}
	if path := strings.TrimSpace(docker.WorkspaceMountPath); path != "" && !filepath.IsAbs(path) {
		return fmt.Errorf("agent %q docker.workspace_mount_path must be absolute", agentName)
	}
	if policy := strings.TrimSpace(docker.PullPolicy); policy != "" {
		switch policy {
		case "missing", "always", "never":
		default:
			return fmt.Errorf("agent %q docker.pull_policy must be one of missing, always, never", agentName)
		}
	}
	switch mode := strings.TrimSpace(docker.Network); mode {
	case "", "bridge", "none", "host":
	default:
		return fmt.Errorf("agent %q docker.network must be one of bridge, none, host", agentName)
	}
	if docker.CPUs < 0 {
		return fmt.Errorf("agent %q docker.cpus must be zero or greater", agentName)
	}
	if docker.PIDsLimit < 0 {
		return fmt.Errorf("agent %q docker.pids_limit must be zero or greater", agentName)
	}
	if err := validateDockerAuthConfig(agentName, docker.Auth); err != nil {
		return err
	}
	if err := validateDockerSecurityConfig(agentName, docker.Security); err != nil {
		return err
	}
	if err := validateDockerCacheConfig(agentName, docker.Cache); err != nil {
		return err
	}
	for _, envName := range docker.EnvPassthrough {
		trimmed := strings.TrimPrefix(strings.TrimSpace(envName), "$")
		if trimmed == "" || strings.Contains(trimmed, "=") {
			return fmt.Errorf("agent %q docker.env_passthrough contains invalid name %q", agentName, envName)
		}
	}
	for _, mount := range docker.Mounts {
		if strings.TrimSpace(mount.Source) == "" {
			return fmt.Errorf("agent %q docker.mounts[].source is required", agentName)
		}
		if strings.TrimSpace(mount.Target) == "" {
			return fmt.Errorf("agent %q docker.mounts[].target is required", agentName)
		}
		if !filepath.IsAbs(strings.TrimSpace(mount.Target)) {
			return fmt.Errorf("agent %q docker.mounts[].target must be absolute", agentName)
		}
		if !mount.ReadOnly {
			return fmt.Errorf("agent %q docker.mounts[] must be read_only in phase 1", agentName)
		}
	}
	return nil
}

func validateDockerAuthConfig(agentName string, auth *DockerAuthConfig) error {
	if auth == nil {
		return nil
	}
	mode := NormalizeDockerAuthMode(auth.Mode)
	if mode == "" {
		if strings.TrimSpace(auth.Source) != "" || strings.TrimSpace(auth.Target) != "" {
			return fmt.Errorf("agent %q docker.auth.mode is required when docker.auth is configured", agentName)
		}
		return nil
	}
	switch mode {
	case DockerAuthClaudeAPIKey, DockerAuthClaudeProxy, DockerAuthClaudeConfig, DockerAuthCodexAPIKey, DockerAuthCodexConfig:
	default:
		return fmt.Errorf("agent %q docker.auth.mode must be one of claude-api-key, claude-proxy, claude-config-mount, codex-api-key, codex-config-mount", agentName)
	}
	if DockerAuthModeUsesEnv(mode) {
		if source := strings.TrimPrefix(strings.TrimSpace(auth.Source), "$"); source != "" && !isValidEnvName(source) {
			return fmt.Errorf("agent %q docker.auth.source contains invalid env name %q", agentName, auth.Source)
		}
		if target := strings.TrimPrefix(strings.TrimSpace(auth.Target), "$"); target != "" && !isValidEnvName(target) {
			return fmt.Errorf("agent %q docker.auth.target contains invalid env name %q", agentName, auth.Target)
		}
		return nil
	}
	if strings.TrimSpace(auth.Source) == "" {
		return fmt.Errorf("agent %q docker.auth.source is required for mode %s", agentName, mode)
	}
	if target := strings.TrimSpace(auth.Target); target != "" && !filepath.IsAbs(target) {
		return fmt.Errorf("agent %q docker.auth.target must be absolute for mode %s", agentName, mode)
	}
	return nil
}

func validateDockerSecurityConfig(agentName string, security *DockerSecurityConfig) error {
	if security == nil {
		return nil
	}
	for _, capName := range security.DropCapabilities {
		if strings.TrimSpace(capName) == "" {
			return fmt.Errorf("agent %q docker.security.drop_capabilities contains an empty capability name", agentName)
		}
	}
	for _, tmpfs := range security.Tmpfs {
		target := strings.TrimSpace(tmpfs)
		if target == "" {
			return fmt.Errorf("agent %q docker.security.tmpfs contains an empty mount target", agentName)
		}
		if !filepath.IsAbs(target) {
			return fmt.Errorf("agent %q docker.security.tmpfs target %q must be absolute", agentName, tmpfs)
		}
	}
	return nil
}

func validateDockerCacheConfig(agentName string, cache *DockerCacheConfig) error {
	if cache == nil {
		return nil
	}
	for _, profile := range cache.Profiles {
		if !KnownDockerCacheProfile(profile) {
			return fmt.Errorf("agent %q docker.cache.profiles contains unknown profile %q", agentName, profile)
		}
	}
	for _, mount := range cache.Mounts {
		if strings.TrimSpace(mount.Source) == "" {
			return fmt.Errorf("agent %q docker.cache.mounts[].source is required", agentName)
		}
		if strings.TrimSpace(mount.Target) == "" {
			return fmt.Errorf("agent %q docker.cache.mounts[].target is required", agentName)
		}
		if !filepath.IsAbs(strings.TrimSpace(mount.Target)) {
			return fmt.Errorf("agent %q docker.cache.mounts[].target must be absolute", agentName)
		}
	}
	return nil
}

func isValidEnvName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "=") {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		case r == '_':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

var scpStyleRepoURLPattern = regexp.MustCompile(`^[^@\s/:]+@[^:/\s]+:.+$`)

func isScpStyleRepoURL(raw string) bool {
	return scpStyleRepoURLPattern.MatchString(strings.TrimSpace(raw))
}

func requiresSourceRepo(workspace string, tracker string) bool {
	return workspace == "git-clone" && (tracker == "linear" || tracker == "gitlab-epic")
}

func reservedLifecycleLabels(prefix string) map[string]struct{} {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if prefix == "" {
		prefix = "maestro"
	}
	return map[string]struct{}{
		prefix + ":active": {},
		prefix + ":retry":  {},
		prefix + ":done":   {},
		prefix + ":failed": {},
	}
}

func validateLifecycleLabels(source SourceConfig, defaultComplete *LifecycleTransition, defaultFailure *LifecycleTransition, reserved map[string]struct{}) error {
	if err := validateLifecycleTransition(fmt.Sprintf("source %q on_complete", source.Name), ResolveLifecycleTransition(defaultComplete, source.OnComplete), reserved); err != nil {
		return err
	}
	if err := validateLifecycleTransition(fmt.Sprintf("source %q on_failure", source.Name), ResolveLifecycleTransition(defaultFailure, source.OnFailure), reserved); err != nil {
		return err
	}
	return nil
}

func validateLifecycleTransition(name string, transition *LifecycleTransition, reserved map[string]struct{}) error {
	if transition == nil {
		return nil
	}
	for _, label := range transition.AddLabels {
		normalized := strings.ToLower(strings.TrimSpace(label))
		if _, ok := reserved[normalized]; ok {
			return fmt.Errorf("%s.add_labels must not include reserved lifecycle label %q", name, label)
		}
	}
	return nil
}
