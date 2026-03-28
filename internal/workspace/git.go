package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/tjohnson/maestro/internal/redact"
)

func runGit(ctx context.Context, dir string, args ...string) error {
	return runGitWithEnv(ctx, dir, nil, args...)
}

func runGitWithEnv(ctx context.Context, dir string, extraEnv []string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Env = append(cmd.Env, extraEnv...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"git %s: %w: %s",
			redact.String(strings.Join(args, " ")),
			err,
			redact.String(strings.TrimSpace(string(output))),
		)
	}

	return nil
}
