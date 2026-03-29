package codex

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/harness"
)

func TestAdapterStartAndWaitWithStubAppServer(t *testing.T) {
	tmp := t.TempDir()
	binary := filepath.Join(tmp, "codex")
	script := `#!/bin/sh
if [ "$1" = "app-server" ]; then
  while IFS= read -r line; do
    case "$line" in
      *'"method":"initialize"'*)
        printf '%s\n' '{"id":1,"result":{"userAgent":"stub"}}'
        ;;
      *'"method":"initialized"'*)
        ;;
      *'"method":"thread/start"'*)
        printf '%s\n' '{"id":2,"result":{"approvalPolicy":"never","cwd":"/tmp","model":"gpt-5","modelProvider":"openai","sandbox":{"type":"danger-full-access"},"thread":{"id":"thread-1","cliVersion":"0.0.0","createdAt":0,"cwd":"/tmp","ephemeral":true,"modelProvider":"openai","preview":"","source":"appServer","status":{"type":"idle"},"turns":[],"updatedAt":0}}}'
        ;;
      *'"method":"turn/start"'*)
        printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1","items":[],"status":"inProgress"}}}'
        printf '%s\n' '{"method":"item/agentMessage/delta","params":{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","delta":"CODEX_OK"}}'
        printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","items":[],"status":"completed"}}}'
        exit 0
        ;;
    esac
  done
  exit 0
fi
echo "unexpected args: $@" >&2
exit 1
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub codex: %v", err)
	}

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	adapter, err := NewAdapter()
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	var stdout strings.Builder
	run, err := adapter.Start(context.Background(), harness.RunConfig{
		RunID:   "run-1",
		Prompt:  "hello world",
		Workdir: tmp,
		Stdout:  &stdout,
	})
	if err != nil {
		t.Fatalf("start harness: %v", err)
	}

	if err := run.Wait(); err != nil {
		t.Fatalf("wait harness: %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "CODEX_OK") {
		t.Fatalf("stdout = %q", got)
	}
}

func TestAdapterStartAndWaitWithDockerRunner(t *testing.T) {
	tmp := t.TempDir()
	argsPath := filepath.Join(tmp, "docker-args.txt")
	stdinPath := filepath.Join(tmp, "docker-stdin.txt")
	authDir := filepath.Join(tmp, "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	script := `#!/bin/sh
printf '%s\n' "$@" > "` + argsPath + `"
while IFS= read -r line; do
  printf '%s\n' "$line" >> "` + stdinPath + `"
  case "$line" in
    *'"method":"initialize"'*)
      printf '%s\n' '{"id":1,"result":{"userAgent":"stub"}}'
      ;;
    *'"method":"initialized"'*)
      ;;
    *'"method":"thread/start"'*)
      printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
      ;;
    *'"method":"turn/start"'*)
      printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1","status":"inProgress"}}}'
      printf '%s\n' '{"method":"item/agentMessage/delta","params":{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","delta":"DOCKER_OK"}}'
      printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed"}}}'
      exit 0
      ;;
  esac
