package channel

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/orchestrator"
)

type fakeRuntime struct {
	snapshot          orchestrator.Snapshot
	mu                sync.Mutex
	approvalDecisions []string
	stopRequests      []string
}

func (f *fakeRuntime) Snapshot() orchestrator.Snapshot {
	return f.snapshot
}

func (f *fakeRuntime) ResolveApproval(requestID string, decision string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approvalDecisions = append(f.approvalDecisions, requestID+":"+decision)
	return nil
}

func (f *fakeRuntime) StopRun(runID string, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopRequests = append(f.stopRequests, runID+":"+reason)
	return nil
}

type fakeSlackClient struct {
	channelID string
	posts     []fakeSlackPost
	updates   []fakeSlackUpdate
}

type fakeSlackPost struct {
	channelID string
	threadTS  string
	text      string
}

type fakeSlackUpdate struct {
	channelID string
	messageTS string
	text      string
}

func (f *fakeSlackClient) ResolveChannel(context.Context) (string, error) {
	return f.channelID, nil
}

func (f *fakeSlackClient) PostMessage(_ context.Context, channelID string, threadTS string, text string, _ []any) (slackPostedMessage, error) {
	f.posts = append(f.posts, fakeSlackPost{
		channelID: channelID,
		threadTS:  threadTS,
		text:      text,
	})
	return slackPostedMessage{
		ChannelID: channelID,
		MessageTS: "ts-" + string(rune('1'+len(f.posts)-1)),
	}, nil
}

func (f *fakeSlackClient) UpdateMessage(_ context.Context, channelID string, messageTS string, text string, _ []any) error {
	f.updates = append(f.updates, fakeSlackUpdate{
		channelID: channelID,
		messageTS: messageTS,
		text:      text,
	})
	return nil
}

func (f *fakeSlackClient) RunSocketMode(context.Context, func(blockActionPayload)) error {
	return nil
}

func TestBridgePostsApprovalLifecycle(t *testing.T) {
	client := &fakeSlackClient{channelID: "D123"}
	runtime := &fakeRuntime{}
	bridge := &Bridge{
		logger:              slog.New(slog.NewTextHandler(io.Discard, nil)),
		runtime:             runtime,
		agentChannels:       map[string]string{"code-pr": "slack-dm"},
		channels:            map[string]*slackChannel{"slack-dm": {name: "slack-dm", client: client}},
		statePath:           filepath.Join(t.TempDir(), "slack.json"),
		state:               emptySlackState(),
		seenApprovalHistory: map[string]struct{}{},
		seenEvents:          map[string]struct{}{},
		runMeta:             map[string]runContext{},
	}

	now := time.Now().UTC()
	snapshot := orchestrator.Snapshot{
		ActiveRuns: []domain.AgentRun{{
			ID:         "run-1",
			AgentName:  "coder",
			AgentType:  "code-pr",
			SourceName: "gitlab-platform",
			Issue: domain.Issue{
				Identifier: "APP-42",
				Title:      "Fix workflow header",
				URL:        "https://example.com/APP-42",
			},
		}},
		PendingApprovals: []orchestrator.ApprovalView{{
			RequestID:       "req-1",
			RunID:           "run-1",
			IssueIdentifier: "APP-42",
			AgentName:       "coder",
			ToolName:        "write_file",
			ApprovalPolicy:  "manual",
			RequestedAt:     now,
		}},
	}

	if err := bridge.reconcile(context.Background(), snapshot); err != nil {
		t.Fatalf("reconcile pending approval: %v", err)
	}
	if len(client.posts) != 2 {
		t.Fatalf("posts = %d, want 2", len(client.posts))
	}
	if client.posts[0].threadTS != "" {
		t.Fatalf("root thread ts = %q, want empty", client.posts[0].threadTS)
	}
	if client.posts[1].threadTS == "" {
		t.Fatal("approval reply thread ts is empty")
	}
	if _, ok := bridge.state.Approvals["req-1"]; !ok {
		t.Fatal("approval message ref was not persisted")
	}

	snapshot.PendingApprovals = nil
	snapshot.ApprovalHistory = []orchestrator.ApprovalHistoryEntry{{
		RequestID:       "req-1",
		RunID:           "run-1",
		IssueIdentifier: "APP-42",
		ToolName:        "write_file",
		Decision:        "approve",
		RequestedAt:     now,
		DecidedAt:       now.Add(time.Minute),
		Outcome:         "resolved",
	}}
	if err := bridge.reconcile(context.Background(), snapshot); err != nil {
		t.Fatalf("reconcile approval history: %v", err)
	}
	if len(client.updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(client.updates))
	}
	if _, ok := bridge.state.Approvals["req-1"]; ok {
		t.Fatal("approval message ref still present after resolution")
	}
}

