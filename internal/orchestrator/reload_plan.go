package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tjohnson/maestro/internal/config"
)

type reloadAction string

const (
	reloadActionKeep    reloadAction = "keep"
	reloadActionStart   reloadAction = "start"
	reloadActionStop    reloadAction = "stop"
	reloadActionRestart reloadAction = "restart"
)

type reloadServiceSpec struct {
	Source    config.SourceConfig
	Agent     config.AgentTypeConfig
	Signature string
}

type reloadTransition struct {
	SourceName string
	Action     reloadAction
	Current    *reloadServiceSpec
	Desired    *reloadServiceSpec
}

type reloadPlan struct {
	Transitions []reloadTransition
	Desired     map[string]reloadServiceSpec
}

type serviceConfigSnapshot struct {
	Source         config.SourceConfig         `json:"source"`
	Agent          config.AgentTypeConfig      `json:"agent"`
	Defaults       config.DefaultsConfig       `json:"defaults"`
	CodexDefaults  *config.CodexConfig         `json:"codex_defaults,omitempty"`
	ClaudeDefaults *config.ClaudeConfig        `json:"claude_defaults,omitempty"`
	User           config.UserConfig           `json:"user"`
	Workspace      config.WorkspaceConfig      `json:"workspace"`
	State          config.StateConfig          `json:"state"`
	Hooks          config.HooksConfig          `json:"hooks"`
	Controls       config.ControlsConfig       `json:"controls"`
	SourceDefaults config.SourceDefaultsConfig `json:"source_defaults"`
}

func planReload(current *config.Config, desired *config.Config) (reloadPlan, error) {
	currentSpecs, err := buildReloadServiceSpecs(current)
	if err != nil {
		return reloadPlan{}, err
	}
	return planReloadWithCurrentSpecs(currentSpecs, desired)
}

func planReloadWithCurrentSpecs(currentSpecs map[string]reloadServiceSpec, desired *config.Config) (reloadPlan, error) {
	desiredSpecs, err := buildReloadServiceSpecs(desired)
	if err != nil {
		return reloadPlan{}, err
	}

	sourceNames := make([]string, 0, len(currentSpecs)+len(desiredSpecs))
	seen := map[string]struct{}{}
	for name := range currentSpecs {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		sourceNames = append(sourceNames, name)
	}
	for name := range desiredSpecs {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		sourceNames = append(sourceNames, name)
	}
	sort.Strings(sourceNames)

	transitions := make([]reloadTransition, 0, len(sourceNames))
	for _, name := range sourceNames {
		currentSpec, hasCurrent := currentSpecs[name]
		desiredSpec, hasDesired := desiredSpecs[name]
		switch {
		case hasCurrent && hasDesired && currentSpec.Signature == desiredSpec.Signature:
			transitions = append(transitions, reloadTransition{
				SourceName: name,
				Action:     reloadActionKeep,
				Current:    &currentSpec,
				Desired:    &desiredSpec,
			})
		case !hasCurrent && hasDesired:
			transitions = append(transitions, reloadTransition{
				SourceName: name,
				Action:     reloadActionStart,
				Desired:    &desiredSpec,
			})
		case hasCurrent && !hasDesired:
			transitions = append(transitions, reloadTransition{
				SourceName: name,
				Action:     reloadActionStop,
				Current:    &currentSpec,
			})
		default:
			transitions = append(transitions, reloadTransition{
				SourceName: name,
				Action:     reloadActionRestart,
				Current:    &currentSpec,
				Desired:    &desiredSpec,
			})
		}
	}

	return reloadPlan{
		Transitions: transitions,
		Desired:     desiredSpecs,
	}, nil
}

func buildReloadServiceSpecs(cfg *config.Config) (map[string]reloadServiceSpec, error) {
	if cfg == nil {
		return nil, nil
	}
	agents := agentMap(cfg)
	specs := make(map[string]reloadServiceSpec, len(cfg.Sources))
	for _, source := range cfg.Sources {
		agent, ok := agents[source.AgentType]
		if !ok {
			return nil, fmt.Errorf("source %q references unknown agent_type %q", source.Name, source.AgentType)
		}
		signature, err := serviceSignature(cfg, source, agent)
		if err != nil {
			return nil, fmt.Errorf("signature for source %q: %w", source.Name, err)
		}
		specs[source.Name] = reloadServiceSpec{
			Source:    source,
			Agent:     agent,
			Signature: signature,
		}
	}
	return specs, nil
}

