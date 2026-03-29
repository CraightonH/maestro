package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/harness"
	"github.com/tjohnson/maestro/internal/testutil"
)

func liveCodexDockerHarnessConfig(t *testing.T) (testutil.LiveDockerHarnessConfig, string, []string) {
	t.Helper()

	live := testutil.RequireLiveDockerHarness(
		t,
		"MAESTRO_TEST_LIVE_CODEX_DOCKER",
		"MAESTRO_TEST_DOCKER_CODEX_IMAGE",
		[]string{"OPENAI_API_KEY"},
		testutil.DefaultCodexAuthSource(),
		"MAESTRO_TEST_DOCKER_CODEX_AUTH_SOURCE",
		"MAESTRO_TEST_DOCKER_CODEX_AUTH_TARGET",
	)

	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	if baseURL == "" {
		return live, "", nil
	}

	if apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); apiKey != "" {
		homeDir := t.TempDir()
		authDir := filepath.Join(homeDir, ".codex")
		if err := os.MkdirAll(authDir, 0o755); err != nil {
			t.Fatalf("mkdir codex auth dir: %v", err)
		}
		authBody, err := json.MarshalIndent(map[string]string{
			"auth_mode":      "apikey",
			"OPENAI_API_KEY": apiKey,
		}, "", "  ")
		if err != nil {
			t.Fatalf("marshal codex auth: %v", err)
		}
		if err := os.WriteFile(filepath.Join(authDir, "auth.json"), append(authBody, '\n'), 0o600); err != nil {
			t.Fatalf("write codex auth: %v", err)
		}
		live.Docker.Mounts = append(live.Docker.Mounts, config.DockerMountConfig{
			Source: homeDir,
			Target: "/tmp/maestro-home",
		})
		live.RunEnv["HOME"] = "/tmp/maestro-home"
	}

	model := strings.TrimSpace(os.Getenv("MAESTRO_TEST_DOCKER_CODEX_MODEL"))
	if model == "" {
		model = "openai/gpt-5-mini"
	}

	return live, model, []string{
		"--config", `forced_login_method="api"`,
		"--config", fmt.Sprintf(`model=%q`, model),
		"--config", fmt.Sprintf(`openai_base_url=%q`, baseURL),
	}
}

func TestLiveCodexHarness(t *testing.T) {
	testutil.RequireFlag(t, "MAESTRO_TEST_LIVE_CODEX")

	adapter, err := NewAdapter()
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var stdout strings.Builder
	run, err := adapter.Start(ctx, harness.RunConfig{
		RunID:   "live-codex-run",
		Prompt:  "Reply with exactly: MAESTRO_CODEX_SMOKE_OK",
		Workdir: t.TempDir(),
		Stdout:  &stdout,
	})
	if err != nil {
		t.Fatalf("start harness: %v", err)
	}
	if err := run.Wait(); err != nil {
		t.Fatalf("wait harness: %v", err)
	}

	if !strings.Contains(stdout.String(), "MAESTRO_CODEX_SMOKE_OK") {
		t.Fatalf("unexpected live codex output: %q", stdout.String())
	}
}

func TestLiveCodexManualApproval(t *testing.T) {
	testutil.RequireFlag(t, "MAESTRO_TEST_LIVE_CODEX")

	adapter, err := NewAdapter()
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	workdir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var stdout strings.Builder
	run, err := adapter.Start(ctx, harness.RunConfig{
		RunID:          "live-codex-approval-run",
		Prompt:         "Run the shell command `printf MAESTRO_CODEX_APPROVAL_OK > APPROVAL_OK.txt` and then reply with exactly MAESTRO_CODEX_APPROVAL_OK.",
		Workdir:        workdir,
		ApprovalPolicy: "manual",
		Stdout:         &stdout,
	})
	if err != nil {
		t.Fatalf("start harness: %v", err)
	}

	var approval harness.ApprovalRequest
	select {
	case approval = <-adapter.Approvals():
	case <-time.After(60 * time.Second):
		t.Skip("codex app-server did not emit an approval request within 60s under on-request policy")
	}

	if err := adapter.Approve(ctx, harness.ApprovalDecision{
		RequestID: approval.RequestID,
		Decision:  "approve",
	}); err != nil {
		t.Fatalf("approve request: %v", err)
	}

	if err := run.Wait(); err != nil {
		t.Fatalf("wait harness: %v", err)
	}

	if !strings.Contains(stdout.String(), "MAESTRO_CODEX_APPROVAL_OK") {
		t.Fatalf("unexpected live codex output: %q", stdout.String())
	}
	content, err := os.ReadFile(filepath.Join(workdir, "APPROVAL_OK.txt"))
	if err != nil {
		t.Fatalf("read approval file: %v", err)
	}
	if strings.TrimSpace(string(content)) != "MAESTRO_CODEX_APPROVAL_OK" {
		t.Fatalf("approval file content = %q", string(content))
	}
}

