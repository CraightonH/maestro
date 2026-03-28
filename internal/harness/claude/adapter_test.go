package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tjohnson/maestro/internal/harness"
)

func TestAdapterStartAndWaitWithStubBinary(t *testing.T) {
	tmp := t.TempDir()
	binary := filepath.Join(tmp, "claude")
	argsPath := filepath.Join(tmp, "args.txt")
	script := "#!/bin/sh\nprintf '%s\n' \"$@\" > \"" + argsPath + "\"\ncat >/tmp/claude-input.$$ \nprintf '%s\n' '{\"type\":\"assistant\"}'\nprintf '{\"type\":\"result\",\"result\":\"prompt:'\nprintf '%s' \"$(cat /tmp/claude-input.$$)\"\nprintf '%s\n' '\"}'\nrm -f /tmp/claude-input.$$\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub claude: %v", err)
	}

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	adapter, err := NewAdapter()
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	var stdout strings.Builder
	run, err := adapter.Start(context.Background(), harness.RunConfig{
		RunID:     "run-1",
		Prompt:    "hello world\n",
		Workdir:   tmp,
		Stdout:    &stdout,
		Model:     "opus-test",
		Reasoning: "medium",
	})
	if err != nil {
		t.Fatalf("start harness: %v", err)
	}

	if err := run.Wait(); err != nil {
		t.Fatalf("wait harness: %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "hello world") {
		t.Fatalf("stdout = %q", got)
	}

	rawArgs, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	args := string(rawArgs)
	if !strings.Contains(args, "--permission-mode\nbypassPermissions") {
		t.Fatalf("args = %q, want bypassPermissions", args)
	}
	if !strings.Contains(args, "--output-format\nstream-json") {
		t.Fatalf("args = %q, want stream-json", args)
	}
	if !strings.Contains(args, "--add-dir\n"+tmp) {
		t.Fatalf("args = %q, want add-dir %s", args, tmp)
	}
	if !strings.Contains(args, "--model\nopus-test") {
		t.Fatalf("args = %q, want model override", args)
	}
	if !strings.Contains(args, "--effort\nmedium") {
		t.Fatalf("args = %q, want effort override", args)
	}
}