done
exit 0
`
	if err := os.WriteFile(filepath.Join(tmp, "docker"), []byte(script), 0o755); err != nil {
		t.Fatalf("write stub docker: %v", err)
	}

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OPENAI_API_KEY", "host-token")

	runner, err := harness.NewProcessRunner(&config.DockerConfig{
		Image:              "codex-image:latest",
		WorkspaceMountPath: "/workspace",
		Mounts: []config.DockerMountConfig{{
			Source:   authDir,
			Target:   "/root/.codex",
			ReadOnly: true,
		}},
		EnvPassthrough: []string{"OPENAI_API_KEY"},
		Network:        "none",
		CPUs:           2,
		Memory:         "3g",
		PIDsLimit:      96,
	})
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}

	adapter, err := NewAdapter(WithProcessRunner(runner))
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	var stdout strings.Builder
	run, err := adapter.Start(context.Background(), harness.RunConfig{
		RunID:          "run-docker",
		Prompt:         "hello docker",
		Workdir:        tmp,
		Stdout:         &stdout,
		ApprovalPolicy: "manual",
		Env: map[string]string{
			"EXPLICIT": "1",
		},
	})
	if err != nil {
		t.Fatalf("start harness: %v", err)
	}

	if err := run.Wait(); err != nil {
		t.Fatalf("wait harness: %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "DOCKER_OK") {
		t.Fatalf("stdout = %q", got)
	}

	rawArgs, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	args := string(rawArgs)
	if !strings.Contains(args, "--mount\ntype=bind,src="+tmp+",dst=/workspace") {
		t.Fatalf("args = %q, want workspace mount", args)
	}
	if !strings.Contains(args, "--mount\ntype=bind,src="+authDir+",dst=/root/.codex,readonly") {
		t.Fatalf("args = %q, want readonly auth mount", args)
	}
	if !strings.Contains(args, "--env\nOPENAI_API_KEY=host-token") || !strings.Contains(args, "--env\nEXPLICIT=1") {
		t.Fatalf("args = %q, want docker envs", args)
	}
	if !strings.Contains(args, "codex-image:latest\ncodex\napp-server") {
		t.Fatalf("args = %q, want image and codex invocation", args)
	}

	rawStdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read stdin log: %v", err)
	}
	stdinLog := string(rawStdin)
	if strings.Contains(stdinLog, `"`+"cwd"+`":"`+tmp+`"`) {
		t.Fatalf("stdin log = %q, want container-visible cwd", stdinLog)
	}
	if !strings.Contains(stdinLog, `"`+"cwd"+`":"/workspace"`) {
		t.Fatalf("stdin log = %q, want /workspace cwd", stdinLog)
	}
	if !strings.Contains(stdinLog, `"`+"writableRoots"+`":["/workspace"]`) {
		t.Fatalf("stdin log = %q, want sandbox writableRoots /workspace", stdinLog)
	}
}

func TestAdapterApprovalFlowWithStubAppServer(t *testing.T) {
	tmp := t.TempDir()
	binary := filepath.Join(tmp, "codex")
	script := `#!/bin/sh
if [ "$1" = "app-server" ]; then
  while IFS= read -r line; do
    case "$line" in
      *'"method":"initialize"'*)
        printf '%s\n' '{"id":1,"result":{"userAgent":"stub"}}'
        ;;
      *'"method":"initialized"'*)
        ;;
      *'"method":"thread/start"'*)
        printf '%s\n' '{"id":2,"result":{"approvalPolicy":"on-request","cwd":"/tmp","model":"gpt-5","modelProvider":"openai","sandbox":{"type":"danger-full-access"},"thread":{"id":"thread-1","cliVersion":"0.0.0","createdAt":0,"cwd":"/tmp","ephemeral":true,"modelProvider":"openai","preview":"","source":"appServer","status":{"type":"idle"},"turns":[],"updatedAt":0}}}'
        ;;
      *'"method":"turn/start"'*)
        printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1","items":[],"status":"inProgress"}}}'
        printf '%s\n' '{"id":40,"method":"item/fileChange/requestApproval","params":{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","reason":"need to edit PROBE.txt"}}'
        read -r response
        case "$response" in
          *'"decision":"accept"'*)
            printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","items":[],"status":"completed"}}}'
            exit 0
            ;;
          *)
            printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","items":[],"status":"failed","error":{"message":"approval denied"}}}}'
            exit 0
            ;;
        esac
        ;;
    esac
  done
  exit 0
fi
echo "unexpected args: $@" >&2
exit 1
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub codex: %v", err)
	}

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	adapter, err := NewAdapter()
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	run, err := adapter.Start(context.Background(), harness.RunConfig{
		RunID:          "run-approval",
		Prompt:         "edit a file",
		Workdir:        tmp,
		ApprovalPolicy: "manual",
	})
	if err != nil {
		t.Fatalf("start harness: %v", err)
	}

	select {
	case request := <-adapter.Approvals():
		if request.ToolName != "file-change" {
			t.Fatalf("tool name = %q, want file-change", request.ToolName)
		}
		if !strings.Contains(request.ToolInput, "PROBE.txt") {
			t.Fatalf("tool input = %q", request.ToolInput)
		}
		if err := adapter.Approve(context.Background(), harness.ApprovalDecision{
			RequestID: request.RequestID,
			Decision:  "approve",
		}); err != nil {
			t.Fatalf("approve request: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for approval request")
	}

	if err := run.Wait(); err != nil {
		t.Fatalf("wait harness: %v", err)
	}
}