func TestBridgeHandleStopAction(t *testing.T) {
	client := &fakeSlackClient{channelID: "D123"}
	runtime := &fakeRuntime{}
	bridge := &Bridge{
		logger:              slog.New(slog.NewTextHandler(io.Discard, nil)),
		runtime:             runtime,
		agentChannels:       map[string]string{"code-pr": "slack-dm"},
		channels:            map[string]*slackChannel{"slack-dm": {name: "slack-dm", client: client}},
		statePath:           filepath.Join(t.TempDir(), "slack.json"),
		state:               emptySlackState(),
		seenApprovalHistory: map[string]struct{}{},
		seenEvents:          map[string]struct{}{},
		runMeta:             map[string]runContext{},
	}

	bridge.handleAction(context.Background(), "slack-dm", blockActionPayload{
		Type: "block_actions",
		Channel: struct {
			ID string `json:"id"`
		}{ID: "D123"},
		Container: struct {
			MessageTS string `json:"message_ts"`
		}{MessageTS: "ts-root"},
		Actions: []struct {
			ActionID string `json:"action_id"`
			Value    string `json:"value"`
		}{
			{ActionID: "maestro_stop_run", Value: "run-9"},
		},
	})

	if len(runtime.stopRequests) != 1 || runtime.stopRequests[0] != "run-9:stopped from Slack" {
		t.Fatalf("stop requests = %+v, want stop from Slack", runtime.stopRequests)
	}
	if len(client.updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(client.updates))
	}
	if !strings.Contains(client.updates[0].text, "Workflow stop requested") {
		t.Fatalf("update text = %q, want workflow stop requested", client.updates[0].text)
	}
}

