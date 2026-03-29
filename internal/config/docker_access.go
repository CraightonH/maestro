package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

const DockerHomeDefault = "/tmp/maestro-home"

const (
	DockerSecretEnvPresetAnthropicBaseURL = "anthropic-base-url"
	DockerMountPresetClaudeConfig         = "claude-config"
	DockerMountPresetCodexConfig          = "codex-config"
	DockerMountPresetNetrc                = "netrc"
	DockerMountPresetGitCredentials       = "git-credentials"
	DockerMountPresetGHConfig             = "gh-config"
	DockerMountPresetGitConfig            = "git-config"
)

type DockerEnvGrant struct {
	Source string
	Target string
	Origin string
}

type DockerMountGrant struct {
	Source string
	Target string
	Origin string
}

type DockerAccessSummary struct {
	Env          []DockerEnvGrant
	SecretMounts []DockerMountGrant
	ToolMounts   []DockerMountGrant
}

func cloneDockerSecretsConfig(src *DockerSecretsConfig) *DockerSecretsConfig {
	if src == nil {
		return nil
	}
	cloned := *src
	if src.Env != nil {
		cloned.Env = append([]DockerSecretEnvConfig{}, src.Env...)
	}
	if src.Mounts != nil {
		cloned.Mounts = append([]DockerAccessMountConfig{}, src.Mounts...)
	}
	return &cloned
}

func cloneDockerToolsConfig(src *DockerToolsConfig) *DockerToolsConfig {
	if src == nil {
		return nil
	}
	cloned := *src
	if src.Mounts != nil {
		cloned.Mounts = append([]DockerAccessMountConfig{}, src.Mounts...)
	}
	return &cloned
}

func mergeDockerSecretsConfig(base *DockerSecretsConfig, override *DockerSecretsConfig) *DockerSecretsConfig {
	if base == nil && override == nil {
		return nil
	}
	if base == nil {
		return cloneDockerSecretsConfig(override)
	}
	merged := cloneDockerSecretsConfig(base)
	if override == nil {
		return merged
	}
	if override.Env != nil {
		merged.Env = append([]DockerSecretEnvConfig{}, override.Env...)
	}
	if override.Mounts != nil {
		merged.Mounts = append([]DockerAccessMountConfig{}, override.Mounts...)
	}
	return merged
}

func mergeDockerToolsConfig(base *DockerToolsConfig, override *DockerToolsConfig) *DockerToolsConfig {
	if base == nil && override == nil {
		return nil
	}
	if base == nil {
		return cloneDockerToolsConfig(override)
	}
	merged := cloneDockerToolsConfig(base)
	if override == nil {
		return merged
	}
	if override.Mounts != nil {
		merged.Mounts = append([]DockerAccessMountConfig{}, override.Mounts...)
	}
	return merged
}

func NormalizeDockerSecretEnvPreset(name string) string {
	return strings.TrimSpace(strings.ToLower(name))
}

func KnownDockerSecretEnvPreset(name string) bool {
	switch NormalizeDockerSecretEnvPreset(name) {
	case "", DockerAuthClaudeAPIKey, DockerAuthClaudeProxy, DockerAuthCodexAPIKey, DockerSecretEnvPresetAnthropicBaseURL:
		return true
	default:
		return false
	}
}

func ResolveDockerSecretEnvPreset(name string) (source string, target string) {
	switch NormalizeDockerSecretEnvPreset(name) {
	case DockerAuthClaudeAPIKey:
		return "ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY"
	case DockerAuthClaudeProxy:
		return "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN"
	case DockerAuthCodexAPIKey:
		return "OPENAI_API_KEY", "OPENAI_API_KEY"
	case DockerSecretEnvPresetAnthropicBaseURL:
		return "ANTHROPIC_BASE_URL", "ANTHROPIC_BASE_URL"
	default:
		return "", ""
	}
}

func NormalizeDockerAccessMountPreset(name string) string {
	return strings.TrimSpace(strings.ToLower(name))
}

func KnownDockerAccessMountPreset(name string) bool {
	switch NormalizeDockerAccessMountPreset(name) {
	case "", DockerMountPresetClaudeConfig, DockerMountPresetCodexConfig, DockerMountPresetNetrc, DockerMountPresetGitCredentials, DockerMountPresetGHConfig, DockerMountPresetGitConfig:
		return true
	default:
		return false
	}
}

func DockerAccessMountPresetTarget(name string, homeTarget string) string {
	homeTarget = dockerHomeTarget(homeTarget)
	switch NormalizeDockerAccessMountPreset(name) {
	case DockerMountPresetClaudeConfig:
		return filepath.Join(homeTarget, ".claude")
	case DockerMountPresetCodexConfig:
		return filepath.Join(homeTarget, ".codex")
	case DockerMountPresetNetrc:
		return filepath.Join(homeTarget, ".netrc")
	case DockerMountPresetGitCredentials:
		return filepath.Join(homeTarget, ".git-credentials")
	case DockerMountPresetGHConfig:
		return filepath.Join(homeTarget, ".config", "gh")
	case DockerMountPresetGitConfig:
		return filepath.Join(homeTarget, ".gitconfig")
	default:
		return ""
	}
}