func serviceSignature(cfg *config.Config, source config.SourceConfig, agent config.AgentTypeConfig) (string, error) {
	scoped := scopedConfig(cfg, source, agent)
	snapshot := serviceConfigSnapshot{
		Source:         scoped.Sources[0],
		Agent:          scoped.AgentTypes[0],
		Defaults:       scoped.Defaults,
		CodexDefaults:  scoped.CodexDefaults,
		ClaudeDefaults: scoped.ClaudeDefaults,
		User:           scoped.User,
		Workspace:      scoped.Workspace,
		State:          scoped.State,
		Hooks:          scoped.Hooks,
		Controls:       scoped.Controls,
		SourceDefaults: scoped.SourceDefaults,
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("encode config snapshot: %w", err)
	}
	assetDigest, err := fingerprintPaths(serviceAssetPaths(agent))
	if err != nil {
		return "", fmt.Errorf("fingerprint service assets: %w", err)
	}

	sum := sha256.Sum256(append(raw, []byte(assetDigest)...))
	return hex.EncodeToString(sum[:]), nil
}

func reloadWatchPaths(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	paths := []string{cfg.ConfigPath}
	for _, source := range cfg.Sources {
		agent, ok := agentMap(cfg)[source.AgentType]
		if !ok {
			continue
		}
		paths = append(paths, serviceAssetPaths(agent)...)
	}
	return uniqueSortedPaths(paths)
}

func serviceAssetPaths(agent config.AgentTypeConfig) []string {
	paths := []string{}
	if strings.TrimSpace(agent.PackPath) != "" {
		paths = append(paths, filepath.Dir(agent.PackPath))
	}
	if strings.TrimSpace(agent.Prompt) != "" {
		paths = append(paths, agent.Prompt)
	}
	for _, path := range agent.ContextFiles {
		if strings.TrimSpace(path) != "" {
			paths = append(paths, path)
		}
	}
	if strings.TrimSpace(agent.PackClaudeDir) != "" {
		paths = append(paths, agent.PackClaudeDir)
	}
	if strings.TrimSpace(agent.PackCodexDir) != "" {
		paths = append(paths, agent.PackCodexDir)
	}
	return uniqueSortedPaths(paths)
}

func uniqueSortedPaths(paths []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		cleaned := filepath.Clean(trimmed)
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	sort.Strings(out)
	return out
}

func fingerprintPaths(paths []string) (string, error) {
	h := sha256.New()
	for _, path := range uniqueSortedPaths(paths) {
		if err := fingerprintPath(h, path); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fingerprintPath(h hash.Hash, path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			_, _ = h.Write([]byte("missing:" + path + "\n"))
			return nil
		}
		return fmt.Errorf("stat %q: %w", path, err)
	}
	if info.IsDir() {
		return fingerprintDir(h, path)
	}
	return fingerprintFile(h, path, info)
}

func fingerprintDir(h hash.Hash, root string) error {
	_, _ = h.Write([]byte("dir:" + root + "\n"))
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				_, _ = h.Write([]byte("gone:" + path + "\n"))
				return nil
			}
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			_, _ = h.Write([]byte("subdir:" + relative + "\n"))
			return nil
		}
		return fingerprintFile(h, path, info, relative)
	})
}

func fingerprintFile(h hash.Hash, path string, info fs.FileInfo, relative ...string) error {
	name := path
	if len(relative) > 0 && strings.TrimSpace(relative[0]) != "" {
		name = relative[0]
	}
	_, _ = h.Write([]byte("file:" + name + "\n"))
	_, _ = h.Write([]byte(info.Mode().String()))
	_, _ = h.Write([]byte{'\n'})
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		_, _ = h.Write([]byte("symlink:" + target + "\n"))
		return nil
	}
	if !info.Mode().IsRegular() {
		_, _ = h.Write([]byte("non-regular\n"))
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	contentSum := sha256.Sum256(raw)
	_, _ = h.Write(contentSum[:])
	_, _ = h.Write([]byte{'\n'})
	return nil
}