func TestSlackHTTPClientSocketModeEndToEndApproval(t *testing.T) {
	server := newFakeSlackServer(t)
	defer server.Close()

	runtime := &fakeRuntime{}
	client := &slackHTTPClient{
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		http:       server.Client(),
		dialer:     websocket.DefaultDialer,
		config:     slackChannelConfig{Mode: "dm", BotToken: "xoxb-test", AppToken: "xapp-test", UserID: "U123"},
		apiBaseURL: server.apiBaseURL(),
	}
	bridge := &Bridge{
		logger:              slog.New(slog.NewTextHandler(io.Discard, nil)),
		runtime:             runtime,
		agentChannels:       map[string]string{"code-pr": "slack-dm"},
		channels:            map[string]*slackChannel{"slack-dm": {name: "slack-dm", client: client}},
		statePath:           filepath.Join(t.TempDir(), "slack.json"),
		state:               emptySlackState(),
		seenApprovalHistory: map[string]struct{}{},
		seenEvents:          map[string]struct{}{},
		runMeta:             map[string]runContext{},
	}

	now := time.Now().UTC()
	snapshot := orchestrator.Snapshot{
		ActiveRuns: []domain.AgentRun{{
			ID:         "run-1",
			AgentName:  "coder",
			AgentType:  "code-pr",
			SourceName: "gitlab-platform",
			Issue: domain.Issue{
				Identifier: "APP-42",
				Title:      "Fix workflow header",
				URL:        "https://example.com/APP-42",
			},
		}},
		PendingApprovals: []orchestrator.ApprovalView{{
			RequestID:       "req-1",
			RunID:           "run-1",
			IssueIdentifier: "APP-42",
			AgentName:       "coder",
			ToolName:        "write_file",
			ApprovalPolicy:  "manual",
			RequestedAt:     now,
		}},
	}

	if err := bridge.reconcile(context.Background(), snapshot); err != nil {
		t.Fatalf("reconcile pending approval: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.RunSocketMode(ctx, func(payload blockActionPayload) {
			bridge.handleAction(context.Background(), "slack-dm", payload)
		})
	}()

	ref := bridge.state.Approvals["req-1"]
	server.sendAction(blockActionPayload{
		Type: "block_actions",
		Channel: struct {
			ID string `json:"id"`
		}{ID: ref.ChannelID},
		Container: struct {
			MessageTS string `json:"message_ts"`
		}{MessageTS: ref.MessageTS},
		Actions: []struct {
			ActionID string `json:"action_id"`
			Value    string `json:"value"`
		}{
			{ActionID: "maestro_approve", Value: "req-1"},
		},
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.mu.Lock()
		decisions := append([]string(nil), runtime.approvalDecisions...)
		runtime.mu.Unlock()
		if len(decisions) == 1 && decisions[0] == "req-1:approve" {
			cancel()
			<-done
			if !server.sawEnvelopeAck() {
				t.Fatal("socket mode envelope ack was not observed")
			}
			if !server.sawUpdateContaining("Approval approve") {
				t.Fatalf("chat.update calls = %+v, want approval update", server.updateTexts())
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	<-done
	t.Fatalf("approval decisions = %+v, want req-1:approve", runtime.approvalDecisions)
}

type fakeSlackServer struct {
	t          *testing.T
	httpServer *httptest.Server
	wsURL      string

	mu           sync.Mutex
	nextTS       int
	updatedTexts []string
	envelopeAck  bool
	conn         *websocket.Conn
}

func newFakeSlackServer(t *testing.T) *fakeSlackServer {
	t.Helper()

	server := &fakeSlackServer{t: t, nextTS: 1}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/conversations.open", server.handleConversationsOpen)
	mux.HandleFunc("/api/chat.postMessage", server.handlePostMessage)
	mux.HandleFunc("/api/chat.update", server.handleUpdateMessage)
	mux.HandleFunc("/api/apps.connections.open", server.handleConnectionsOpen)
	mux.HandleFunc("/socket", server.handleSocket)
	server.httpServer = httptest.NewServer(mux)
	server.wsURL = "ws" + strings.TrimPrefix(server.httpServer.URL, "http") + "/socket"
	return server
}

func (s *fakeSlackServer) Close() {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	s.httpServer.Close()
}

func (s *fakeSlackServer) apiBaseURL() string {
	return s.httpServer.URL + "/api"
}

func (s *fakeSlackServer) Client() *http.Client {
	return s.httpServer.Client()
}

func (s *fakeSlackServer) handleConversationsOpen(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"channel": map[string]any{"id": "D123"},
	})
}

func (s *fakeSlackServer) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	var request map[string]any
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		s.t.Fatalf("decode postMessage request: %v", err)
	}
	channelID, _ := request["channel"].(string)

	s.mu.Lock()
	ts := "ts-" + time.Now().Format("150405") + "-" + strings.TrimSpace(url.QueryEscape(time.Now().Format("15:04:05.000")))
	s.nextTS++
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"channel": map[string]any{"id": channelID},
		"ts":      ts,
	})
}

func (s *fakeSlackServer) handleUpdateMessage(w http.ResponseWriter, r *http.Request) {
	var request map[string]any
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		s.t.Fatalf("decode update request: %v", err)
	}
	text, _ := request["text"].(string)

	s.mu.Lock()
	s.updatedTexts = append(s.updatedTexts, text)
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *fakeSlackServer) handleConnectionsOpen(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":  true,
		"url": s.wsURL,
	})
}

func (s *fakeSlackServer) handleSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.t.Fatalf("upgrade socket: %v", err)
	}

	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()

	go func() {
		defer conn.Close()
		for {
			var ack map[string]any
			if err := conn.ReadJSON(&ack); err != nil {
				return
			}
			if ack["envelope_id"] != nil {
				s.mu.Lock()
				s.envelopeAck = true
				s.mu.Unlock()
			}
		}
	}()
}

func (s *fakeSlackServer) sendAction(payload blockActionPayload) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		conn := s.conn
		s.mu.Unlock()
		if conn != nil {
			envelope := map[string]any{
				"envelope_id": "env-1",
				"type":        "interactive",
				"payload":     payload,
			}
			if err := conn.WriteJSON(envelope); err != nil {
				s.t.Fatalf("write socket envelope: %v", err)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.t.Fatal("socket connection not established")
}

func (s *fakeSlackServer) sawEnvelopeAck() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.envelopeAck
}

func (s *fakeSlackServer) sawUpdateContaining(fragment string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, text := range s.updatedTexts {
		if strings.Contains(text, fragment) {
			return true
		}
	}
	return false
}

func (s *fakeSlackServer) updateTexts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.updatedTexts...)
}