func ResolveDockerSecretEnvConfig(item DockerSecretEnvConfig) (source string, target string, origin string, err error) {
	preset := NormalizeDockerSecretEnvPreset(item.Preset)
	if preset != "" {
		if strings.TrimSpace(item.Source) != "" || strings.TrimSpace(item.Target) != "" {
			return "", "", "", fmt.Errorf("preset cannot be combined with source or target")
		}
		if !KnownDockerSecretEnvPreset(preset) {
			return "", "", "", fmt.Errorf("unknown preset %q", item.Preset)
		}
		source, target = ResolveDockerSecretEnvPreset(preset)
		return source, target, "secret preset " + preset, nil
	}

	source = strings.TrimPrefix(strings.TrimSpace(item.Source), "$")
	target = strings.TrimPrefix(strings.TrimSpace(item.Target), "$")
	if source == "" {
		return "", "", "", fmt.Errorf("source is required")
	}
	if target == "" {
		target = source
	}
	return source, target, "secret env", nil
}

func ResolveDockerAccessMountConfig(item DockerAccessMountConfig, homeTarget string) (source string, target string, origin string, err error) {
	preset := NormalizeDockerAccessMountPreset(item.Preset)
	source = strings.TrimSpace(item.Source)
	target = strings.TrimSpace(item.Target)
	if preset != "" {
		if target != "" {
			return "", "", "", fmt.Errorf("preset cannot be combined with target")
		}
		if !KnownDockerAccessMountPreset(preset) {
			return "", "", "", fmt.Errorf("unknown preset %q", item.Preset)
		}
		if source == "" {
			return "", "", "", fmt.Errorf("source is required")
		}
		target = DockerAccessMountPresetTarget(preset, homeTarget)
		return source, target, "mount preset " + preset, nil
	}
	if source == "" {
		return "", "", "", fmt.Errorf("source is required")
	}
	if target == "" {
		return "", "", "", fmt.Errorf("target is required")
	}
	return source, target, "mount", nil
}

func ResolveDockerAccess(docker *DockerConfig, homeTarget string) DockerAccessSummary {
	var access DockerAccessSummary
	if docker == nil {
		return access
	}
	homeTarget = dockerHomeTarget(homeTarget)

	if docker.Auth != nil {
		mode := NormalizeDockerAuthMode(docker.Auth.Mode)
		switch {
		case DockerAuthModeUsesEnv(mode):
			source := strings.TrimSpace(docker.Auth.Source)
			if source == "" {
				source = DockerAuthDefaultSource(mode)
			}
			target := strings.TrimSpace(docker.Auth.Target)
			if target == "" {
				target = DockerAuthDefaultTarget(mode, homeTarget)
			}
			if source != "" && target != "" {
				access.Env = append(access.Env, DockerEnvGrant{
					Source: source,
					Target: target,
					Origin: "auth preset " + mode,
				})
			}
		case DockerAuthModeUsesMount(mode):
			source := strings.TrimSpace(docker.Auth.Source)
			target := strings.TrimSpace(docker.Auth.Target)
			if target == "" {
				target = DockerAuthDefaultTarget(mode, homeTarget)
			}
			if source != "" && target != "" {
				access.SecretMounts = append(access.SecretMounts, DockerMountGrant{
					Source: source,
					Target: target,
					Origin: "auth preset " + mode,
				})
			}
		}
	}

	for _, rawKey := range docker.EnvPassthrough {
		key := strings.TrimPrefix(strings.TrimSpace(rawKey), "$")
		if key == "" {
			continue
		}
		access.Env = append(access.Env, DockerEnvGrant{
			Source: key,
			Target: key,
			Origin: "legacy env_passthrough",
		})
	}

	if docker.Secrets != nil {
		for _, item := range docker.Secrets.Env {
			source, target, origin, err := ResolveDockerSecretEnvConfig(item)
			if err != nil {
				continue
			}
			access.Env = append(access.Env, DockerEnvGrant{
				Source: source,
				Target: target,
				Origin: origin,
			})
		}
		for _, item := range docker.Secrets.Mounts {
			source, target, origin, err := ResolveDockerAccessMountConfig(item, homeTarget)
			if err != nil {
				continue
			}
			access.SecretMounts = append(access.SecretMounts, DockerMountGrant{
				Source: source,
				Target: target,
				Origin: "secret " + origin,
			})
		}
	}

	for _, mount := range docker.Mounts {
		grant := DockerMountGrant{
			Source: strings.TrimSpace(mount.Source),
			Target: strings.TrimSpace(mount.Target),
			Origin: "legacy mount",
		}
		if grant.Source == "" || grant.Target == "" {
			continue
		}
		if DockerMountLooksSecret(grant.Source, grant.Target) {
			access.SecretMounts = append(access.SecretMounts, grant)
			continue
		}
		access.ToolMounts = append(access.ToolMounts, grant)
	}

	if docker.Tools != nil {
		for _, item := range docker.Tools.Mounts {
			source, target, origin, err := ResolveDockerAccessMountConfig(item, homeTarget)
			if err != nil {
				continue
			}
			access.ToolMounts = append(access.ToolMounts, DockerMountGrant{
				Source: source,
				Target: target,
				Origin: "tool " + origin,
			})
		}
	}

	return access
}

func DockerMountLooksSecret(source string, target string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	target = strings.ToLower(strings.TrimSpace(target))
	for _, candidate := range []string{target, source} {
		if candidate == "" {
			continue
		}
		if strings.Contains(candidate, ".claude") || strings.Contains(candidate, ".codex") {
			return true
		}
		if strings.Contains(candidate, ".netrc") || strings.Contains(candidate, ".git-credentials") {
			return true
		}
		if strings.Contains(candidate, "auth.json") || strings.Contains(candidate, "credentials") || strings.Contains(candidate, "token") {
			return true
		}
		if strings.Contains(candidate, "/run/secrets") {
			return true
		}
	}
	return false
}

func dockerHomeTarget(homeTarget string) string {
	homeTarget = strings.TrimSpace(homeTarget)
	if homeTarget == "" {
		return DockerHomeDefault
	}
	return homeTarget
}
