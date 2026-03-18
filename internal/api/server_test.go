package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/orchestrator"
)

type fakeRuntime struct {
	snapshot  orchestrator.Snapshot
	decisions []string
	replies   []string
	stops     []string
}

func (f *fakeRuntime) Snapshot() orchestrator.Snapshot {
	return f.snapshot
}

func (f *fakeRuntime) ResolveApproval(requestID string, decision string) error {
	f.decisions = append(f.decisions, requestID+":"+decision)
	return nil
}

func (f *fakeRuntime) ResolveMessage(requestID string, reply string, resolvedVia string) error {
	f.replies = append(f.replies, requestID+":"+reply+":"+resolvedVia)
	return nil
}

func (f *fakeRuntime) StopRun(runID string, reason string) error {
	f.stops = append(f.stops, runID+":"+reason)
	return nil
}

func TestStatusEndpointReturnsSnapshotAndConfig(t *testing.T) {
	runtime := &fakeRuntime{
		snapshot: orchestrator.Snapshot{
			SourceName: "gitlab-a, linear-a",
			ActiveRuns: []domain.AgentRun{
				{ID: "run-1", AgentName: "coder", SourceName: "gitlab-a", Issue: domain.Issue{Identifier: "team/project#1"}},
			},
			PendingApprovals: []orchestrator.ApprovalView{
				{RequestID: "approval-1", IssueIdentifier: "team/project#1", ToolName: "Write"},
			},
		},
	}
	cfg := &config.Config{
		Server: config.ServerConfig{Enabled: true, Host: "127.0.0.1", Port: 8742},
		Workspace: config.WorkspaceConfig{
			Root: "/tmp/workspaces",
		},
		State: config.StateConfig{
			Dir: "/tmp/state",
		},
		Logging: config.LoggingConfig{
			Dir:      "/tmp/logs",
			MaxFiles: 20,
		},
		Sources: []config.SourceConfig{
			{Name: "gitlab-a", Tracker: "gitlab"},
			{Name: "linear-a", Tracker: "linear"},
		},
		AgentTypes: []config.AgentTypeConfig{
			{Name: "code-pr", Harness: "claude-code", Workspace: "git-clone", ApprovalPolicy: "auto", Prompt: "/tmp/prompt.md"},
		},
	}

	server := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), runtime)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)

	server.httpServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", recorder.Code)
	}

	var payload struct {
		GeneratedAt time.Time      `json:"generated_at"`
		Config      map[string]any `json:"config"`
		Snapshot    map[string]any `json:"snapshot"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload.GeneratedAt.IsZero() {
		t.Fatal("expected generated_at")
	}
	activeRuns, ok := payload.Snapshot["active_runs"].([]any)
	if !ok || len(activeRuns) != 1 {
		t.Fatalf("active runs = %#v, want 1 item", payload.Snapshot["active_runs"])
	}
	approvals, ok := payload.Snapshot["pending_approvals"].([]any)
	if !ok || len(approvals) != 1 {
		t.Fatalf("approvals = %#v, want 1 item", payload.Snapshot["pending_approvals"])
	}
	if _, ok := payload.Config["sources"]; !ok {
		t.Fatalf("config response missing sources: %v", payload.Config)
	}
}

func TestApprovalActionEndpointResolvesDecision(t *testing.T) {
	runtime := &fakeRuntime{}
	cfg := &config.Config{
		Server: config.ServerConfig{Enabled: true, Host: "127.0.0.1", Port: 8742},
	}
	server := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), runtime)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/approval-1/approve", nil)
	server.httpServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", recorder.Code)
	}
	if got := strings.Join(runtime.decisions, ","); got != "approval-1:approve" {
		t.Fatalf("decisions = %q", got)
	}
}

func TestRunStopEndpointStopsRun(t *testing.T) {
	runtime := &fakeRuntime{}
	cfg := &config.Config{
		Server: config.ServerConfig{Enabled: true, Host: "127.0.0.1", Port: 8742},
	}
	server := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), runtime)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/runs/run-1/stop", nil)
	server.httpServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", recorder.Code)
	}
	if got := strings.Join(runtime.stops, ","); !strings.Contains(got, "run-1:stopped from the web console") {
		t.Fatalf("stops = %q", got)
	}
}

func TestResourceEndpointsReturnCollections(t *testing.T) {
	runtime := &fakeRuntime{
		snapshot: orchestrator.Snapshot{
			SourceSummaries: []orchestrator.SourceSummary{
				{Name: "gitlab-a", Tracker: "gitlab", ActiveRunCount: 1},
			},
			ActiveRuns: []domain.AgentRun{
				{ID: "run-1", AgentName: "coder", SourceName: "gitlab-a", Issue: domain.Issue{Identifier: "team/project#1"}},
			},
			RunOutputs: []orchestrator.RunOutputView{
				{RunID: "run-1", SourceName: "gitlab-a", IssueIdentifier: "team/project#1", StdoutTail: "hello"},
			},
			Retries: []orchestrator.RetryView{
				{IssueIdentifier: "team/project#2", SourceName: "gitlab-a", Attempt: 2},
			},
			RecentEvents: []orchestrator.Event{
				{Level: "INFO", Source: "gitlab-a", Message: "polled"},
			},
			PendingApprovals: []orchestrator.ApprovalView{
				{RequestID: "approval-1", IssueIdentifier: "team/project#1", ToolName: "Write"},
			},
			PendingMessages: []orchestrator.MessageView{
				{RequestID: "message-1", IssueIdentifier: "team/project#1", Kind: "before_work", Summary: "Before work: team/project#1", Body: "Reply with start", Resolvable: true},
			},
		},
	}
	cfg := &config.Config{
		Server: config.ServerConfig{Enabled: true, Host: "127.0.0.1", Port: 8742},
	}
	server := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), runtime)

	for _, test := range []struct {
		path       string
		wantCount  string
		wantPieces []string
	}{
		{path: "/api/v1/sources", wantCount: `"count": 1`, wantPieces: []string{`"name": "gitlab-a"`}},
		{path: "/api/v1/runs", wantCount: `"count": 1`, wantPieces: []string{`"agent_name": "coder"`, `"outputs": [`}},
		{path: "/api/v1/retries", wantCount: `"count": 1`, wantPieces: []string{`"issue_identifier": "team/project#2"`}},
		{path: "/api/v1/events", wantCount: `"count": 1`, wantPieces: []string{`"message": "polled"`}},
		{path: "/api/v1/approvals", wantCount: `"count": 1`, wantPieces: []string{`"request_id": "approval-1"`}},
		{path: "/api/v1/messages", wantCount: `"count": 1`, wantPieces: []string{`"request_id": "message-1"`, `"kind": "before_work"`}},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		server.httpServer.Handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status code = %d, want 200", test.path, recorder.Code)
		}
		body := recorder.Body.String()
		if !strings.Contains(body, test.wantCount) {
			t.Fatalf("%s missing count in body:\n%s", test.path, body)
		}
		for _, want := range test.wantPieces {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing %q:\n%s", test.path, want, body)
			}
		}
	}
}

func TestMessageReplyEndpointResolvesReply(t *testing.T) {
	runtime := &fakeRuntime{}
	cfg := &config.Config{
		Server: config.ServerConfig{Enabled: true, Host: "127.0.0.1", Port: 8742},
	}
	server := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), runtime)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/messages/message-1/reply", strings.NewReader(`{"reply":"start"}`))
	request.Header.Set("Content-Type", "application/json")
	server.httpServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", recorder.Code)
	}
	if got := strings.Join(runtime.replies, ","); got != "message-1:start:web" {
		t.Fatalf("replies = %q", got)
	}
}

func TestMessageReplyEndpointRejectsEmptyReply(t *testing.T) {
	runtime := &fakeRuntime{}
	cfg := &config.Config{
		Server: config.ServerConfig{Enabled: true, Host: "127.0.0.1", Port: 8742},
	}
	server := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), runtime)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/messages/message-1/reply", strings.NewReader(`{"reply":"   "}`))
	request.Header.Set("Content-Type", "application/json")
	server.httpServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want 400", recorder.Code)
	}
}

func TestDashboardServesHTML(t *testing.T) {
	runtime := &fakeRuntime{}
	cfg := &config.Config{
		Server: config.ServerConfig{Enabled: true, Host: "127.0.0.1", Port: 8742},
	}
	server := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), runtime)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	server.httpServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{"<title>Maestro</title>", "<div id=\"root\"></div>", "/assets/", "index-"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard missing %q:\n%s", want, body)
		}
	}
}

func TestStreamEndpointEmitsInitialUpdate(t *testing.T) {
	runtime := &fakeRuntime{
		snapshot: orchestrator.Snapshot{
			SourceSummaries: []orchestrator.SourceSummary{{Name: "gitlab-a", Tracker: "gitlab"}},
			ActiveRuns: []domain.AgentRun{
				{ID: "run-1", AgentName: "coder", SourceName: "gitlab-a", Issue: domain.Issue{Identifier: "team/project#1"}},
			},
			PendingApprovals: []orchestrator.ApprovalView{
				{RequestID: "approval-1", IssueIdentifier: "team/project#1", ToolName: "Write"},
			},
		},
	}
	cfg := &config.Config{
		Server: config.ServerConfig{Enabled: true, Host: "127.0.0.1", Port: 8742},
	}
	server := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), runtime)
	testServer := httptest.NewServer(server.httpServer.Handler)
	defer testServer.Close()

	resp, err := http.Get(testServer.URL + "/api/v1/stream")
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("content-type = %q", got)
	}

	reader := bufio.NewReader(resp.Body)
	line1, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read event line: %v", err)
	}
	line2, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read data line: %v", err)
	}
	if !strings.HasPrefix(line1, "event: update") {
		t.Fatalf("unexpected first line: %q", line1)
	}
	if !strings.HasPrefix(line2, "data: ") {
		t.Fatalf("unexpected second line: %q", line2)
	}
	raw := strings.TrimPrefix(strings.TrimSpace(line2), "data: ")
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	snapshot, ok := payload["snapshot"].(map[string]any)
	if !ok {
		t.Fatalf("snapshot payload missing: %#v", payload)
	}
	if got := int(snapshot["source_count"].(float64)); got != 1 {
		t.Fatalf("source_count = %d, want 1", got)
	}
	if got := int(snapshot["active_run_count"].(float64)); got != 1 {
		t.Fatalf("active_run_count = %d, want 1", got)
	}
	if got := int(snapshot["approval_count"].(float64)); got != 1 {
		t.Fatalf("approval_count = %d, want 1", got)
	}
}

func TestPackSavePersistsFiles(t *testing.T) {
	root := t.TempDir()
	packsDir := filepath.Join(root, "agents", "code-pr")
	if err := os.MkdirAll(packsDir, 0o755); err != nil {
		t.Fatalf("mkdir packs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packsDir, "agent.yaml"), []byte(strings.TrimSpace(`
name: code-pr
description: Original description
harness: claude-code
workspace: git-clone
prompt: prompt.md
approval_policy: auto
max_concurrent: 1
tools:
  - git
skills:
  - narrow diffs
context_files:
  - context.md
`)), 0o600); err != nil {
		t.Fatalf("write pack yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packsDir, "prompt.md"), []byte("Old prompt"), 0o600); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packsDir, "context.md"), []byte("Old context"), 0o600); err != nil {
		t.Fatalf("write context: %v", err)
	}

	configPath := filepath.Join(root, "maestro.yaml")
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(`
agent_packs_dir: ./agents
defaults:
  max_concurrent_global: 1
sources:
  - name: demo
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      project: team/project
    filter:
      labels:
        - agent:ready
    agent_type: code-pr
agent_types:
  - name: code-pr
    agent_pack: code-pr
workspace:
  root: ./workspaces
state:
  dir: ./state
logging:
  dir: ./logs
  max_files: 5
server:
  enabled: true
  host: 127.0.0.1
  port: 8742
`)), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	server := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), &fakeRuntime{})

	body := []byte(`{"original_name":"code-pr","name":"code-pr","description":"Updated description","instance_name":"code-pr","harness":"claude-code","workspace":"git-clone","approval_policy":"auto","max_concurrent":2,"tools":["git","make"],"skills":["narrow diffs","verification"],"env_keys":[],"prompt_body":"Updated prompt","context_body":"Updated context"}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/packs/save", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	server.httpServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	promptRaw, err := os.ReadFile(filepath.Join(packsDir, "prompt.md"))
	if err != nil {
		t.Fatalf("read prompt: %v", err)
	}
	if got := strings.TrimSpace(string(promptRaw)); got != "Updated prompt" {
		t.Fatalf("prompt = %q", got)
	}
	contextRaw, err := os.ReadFile(filepath.Join(packsDir, "context.md"))
	if err != nil {
		t.Fatalf("read context: %v", err)
	}
	if got := strings.TrimSpace(string(contextRaw)); got != "Updated context" {
		t.Fatalf("context = %q", got)
	}
	agentRaw, err := os.ReadFile(filepath.Join(packsDir, "agent.yaml"))
	if err != nil {
		t.Fatalf("read agent yaml: %v", err)
	}
	if !strings.Contains(string(agentRaw), "description: Updated description") {
		t.Fatalf("agent yaml missing updated description:\n%s", string(agentRaw))
	}
}

func TestConfigRawValidateAndSaveEndpoints(t *testing.T) {
	configPath, raw := testConfigFile(t)
	cfg := &config.Config{
		ConfigPath: configPath,
		ConfigDir:  filepath.Dir(configPath),
		Server:     config.ServerConfig{Enabled: true, Host: "127.0.0.1", Port: 8742},
		Workspace:  config.WorkspaceConfig{Root: "/tmp/workspaces"},
		State:      config.StateConfig{Dir: "/tmp/state"},
		Logging:    config.LoggingConfig{Dir: "/tmp/logs", MaxFiles: 20},
		Sources:    []config.SourceConfig{{Name: "gitlab-a", Tracker: "gitlab", AgentType: "code-pr"}},
		AgentTypes: []config.AgentTypeConfig{{Name: "code-pr", Harness: "claude-code", Workspace: "git-clone", ApprovalPolicy: "auto", Prompt: "/tmp/prompt.md"}},
	}
	server := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), &fakeRuntime{})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/config/raw", nil)
	server.httpServer.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("config raw status code = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "sources:") {
		t.Fatalf("config raw missing yaml: %s", recorder.Body.String())
	}

	validateRecorder := httptest.NewRecorder()
	validateRequest := httptest.NewRequest(http.MethodPost, "/api/v1/config/validate", strings.NewReader(`{"yaml":`+strconvQuote(raw)+`}`))
	validateRequest.Header.Set("Content-Type", "application/json")
	server.httpServer.Handler.ServeHTTP(validateRecorder, validateRequest)
	if validateRecorder.Code != http.StatusOK {
		t.Fatalf("config validate status code = %d, want 200", validateRecorder.Code)
	}
	if !strings.Contains(validateRecorder.Body.String(), `"ok": true`) {
		t.Fatalf("config validate missing ok response: %s", validateRecorder.Body.String())
	}

	updated := strings.Replace(raw, "gitlab-a", "gitlab-b", 1)
	saveRecorder := httptest.NewRecorder()
	saveRequest := httptest.NewRequest(http.MethodPost, "/api/v1/config/save", strings.NewReader(`{"yaml":`+strconvQuote(updated)+`}`))
	saveRequest.Header.Set("Content-Type", "application/json")
	server.httpServer.Handler.ServeHTTP(saveRecorder, saveRequest)
	if saveRecorder.Code != http.StatusOK {
		t.Fatalf("config save status code = %d, want 200", saveRecorder.Code)
	}
	savedRaw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !strings.Contains(string(savedRaw), "gitlab-b") {
		t.Fatalf("saved config missing updated source name: %s", string(savedRaw))
	}

	dryRunRecorder := httptest.NewRecorder()
	dryRunRequest := httptest.NewRequest(http.MethodPost, "/api/v1/config/dry-run", strings.NewReader(`{"yaml":`+strconvQuote(strings.Replace(updated, "gitlab-b", "gitlab-c", 1))+`}`))
	dryRunRequest.Header.Set("Content-Type", "application/json")
	server.httpServer.Handler.ServeHTTP(dryRunRecorder, dryRunRequest)
	if dryRunRecorder.Code != http.StatusOK {
		t.Fatalf("config dry-run status code = %d, want 200", dryRunRecorder.Code)
	}
	if !strings.Contains(dryRunRecorder.Body.String(), `"diff":`) {
		t.Fatalf("config dry-run missing diff: %s", dryRunRecorder.Body.String())
	}

	backupsRecorder := httptest.NewRecorder()
	backupsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/config/backups", nil)
	server.httpServer.Handler.ServeHTTP(backupsRecorder, backupsRequest)
	if backupsRecorder.Code != http.StatusOK {
		t.Fatalf("config backups status code = %d, want 200", backupsRecorder.Code)
	}
	if !strings.Contains(backupsRecorder.Body.String(), `"count": 1`) {
		t.Fatalf("config backups missing count: %s", backupsRecorder.Body.String())
	}

	backupEntries, err := os.ReadDir(filepath.Dir(configPath))
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	backupName := ""
	for _, entry := range backupEntries {
		if strings.HasPrefix(entry.Name(), filepath.Base(configPath)+".bak.") {
			backupName = entry.Name()
			break
		}
	}
	if backupName == "" {
		t.Fatal("expected backup file to exist after save")
	}
	backupDetailRecorder := httptest.NewRecorder()
	backupDetailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/config/backups/"+backupName, nil)
	server.httpServer.Handler.ServeHTTP(backupDetailRecorder, backupDetailRequest)
	if backupDetailRecorder.Code != http.StatusOK {
		t.Fatalf("config backup detail status code = %d, want 200", backupDetailRecorder.Code)
	}
	if !strings.Contains(backupDetailRecorder.Body.String(), `"backup"`) {
		t.Fatalf("config backup detail missing payload: %s", backupDetailRecorder.Body.String())
	}

	restoreRecorder := httptest.NewRecorder()
	restoreRequest := httptest.NewRequest(http.MethodPost, "/api/v1/config/backups/"+backupName, nil)
	server.httpServer.Handler.ServeHTTP(restoreRecorder, restoreRequest)
	if restoreRecorder.Code != http.StatusOK {
		t.Fatalf("config restore status code = %d, want 200", restoreRecorder.Code)
	}
	restoredRaw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read restored config: %v", err)
	}
	if !strings.Contains(string(restoredRaw), "gitlab-a") {
		t.Fatalf("restore did not bring back original config: %s", string(restoredRaw))
	}

	createBackupRecorder := httptest.NewRecorder()
	createBackupRequest := httptest.NewRequest(http.MethodPost, "/api/v1/config/backups/create", nil)
	server.httpServer.Handler.ServeHTTP(createBackupRecorder, createBackupRequest)
	if createBackupRecorder.Code != http.StatusOK {
		t.Fatalf("config backup create status code = %d, want 200", createBackupRecorder.Code)
	}
	if !strings.Contains(createBackupRecorder.Body.String(), `"ok": true`) {
		t.Fatalf("config backup create missing ok response: %s", createBackupRecorder.Body.String())
	}

	invalidSaveRecorder := httptest.NewRecorder()
	invalidSaveRequest := httptest.NewRequest(http.MethodPost, "/api/v1/config/save", strings.NewReader(`{"yaml":"sources:\n  - name: broken\n    tracker: gitlab\n"}`))
	invalidSaveRequest.Header.Set("Content-Type", "application/json")
	server.httpServer.Handler.ServeHTTP(invalidSaveRecorder, invalidSaveRequest)
	if invalidSaveRecorder.Code != http.StatusOK {
		t.Fatalf("invalid config save status code = %d, want 200", invalidSaveRecorder.Code)
	}
	if !strings.Contains(invalidSaveRecorder.Body.String(), `"ok": false`) {
		t.Fatalf("invalid config save missing failed response: %s", invalidSaveRecorder.Body.String())
	}
	postInvalidRaw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after invalid save: %v", err)
	}
	if string(postInvalidRaw) != string(restoredRaw) {
		t.Fatalf("invalid save should not modify config\nwant:\n%s\n\ngot:\n%s", string(restoredRaw), string(postInvalidRaw))
	}
}

