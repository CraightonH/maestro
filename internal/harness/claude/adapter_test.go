package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/harness"
)

func writeStubClaude(t *testing.T, dir string, script string) string {
	t.Helper()

	binary := filepath.Join(dir, "claude")
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub claude: %v", err)
	}
	return binary
}

func TestAdapterStartAndWaitWithStubBinary(t *testing.T) {
	tmp := t.TempDir()
	argsPath := filepath.Join(tmp, "args.txt")
	script := "#!/bin/sh\nprintf '%s\n' \"$@\" > \"" + argsPath + "\"\ncat >/tmp/claude-input.$$ \nprintf '%s\n' '{\"type\":\"assistant\"}'\nprintf '{\"type\":\"result\",\"result\":\"prompt:'\nprintf '%s' \"$(cat /tmp/claude-input.$$)\"\nprintf '%s\n' '\"}'\nrm -f /tmp/claude-input.$$\n"
	writeStubClaude(t, tmp, script)

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
	writeStubClaude(t, tmp, script)

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

func TestAdapterMultiTurnContinuationWithStubBinary(t *testing.T) {
	tmp := t.TempDir()
	invocations := filepath.Join(tmp, "invocations.txt")
	statePath := filepath.Join(tmp, "state.txt")
	script := `#!/bin/sh
if [ "$1" = "--help" ]; then
  printf '%s\n' '  -r, --resume [value]'
  printf '%s\n' '  -c, --continue'
  exit 0
fi
printf '%s\n' "$@" >> "` + invocations + `"
printf '%s\n' "---" >> "` + invocations + `"
prompt=$(cat)
resume=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--resume" ]; then
    resume=$2
    shift 2
    continue
  fi
  shift
done
if [ -n "$resume" ]; then
  saved=$(cat "` + statePath + `")
  printf '%s\n' '{"type":"assistant","session_id":"session-123"}'
  printf '{"type":"result","session_id":"session-123","result":"turn2:'
  printf '%s' "$saved"
  printf '%s\n' '"}'
  exit 0
fi
printf '%s' "$prompt" > "` + statePath + `"
printf '%s\n' '{"type":"assistant","session_id":"session-123"}'
printf '{"type":"result","session_id":"session-123","result":"turn1:'
printf '%s' "$prompt"
printf '%s\n' '"}'
`
	writeStubClaude(t, tmp, script)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	adapter, err := NewAdapter()
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	var stdout strings.Builder
	run, err := adapter.Start(context.Background(), harness.RunConfig{
		RunID:    "run-multi-turn",
		Prompt:   "CLAUDE_RESUME_TOKEN",
		Workdir:  tmp,
		Stdout:   &stdout,
		MaxTurns: 2,
		ContinuationFunc: func(ctx context.Context, turnNumber int) (string, bool, error) {
			if turnNumber != 1 {
				t.Fatalf("turnNumber = %d, want 1", turnNumber)
			}
			return "ignored-second-turn-prompt", true, nil
		},
	})
	if err != nil {
		t.Fatalf("start harness: %v", err)
	}

	if err := run.Wait(); err != nil {
		t.Fatalf("wait harness: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "turn1:CLAUDE_RESUME_TOKEN") {
		t.Fatalf("stdout = %q, want first turn output", got)
	}
	if !strings.Contains(got, "turn2:CLAUDE_RESUME_TOKEN") {
		t.Fatalf("stdout = %q, want resumed turn output", got)
	}

	rawInvocations, err := os.ReadFile(invocations)
	if err != nil {
		t.Fatalf("read invocations: %v", err)
	}
	log := string(rawInvocations)
	if !strings.Contains(log, "--resume\nsession-123") {
		t.Fatalf("invocations = %q, want resumed second turn", log)
	}
}

func TestAdapterMultiTurnRequiresResumeFlagSupport(t *testing.T) {
	tmp := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "--help" ]; then
  printf '%s\n' '  -c, --continue'
  exit 0
fi
echo "unexpected invocation" >&2
exit 1
`
	writeStubClaude(t, tmp, script)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	adapter, err := NewAdapter()
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	_, err = adapter.Start(context.Background(), harness.RunConfig{
		RunID:    "run-needs-resume",
		Prompt:   "hello",
		Workdir:  tmp,
		MaxTurns: 2,
		ContinuationFunc: func(ctx context.Context, turnNumber int) (string, bool, error) {
			return "next", true, nil
		},
	})
	if err == nil {
		t.Fatal("start harness error = nil, want resume support error")
	}
	if !strings.Contains(err.Error(), "does not support multi-turn session resume") {
		t.Fatalf("start harness error = %v, want resume support error", err)
	}
}

func TestAdapterMultiTurnTerminationConditions(t *testing.T) {
	tests := []struct {
		name              string
		maxTurns          int
		wantTurns         string
		wantContinueCalls int
		continuation      func(turnNumber int) (string, bool)
	}{
		{
			name:              "stops when continuation says no",
			maxTurns:          5,
			wantTurns:         "1",
			wantContinueCalls: 1,
			continuation: func(turnNumber int) (string, bool) {
				return "unused", false
			},
		},
		{
			name:              "stops at max turns",
			maxTurns:          3,
			wantTurns:         "3",
			wantContinueCalls: 2,
			continuation: func(turnNumber int) (string, bool) {
				return "next-turn", true
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tmp := t.TempDir()
			countPath := filepath.Join(tmp, "count.txt")
			script := `#!/bin/sh
if [ "$1" = "--help" ]; then
  printf '%s\n' '  -r, --resume [value]'
  exit 0
fi
count=0
if [ -f "` + countPath + `" ]; then
  count=$(cat "` + countPath + `")
fi
count=$((count + 1))
printf '%s' "$count" > "` + countPath + `"
cat >/dev/null
printf '%s\n' '{"type":"assistant","session_id":"session-123"}'
printf '{"type":"result","session_id":"session-123","result":"turn:'
printf '%s' "$count"
printf '%s\n' '"}'
`
			writeStubClaude(t, tmp, script)
			t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

			adapter, err := NewAdapter()
			if err != nil {
				t.Fatalf("new adapter: %v", err)
			}

			continueCalls := 0
			run, err := adapter.Start(context.Background(), harness.RunConfig{
				RunID:    "run-termination",
				Prompt:   "turn-one",
				Workdir:  tmp,
				MaxTurns: test.maxTurns,
				ContinuationFunc: func(ctx context.Context, turnNumber int) (string, bool, error) {
					continueCalls++
					prompt, cont := test.continuation(turnNumber)
					return prompt, cont, nil
				},
			})
			if err != nil {
				t.Fatalf("start harness: %v", err)
			}
			if err := run.Wait(); err != nil {
				t.Fatalf("wait harness: %v", err)
			}

			count, err := os.ReadFile(countPath)
			if err != nil {
				t.Fatalf("read count: %v", err)
			}
			if got := strings.TrimSpace(string(count)); got != test.wantTurns {
				t.Fatalf("turn count = %q, want %q", got, test.wantTurns)
			}
			if continueCalls != test.wantContinueCalls {
				t.Fatalf("continuation calls = %d, want %d", continueCalls, test.wantContinueCalls)
			}
		})
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

func TestClaudeMetricsFromResultEventPrefersModelUsageAndCost(t *testing.T) {
	duration := int64(2507)
	cost := 0.057416999999999996
	event := streamEvent{
		Type:         "result",
		DurationMS:   &duration,
		TotalCostUSD: &cost,
		ModelUsage: claudeModelMap{
			"claude-opus-4-6[1m]": {
				InputTokens:  int64Ptr(3),
				OutputTokens: int64Ptr(16),
			},
		},
	}

	metrics := claudeMetricsFromResultEvent(event)
	if metrics.TokensIn == nil || *metrics.TokensIn != 3 {
		t.Fatalf("tokens_in = %#v, want 3", metrics.TokensIn)
	}
	if metrics.TokensOut == nil || *metrics.TokensOut != 16 {
		t.Fatalf("tokens_out = %#v, want 16", metrics.TokensOut)
	}
	if metrics.TotalTokens == nil || *metrics.TotalTokens != 19 {
		t.Fatalf("total_tokens = %#v, want 19", metrics.TotalTokens)
	}
	if metrics.CostUSD == nil || *metrics.CostUSD != cost {
		t.Fatalf("cost_usd = %#v, want %f", metrics.CostUSD, cost)
	}
	if metrics.DurationMS == nil || *metrics.DurationMS != duration {
		t.Fatalf("duration_ms = %#v, want %d", metrics.DurationMS, duration)
	}
	if metrics.ThroughputTokensPerSecond == nil {
		t.Fatal("throughput = nil, want derived value")
	}
}

func TestAccumulateRunMetricsSumsAcrossTurns(t *testing.T) {
	first := domain.RunMetrics{
		TokensIn:   int64Ptr(10),
		TokensOut:  int64Ptr(5),
		DurationMS: int64Ptr(1000),
		CostUSD:    float64Ptr(0.0100),
	}
	second := domain.RunMetrics{
		TokensIn:   int64Ptr(7),
		TokensOut:  int64Ptr(3),
		DurationMS: int64Ptr(500),
		CostUSD:    float64Ptr(0.0025),
	}

	got := accumulateRunMetrics(first, second)
	if got.TokensIn == nil || *got.TokensIn != 17 {
		t.Fatalf("tokens_in = %#v, want 17", got.TokensIn)
	}
	if got.TokensOut == nil || *got.TokensOut != 8 {
		t.Fatalf("tokens_out = %#v, want 8", got.TokensOut)
	}
	if got.TotalTokens == nil || *got.TotalTokens != 25 {
		t.Fatalf("total_tokens = %#v, want 25", got.TotalTokens)
	}
	if got.DurationMS == nil || *got.DurationMS != 1500 {
		t.Fatalf("duration_ms = %#v, want 1500", got.DurationMS)
	}
	if got.CostUSD == nil || *got.CostUSD != 0.0125 {
		t.Fatalf("cost_usd = %#v, want 0.0125", got.CostUSD)
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}

func float64Ptr(value float64) *float64 {
	return &value
}