func TestLiveCodexMessageRequest(t *testing.T) {
	testutil.RequireFlag(t, "MAESTRO_TEST_LIVE_CODEX")

	adapter, err := NewAdapter()
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var stdout strings.Builder
	run, err := adapter.Start(ctx, harness.RunConfig{
		RunID: "live-codex-question-run",
		Prompt: strings.Join([]string{
			"Before doing any work, use request_user_input to ask one concise question about scope.",
			"Ask whether you should update the API contract too.",
			"After you receive the answer `yes`, reply with exactly MAESTRO_CODEX_QUESTION_OK.",
		}, " "),
		Workdir: t.TempDir(),
		Stdout:  &stdout,
	})
	if err != nil {
		t.Fatalf("start harness: %v", err)
	}

	var request harness.MessageRequest
	select {
	case request = <-adapter.Messages():
	case <-time.After(60 * time.Second):
		t.Skip("codex did not emit a request_user_input message within 60s")
	}

	if !strings.Contains(strings.ToLower(request.Body), "api") {
		t.Fatalf("unexpected message body: %q", request.Body)
	}
	if err := adapter.Reply(ctx, harness.MessageReply{
		RequestID: request.RequestID,
		Kind:      request.Kind,
		Reply:     "yes",
		RepliedAt: time.Now(),
	}); err != nil {
		t.Fatalf("reply message request: %v", err)
	}

	if err := run.Wait(); err != nil {
		t.Fatalf("wait harness: %v", err)
	}
	if !strings.Contains(stdout.String(), "MAESTRO_CODEX_QUESTION_OK") {
		t.Fatalf("unexpected live codex output: %q", stdout.String())
	}
}

func TestLiveCodexHarnessDocker(t *testing.T) {
	live, model, extraArgs := liveCodexDockerHarnessConfig(t)

	runner, err := harness.NewProcessRunner(&live.Docker)
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}
	adapter, err := NewAdapter(WithProcessRunner(runner))
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	workdir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	var stdout strings.Builder
	run, err := adapter.Start(ctx, harness.RunConfig{
		RunID:     "live-codex-docker-run",
		Prompt:    "Create a file named DOCKER_OK.txt containing exactly MAESTRO_CODEX_DOCKER_OK and then reply with exactly MAESTRO_CODEX_DOCKER_OK.",
		Workdir:   workdir,
		Stdout:    &stdout,
		Env:       map[string]string(live.RunEnv),
		Model:     model,
		ExtraArgs: extraArgs,
	})
	if err != nil {
		t.Fatalf("start harness: %v", err)
	}
	if err := run.Wait(); err != nil {
		t.Fatalf("wait harness: %v", err)
	}

	if !strings.Contains(stdout.String(), "MAESTRO_CODEX_DOCKER_OK") {
		t.Fatalf("unexpected live codex docker output: %q", stdout.String())
	}
	content, err := os.ReadFile(filepath.Join(workdir, "DOCKER_OK.txt"))
	if err != nil {
		t.Fatalf("read docker output file: %v", err)
	}
	if strings.TrimSpace(string(content)) != "MAESTRO_CODEX_DOCKER_OK" {
		t.Fatalf("docker output file content = %q", string(content))
	}
}