func TestConfigValidateRejectsBrokenYAML(t *testing.T) {
	server := New(&config.Config{Server: config.ServerConfig{Enabled: true, Host: "127.0.0.1", Port: 8742}}, slog.New(slog.NewTextHandler(io.Discard, nil)), &fakeRuntime{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/config/validate", strings.NewReader(`{"yaml":"sources:\n  - name: bad\n    tracker: gitlab\n"}`))
	request.Header.Set("Content-Type", "application/json")
	server.httpServer.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("broken validate status code = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"ok": false`) {
		t.Fatalf("expected failed validation response: %s", recorder.Body.String())
	}
}

func testConfigFile(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GITLAB_TOKEN", "test-token")
	promptPath := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("demo prompt"), 0o600); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	raw := strings.TrimSpace(`
defaults:
  poll_interval: 30s
  stall_timeout: 10m
  max_concurrent_global: 1
user:
  name: Demo Operator
sources:
  - name: gitlab-a
    tracker: gitlab
    connection:
      base_url: https://gitlab.example.com
      token_env: GITLAB_TOKEN
      project: team/project
    filter:
      labels: ["agent:ready"]
    agent_type: code-pr
agent_types:
  - name: code-pr
    harness: claude-code
    workspace: git-clone
    prompt: `+promptPath+`
    approval_policy: auto
    max_concurrent: 1
workspace:
  root: `+filepath.Join(dir, "workspaces")+`
state:
  dir: `+filepath.Join(dir, "state")+`
logging:
  dir: `+filepath.Join(dir, "logs")+`
`) + "\n"
	configPath := filepath.Join(dir, "maestro.yaml")
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, raw
}

func strconvQuote(input string) string {
	raw, _ := json.Marshal(input)
	return string(raw)
}
