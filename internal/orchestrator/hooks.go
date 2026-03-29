package orchestrator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/harness"
)

func (s *Service) runHook(ctx context.Context, script string, workdir string, run *domain.AgentRun, stage string) error {
	if strings.TrimSpace(script) == "" || strings.TrimSpace(workdir) == "" {
		return nil
	}

	hookCtx, cancel := context.WithTimeout(ctx, s.cfg.Hooks.Timeout.Duration)
	defer cancel()

	cmd, err := s.hookCommand(hookCtx, script, workdir, run, stage)
	if err != nil {
		return err
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("hook %s failed: %w output=%s", stage, err, sanitizeOutput(strings.TrimSpace(string(output))))
	}
	return nil
}

func (s *Service) hookCommand(ctx context.Context, script string, workdir string, run *domain.AgentRun, stage string) (*exec.Cmd, error) {
	shell, args := shellCommand(script)
	mode := strings.ToLower(strings.TrimSpace(s.cfg.Hooks.Execution))
	if mode == "" {
		mode = "host"
	}

	switch mode {
	case "host":
		cmd := exec.CommandContext(ctx, shell, args...)
		cmd.Dir = workdir
		cmd.Env = append(os.Environ(), hookEnv(run, stage)...)
		return cmd, nil
	case "container":
		if s.processRunner == nil || s.processRunner.Kind() != "docker" {
			return nil, fmt.Errorf("hook %s requires docker execution but no docker runner is configured", stage)
		}
		return s.processRunner.CommandContext(ctx, harness.ProcessSpec{
			Binary:  shell,
			Args:    args,
			Workdir: workdir,
			Env:     hookEnvMap(run, stage),
		})
	default:
		return nil, fmt.Errorf("unknown hook execution mode %q", s.cfg.Hooks.Execution)
	}
}

func (s *Service) runHookBestEffort(ctx context.Context, script string, workdir string, run *domain.AgentRun, stage string) {
	if err := s.runHook(ctx, script, workdir, run, stage); err != nil {
		s.recordRunEvent(run, "warn", "%v", err)
	} else if strings.TrimSpace(script) != "" && strings.TrimSpace(workdir) != "" {
		s.recordRunEvent(run, "info", "hook %s completed for %s", stage, run.Issue.Identifier)
	}
}

func hookEnv(run *domain.AgentRun, stage string) []string {
	return []string{
		"MAESTRO_RUN_ID=" + run.ID,
		"MAESTRO_ISSUE_ID=" + run.Issue.ID,
		"MAESTRO_ISSUE_IDENTIFIER=" + run.Issue.Identifier,
		"MAESTRO_AGENT_NAME=" + run.AgentName,
		"MAESTRO_AGENT_TYPE=" + run.AgentType,
		"MAESTRO_RUN_STAGE=" + stage,
		"MAESTRO_RUN_STATUS=" + string(run.Status),
		"MAESTRO_WORKSPACE_PATH=" + run.WorkspacePath,
	}
}

func hookEnvMap(run *domain.AgentRun, stage string) map[string]string {
	env := hookEnv(run, stage)
	out := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		out[key] = value
	}
	return out
}

func shellCommand(script string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", script}
	}
	return "sh", []string{"-lc", script}
}