func TestAdapterApprovalFlowWithStubBinary(t *testing.T) {
	tmp := t.TempDir()
	binary := filepath.Join(tmp, "claude")
	invocations := filepath.Join(tmp, "invocations.txt")
	script := `#!/bin/sh
printf '%s\n' "$@" >> "` + invocations + `"
printf '%s\n' "---" >> "` + invocations + `"
case "$*" in
  *"--output-format stream-json --permission-mode default"*)
    cat >/dev/null
    printf '%s\n' '{"type":"system","subtype":"init"}'
    printf '%s\n' '{"type":"result","subtype":"success","result":"approval pending","permission_denials":[{"tool_name":"Write","tool_use_id":"tool-1","tool_input":{"file_path":"` + tmp + `/APPROVAL.txt","content":"APPROVED"}}]}'
    ;;
  *"--permission-mode bypassPermissions"*)
    cat >/dev/null
    printf '%s\n' '{"type":"assistant"}'
    printf '%s\n' '{"type":"result","result":"APPROVED"}'
    printf '%s' 'APPROVED' > "` + tmp + `/APPROVAL.txt"
    ;;
  *)
    echo "unexpected args: $*" >&2
    exit 1
    ;;
esac
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub claude: %v", err)
	}

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	adapter, err := NewAdapter()
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	var stdout strings.Builder
	run, err := adapter.Start(context.Background(), harness.RunConfig{
		RunID:          "run-approval",
		Prompt:         "please edit",
		Workdir:        tmp,
		ApprovalPolicy: "manual",
		Stdout:         &stdout,
	})
	if err != nil {
		t.Fatalf("start harness: %v", err)
	}

	select {
	case request := <-adapter.Approvals():
		if request.ToolName != "Write" {
			t.Fatalf("tool name = %q, want Write", request.ToolName)
		}
		if !strings.Contains(request.ToolInput, "APPROVAL.txt") {
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
	if got := stdout.String(); !strings.Contains(got, "APPROVED") {
		t.Fatalf("stdout = %q", got)
	}

	content, err := os.ReadFile(filepath.Join(tmp, "APPROVAL.txt"))
	if err != nil {
		t.Fatalf("read approval file: %v", err)
	}
	if strings.TrimSpace(string(content)) != "APPROVED" {
		t.Fatalf("approval file content = %q", string(content))
	}

	rawInvocations, err := os.ReadFile(invocations)
	if err != nil {
		t.Fatalf("read invocations: %v", err)
	}
	log := string(rawInvocations)
	if !strings.Contains(log, "--output-format\nstream-json") {
		t.Fatalf("invocations = %q, want stream-json detection pass", log)
	}
	if !strings.Contains(log, "--permission-mode\nbypassPermissions") {
		t.Fatalf("invocations = %q, want bypassPermissions rerun", log)
	}
	if !strings.Contains(log, "--output-format\nstream-json") {
		t.Fatalf("invocations = %q, want stream-json in permissive run", log)
	}
}

func TestWriteStreamEventRendersTextAndToolUse(t *testing.T) {
	var stdout strings.Builder
	run := &claudeRun{stdout: &stdout}

	input, err := json.Marshal(map[string]any{
		"description": "List files in current directory",
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	run.writeStreamEvent(streamEvent{
		Message: &streamMessage{
			Content: []streamContent{
				{Type: "text", Text: "Let me inspect the repository."},
				{Type: "tool_use", Name: "Bash", Input: input},
			},
		},
	})

	got := stdout.String()
	if !strings.Contains(got, "Let me inspect the repository.\n") {
		t.Fatalf("stdout = %q, want text output", got)
	}
	if !strings.Contains(got, "Using Bash: List files in current directory\n") {
		t.Fatalf("stdout = %q, want tool summary", got)
	}
}

func TestWriteStreamEventSkipsThinkingBlocks(t *testing.T) {
	var stdout strings.Builder
	run := &claudeRun{stdout: &stdout}

	run.writeStreamEvent(streamEvent{
		Message: &streamMessage{
			Content: []streamContent{
				{Type: "thinking", Text: "should not render"},
			},
		},
	})

	if got := stdout.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
}

func TestToolUseSummaryHandlesMCPAndMalformedInput(t *testing.T) {
	if got := toolUseSummary("mcp__elastic__search", json.RawMessage(`{"query":"x"}`)); got != "mcp__elastic__search" {
		t.Fatalf("mcp summary = %q", got)
	}
	if got := toolUseSummary("Read", json.RawMessage(`{`)); got != "Read" {
		t.Fatalf("malformed summary = %q", got)
	}
}

func TestToolUseSummaryFormatsCommonBuiltInTools(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "Read", input: `{"file_path":"internal/api/server.go"}`, want: "Read: internal/api/server.go"},
		{name: "Glob", input: `{"pattern":"templates/*.json"}`, want: "Glob: templates/*.json"},
		{name: "Glob", input: `{"pattern":"*.txt","path":"/tmp/work"}`, want: "Glob: *.txt in /tmp/work"},
		{name: "Grep", input: `{"pattern":"parser.success","path":"internal/"}`, want: `Grep: "parser.success" in internal/`},
		{name: "WebFetch", input: `{"url":"https://example.com"}`, want: "WebFetch: https://example.com"},
	}

	for _, test := range tests {
		if got := toolUseSummary(test.name, json.RawMessage(test.input)); got != test.want {
			t.Fatalf("%s summary = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestToolUseSummaryFallsBackForUnknownTools(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "UnknownTool", input: `{"description":"Summarize the repo"}`, want: "UnknownTool: Summarize the repo"},
		{name: "UnknownTool", input: `{"file_path":"docs/guide.md"}`, want: "UnknownTool: docs/guide.md"},
		{name: "UnknownTool", input: `{"query":"latest release notes"}`, want: "UnknownTool: latest release notes"},
	}

	for _, test := range tests {
		if got := toolUseSummary(test.name, json.RawMessage(test.input)); got != test.want {
			t.Fatalf("%s fallback summary = %q, want %q", test.name, got, test.want)
		}
	}
}