func TestAdapterMessageRequestFlowWithStubAppServer(t *testing.T) {
	tmp := t.TempDir()
	binary := filepath.Join(tmp, "codex")
	script := `#!/bin/sh
if [ "$1" = "app-server" ]; then
  while IFS= read -r line; do
    case "$line" in
      *'"method":"initialize"'*)
        printf '%s\n' '{"id":1,"result":{"userAgent":"stub"}}'
        ;;
      *'"method":"initialized"'*)
        ;;
      *'"method":"thread/start"'*)
        printf '%s\n' '{"id":2,"result":{"approvalPolicy":"never","cwd":"/tmp","model":"gpt-5","modelProvider":"openai","sandbox":{"type":"danger-full-access"},"thread":{"id":"thread-1","cliVersion":"0.0.0","createdAt":0,"cwd":"/tmp","ephemeral":true,"modelProvider":"openai","preview":"","source":"appServer","status":{"type":"idle"},"turns":[],"updatedAt":0}}}'
        ;;
      *'"method":"turn/start"'*)
        printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1","items":[],"status":"inProgress"}}}'
        printf '%s\n' '{"id":41,"method":"item/tool/requestUserInput","params":{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","questions":[{"id":"scope","header":"Clarify scope","question":"Should I update the API too?","isOther":true,"isSecret":false,"options":[{"label":"yes","description":"Update the API contract too"},{"label":"no","description":"UI only"}]}]}}'
        read -r response
        case "$response" in
          *'"scope":{"answers":["yes"]}'*)
            printf '%s\n' '{"method":"item/agentMessage/delta","params":{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","delta":"QUESTION_OK"}}'
            printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","items":[],"status":"completed"}}}'
            exit 0
            ;;
          *)
            printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","items":[],"status":"failed","error":{"message":"unexpected reply"}}}}'
            exit 0
            ;;
        esac
        ;;
    esac
  done
  exit 0
fi
echo "unexpected args: $@" >&2
exit 1
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub codex: %v", err)
	}

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	adapter, err := NewAdapter()
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	var stdout strings.Builder
	run, err := adapter.Start(context.Background(), harness.RunConfig{
		RunID:   "run-question",
		Prompt:  "ask one question before writing code",
		Workdir: tmp,
		Stdout:  &stdout,
	})
	if err != nil {
		t.Fatalf("start harness: %v", err)
	}

	select {
	case request := <-adapter.Messages():
		if request.Kind != "agent_question" {
			t.Fatalf("kind = %q, want agent_question", request.Kind)
		}
		if !strings.Contains(request.Body, "Should I update the API too?") {
			t.Fatalf("body = %q", request.Body)
		}
		if err := adapter.Reply(context.Background(), harness.MessageReply{
			RequestID: request.RequestID,
			Kind:      request.Kind,
			Reply:     "yes",
			RepliedAt: time.Now(),
		}); err != nil {
			t.Fatalf("reply request: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message request")
	}

	if err := run.Wait(); err != nil {
		t.Fatalf("wait harness: %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "QUESTION_OK") {
		t.Fatalf("stdout = %q", got)
	}
}

func TestCodexApprovalPolicyAndSandboxSelection(t *testing.T) {
	if got := codexApprovalPolicy("auto"); got != "never" {
		t.Fatalf("auto approval policy = %q, want never", got)
	}
	if got := codexApprovalPolicy("manual"); got != "on-request" {
		t.Fatalf("manual approval policy = %q, want on-request", got)
	}
	if got := codexSandboxMode("auto"); got != "danger-full-access" {
		t.Fatalf("auto sandbox mode = %q, want danger-full-access", got)
	}
	if got := codexSandboxMode("manual"); got != "workspace-write" {
		t.Fatalf("manual sandbox mode = %q, want workspace-write", got)
	}

	policy := codexSandboxPolicy("manual", "/tmp/work")
	if got := policy["type"]; got != "workspaceWrite" {
		t.Fatalf("manual sandbox policy type = %v, want workspaceWrite", got)
	}
	roots, ok := policy["writableRoots"].([]string)
	if !ok || len(roots) != 1 || roots[0] != "/tmp/work" {
		t.Fatalf("manual writableRoots = %#v", policy["writableRoots"])
	}
}

func TestRequestReturnsMarshalErrorWithoutLeakingPendingState(t *testing.T) {
	run := &codexRun{
		pending: map[int64]chan rpcResponse{},
	}

	err := run.request("initialize", map[string]any{
		"bad": func() {},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("request error = %v, want marshal error", err)
	}
	if len(run.pending) != 0 {
		t.Fatalf("pending requests = %d, want 0", len(run.pending))
	}
}

func TestNotifyReturnsMarshalError(t *testing.T) {
	run := &codexRun{}

	err := run.notify("initialized", map[string]any{
		"bad": func() {},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("notify error = %v, want marshal error", err)
	}
}

func TestAdapterStartFailsIfAppServerExitsBeforeInitializeResponse(t *testing.T) {
	tmp := t.TempDir()
	binary := filepath.Join(tmp, "codex")
	script := `#!/bin/sh
if [ "$1" = "app-server" ]; then
  exit 0
fi
echo "unexpected args: $@" >&2
exit 1
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub codex: %v", err)
	}

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	adapter, err := NewAdapter()
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := adapter.Start(context.Background(), harness.RunConfig{
			RunID:   "run-init-exit",
			Prompt:  "hello world",
			Workdir: tmp,
		})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "codex app-server exited before turn completed") {
			t.Fatalf("start error = %v, want app-server exit error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for start failure")
	}
}