func TestLiveCodexManualApprovalDocker(t *testing.T) {
	live, model, extraArgs := liveCodexDockerHarnessConfig(t)

	runner, err := harness.NewProcessRunner(&live.Docker)
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}
	adapter, err := NewAdapter(WithProcessRunner(runner))
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	workdir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	var stdout strings.Builder
	run, err := adapter.Start(ctx, harness.RunConfig{
		RunID:          "live-codex-docker-approval-run",
		Prompt:         "Run the shell command `printf MAESTRO_CODEX_DOCKER_APPROVAL_OK > APPROVAL_OK.txt` and then reply with exactly MAESTRO_CODEX_DOCKER_APPROVAL_OK.",
		Workdir:        workdir,
		ApprovalPolicy: "manual",
		Stdout:         &stdout,
		Env:            map[string]string(live.RunEnv),
		Model:          model,
		ExtraArgs:      extraArgs,
	})
	if err != nil {
		t.Fatalf("start harness: %v", err)
	}

	var approval harness.ApprovalRequest
	select {
	case approval = <-adapter.Approvals():
	case <-time.After(60 * time.Second):
		t.Skip("codex docker app-server did not emit an approval request within 60s under on-request policy")
	}

	if err := adapter.Approve(ctx, harness.ApprovalDecision{
		RequestID: approval.RequestID,
		Decision:  "approve",
	}); err != nil {
		t.Fatalf("approve request: %v", err)
	}

	if err := run.Wait(); err != nil {
		t.Fatalf("wait harness: %v", err)
	}
	if !strings.Contains(stdout.String(), "MAESTRO_CODEX_DOCKER_APPROVAL_OK") {
		t.Fatalf("unexpected live codex docker approval output: %q", stdout.String())
	}
	content, err := os.ReadFile(filepath.Join(workdir, "APPROVAL_OK.txt"))
	if err != nil {
		t.Fatalf("read approval file: %v", err)
	}
	if strings.TrimSpace(string(content)) != "MAESTRO_CODEX_DOCKER_APPROVAL_OK" {
		t.Fatalf("approval file content = %q", string(content))
	}
}

func TestLiveCodexMessageRequestDocker(t *testing.T) {
	live, model, extraArgs := liveCodexDockerHarnessConfig(t)

	runner, err := harness.NewProcessRunner(&live.Docker)
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}
	adapter, err := NewAdapter(WithProcessRunner(runner))
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	var stdout strings.Builder
	run, err := adapter.Start(ctx, harness.RunConfig{
		RunID: "live-codex-docker-question-run",
		Prompt: strings.Join([]string{
			"Before doing any work, use request_user_input to ask one concise question about scope.",
			"Ask whether you should update the API contract too.",
			"After you receive the answer `yes`, reply with exactly MAESTRO_CODEX_DOCKER_QUESTION_OK.",
		}, " "),
		Workdir:   t.TempDir(),
		Stdout:    &stdout,
		Env:       map[string]string(live.RunEnv),
		Model:     model,
		ExtraArgs: extraArgs,
	})
	if err != nil {
		t.Fatalf("start harness: %v", err)
	}

	var request harness.MessageRequest
	select {
	case request = <-adapter.Messages():
	case <-time.After(60 * time.Second):
		t.Skip("codex docker did not emit a request_user_input message within 60s")
	}

	if !strings.Contains(strings.ToLower(request.Body), "api") {
		t.Fatalf("unexpected message body: %q", request.Body)
	}
	if err := adapter.Reply(ctx, harness.MessageReply{
		RequestID: request.RequestID,
		Kind:      request.Kind,
		Reply:     "yes",
		RepliedAt: time.Now(),
	}); err != nil {
		t.Fatalf("reply message request: %v", err)
	}

	if err := run.Wait(); err != nil {
		t.Fatalf("wait harness: %v", err)
	}
	if !strings.Contains(stdout.String(), "MAESTRO_CODEX_DOCKER_QUESTION_OK") {
		t.Fatalf("unexpected live codex docker message output: %q", stdout.String())
	}
}

func TestLiveCodexHarnessContinuationDocker(t *testing.T) {
	live, model, extraArgs := liveCodexDockerHarnessConfig(t)

	runner, err := harness.NewProcessRunner(&live.Docker)
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}
	adapter, err := NewAdapter(WithProcessRunner(runner))
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	var stdout strings.Builder
	run, err := adapter.Start(ctx, harness.RunConfig{
		RunID:     "live-codex-docker-multi-turn-run",
		Prompt:    "Remember the token MAESTRO_CODEX_DOCKER_MULTI_TURN_OK and reply with exactly TURN_ONE_OK",
		Workdir:   t.TempDir(),
		Stdout:    &stdout,
		Env:       map[string]string(live.RunEnv),
		Model:     model,
		ExtraArgs: extraArgs,
		MaxTurns:  2,
		ContinuationFunc: func(ctx context.Context, turnNumber int) (string, bool, error) {
			if turnNumber != 1 {
				return "", false, nil
			}
			return "What token did I ask you to remember? Reply with exactly MAESTRO_CODEX_DOCKER_MULTI_TURN_OK", true, nil
		},
	})
	if err != nil {
		t.Fatalf("start harness: %v", err)
	}
	if err := run.Wait(); err != nil {
		t.Fatalf("wait harness: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "TURN_ONE_OK") {
		t.Fatalf("unexpected live codex docker output: %q", got)
	}
	if !strings.Contains(got, "MAESTRO_CODEX_DOCKER_MULTI_TURN_OK") {
		t.Fatalf("unexpected live codex docker continuation output: %q", got)
	}
}