func TestAdapterWaitFailsIfAppServerExitsBeforeTurnCompleted(t *testing.T) {
	tmp := t.TempDir()
	binary := filepath.Join(tmp, "codex")
	script := `#!/bin/sh
if [ "$1" = "app-server" ]; then
  while IFS= read -r line; do
    case "$line" in
      *'"method":"initialize"'*)
        printf '%s\n' '{"id":1,"result":{"userAgent":"stub"}}'
        ;;
      *'"method":"initialized"'*)
        ;;
      *'"method":"thread/start"'*)
        printf '%s\n' '{"id":2,"result":{"approvalPolicy":"never","cwd":"/tmp","model":"gpt-5","modelProvider":"openai","sandbox":{"type":"danger-full-access"},"thread":{"id":"thread-1","cliVersion":"0.0.0","createdAt":0,"cwd":"/tmp","ephemeral":true,"modelProvider":"openai","preview":"","source":"appServer","status":{"type":"idle"},"turns":[],"updatedAt":0}}}'
        ;;
      *'"method":"turn/start"'*)
        printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1","items":[],"status":"inProgress"}}}'
        exit 0
        ;;
    esac
  done
  exit 0
fi
echo "unexpected args: $@" >&2
exit 1
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub codex: %v", err)
	}

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	adapter, err := NewAdapter()
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	run, err := adapter.Start(context.Background(), harness.RunConfig{
		RunID:   "run-eof-before-complete",
		Prompt:  "hello world",
		Workdir: tmp,
	})
	if err != nil {
		t.Fatalf("start harness: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- run.Wait()
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "codex app-server exited before turn completed") {
			t.Fatalf("wait error = %v, want app-server exit error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for wait failure")
	}
}

func TestHandleNotificationPublishesTokenUsageMetrics(t *testing.T) {
	var got domain.RunMetrics
	run := &codexRun{
		metricsCallback: func(metrics domain.RunMetrics) {
			got = metrics
		},
	}

	run.handleNotification("thread/tokenUsage/updated", jsonRaw(`{
		"threadId":"thread-1",
		"turnId":"turn-1",
		"tokenUsage":{
			"last":{"inputTokens":4,"outputTokens":2,"totalTokens":6,"cachedInputTokens":0,"reasoningOutputTokens":0},
			"total":{"inputTokens":11,"outputTokens":7,"totalTokens":18,"cachedInputTokens":0,"reasoningOutputTokens":0}
		}
	}`), io.Discard)

	if got.TokensIn == nil || *got.TokensIn != 11 {
		t.Fatalf("tokens_in = %#v, want 11", got.TokensIn)
	}
	if got.TokensOut == nil || *got.TokensOut != 7 {
		t.Fatalf("tokens_out = %#v, want 7", got.TokensOut)
	}
	if got.TotalTokens == nil || *got.TotalTokens != 18 {
		t.Fatalf("total_tokens = %#v, want 18", got.TotalTokens)
	}
}

func jsonRaw(value string) json.RawMessage {
	return json.RawMessage(value)
}
