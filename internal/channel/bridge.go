package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/tjohnson/maestro/internal/config"
	"github.com/tjohnson/maestro/internal/orchestrator"
	"github.com/tjohnson/maestro/internal/redact"
)

const slackTickInterval = 2 * time.Second

type Runtime interface {
	Snapshot() orchestrator.Snapshot
	ResolveApproval(requestID string, decision string) error
	StopRun(runID string, reason string) error
}

type Bridge struct {
	logger  *slog.Logger
	runtime Runtime

	agentChannels map[string]string
	channels      map[string]*slackChannel
	statePath     string

	mu                  sync.Mutex
	state               slackBridgeState
	seenApprovalHistory map[string]struct{}
	seenEvents          map[string]struct{}
	runMeta             map[string]runContext
}

type runContext struct {
	RunID           string
	AgentType       string
	AgentName       string
	SourceName      string
	IssueIdentifier string
	IssueURL        string
	IssueTitle      string
}

type slackBridgeState struct {
	Threads   map[string]slackThreadRef  `json:"threads"`
	Approvals map[string]slackMessageRef `json:"approvals"`
}

type slackThreadRef struct {
	ChannelName string `json:"channel_name"`
	ChannelID   string `json:"channel_id"`
	ThreadTS    string `json:"thread_ts"`
}

type slackMessageRef struct {
	ChannelName string `json:"channel_name"`
	ChannelID   string `json:"channel_id"`
	MessageTS   string `json:"message_ts"`
	ThreadTS    string `json:"thread_ts"`
}

type slackChannel struct {
	name   string
	config slackChannelConfig
	client slackClient
}

type slackChannelConfig struct {
	Mode      string
	BotToken  string
	AppToken  string
	UserID    string
	ChannelID string
}

type slackClient interface {
	ResolveChannel(ctx context.Context) (string, error)
	PostMessage(ctx context.Context, channelID string, threadTS string, text string, blocks []any) (slackPostedMessage, error)
	UpdateMessage(ctx context.Context, channelID string, messageTS string, text string, blocks []any) error
	RunSocketMode(ctx context.Context, handler func(blockActionPayload)) error
}

type slackPostedMessage struct {
	ChannelID string
	MessageTS string
}

type slackHTTPClient struct {
	logger     *slog.Logger
	http       *http.Client
	dialer     *websocket.Dialer
	config     slackChannelConfig
	apiBaseURL string

	mu              sync.Mutex
	resolvedChannel string
}

type slackAPIResponse struct {
	OK      bool            `json:"ok"`
	Error   string          `json:"error"`
	Channel slackChannelRef `json:"channel"`
	TS      string          `json:"ts"`
	URL     string          `json:"url"`
}

type slackChannelObj struct {
	ID string `json:"id"`
}

type slackChannelRef struct {
	ID string
}

func (r *slackChannelRef) UnmarshalJSON(data []byte) error {
	var id string
	if err := json.Unmarshal(data, &id); err == nil {
		r.ID = id
		return nil
	}
	var channel slackChannelObj
	if err := json.Unmarshal(data, &channel); err != nil {
		return err
	}
	r.ID = channel.ID
	return nil
}

type socketEnvelope struct {
	EnvelopeID string          `json:"envelope_id"`
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload"`
}

type blockActionPayload struct {
	Type    string `json:"type"`
	Channel struct {
		ID string `json:"id"`
	} `json:"channel"`
	Container struct {
		MessageTS string `json:"message_ts"`
	} `json:"container"`
	Actions []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
}

func NewBridge(cfg *config.Config, logger *slog.Logger, runtime Runtime) (*Bridge, error) {
	agentChannels := map[string]string{}
	channelDefs := map[string]config.ChannelConfig{}
	for _, channel := range cfg.Channels {
		channelDefs[channel.Name] = channel
	}
	for _, agent := range cfg.AgentTypes {
		if strings.TrimSpace(agent.Communication) == "" {
			continue
		}
		agentChannels[agent.Name] = agent.Communication
	}
	if len(agentChannels) == 0 {
		return nil, nil
	}

	channels := map[string]*slackChannel{}
	for _, channelName := range agentChannels {
		channelDef, ok := channelDefs[channelName]
		if !ok || channelDef.Kind != "slack" {
			continue
		}
		channelCfg, err := loadSlackChannelConfig(channelDef)
		if err != nil {
			return nil, fmt.Errorf("load slack channel %q: %w", channelName, err)
		}
		channels[channelName] = &slackChannel{
			name:   channelName,
			config: channelCfg,
			client: &slackHTTPClient{
				logger:     logger,
				http:       &http.Client{Timeout: 15 * time.Second},
				dialer:     websocket.DefaultDialer,
				config:     channelCfg,
				apiBaseURL: "https://slack.com/api",
			},
		}
	}
	if len(channels) == 0 {
		return nil, nil
	}

	bridge := &Bridge{
		logger:              logger,
		runtime:             runtime,
		agentChannels:       agentChannels,
		channels:            channels,
		statePath:           filepath.Join(cfg.State.Dir, "slack.json"),
		state:               emptySlackState(),
		seenApprovalHistory: map[string]struct{}{},
		seenEvents:          map[string]struct{}{},
		runMeta:             map[string]runContext{},
	}
	if err := bridge.loadState(); err != nil {
		logger.Warn("load slack state failed", "path", bridge.statePath, "error", err)
	}
	bridge.seed(runtime.Snapshot())
	return bridge, nil
}

func (b *Bridge) Run(ctx context.Context) error {
	for _, channel := range b.channels {
		go func(channel *slackChannel) {
			if err := channel.client.RunSocketMode(ctx, func(payload blockActionPayload) {
				b.handleAction(context.Background(), channel.name, payload)
			}); err != nil && !errors.Is(err, context.Canceled) {
				b.logger.Warn("slack socket mode stopped", "channel", channel.name, "error", err)
			}
		}(channel)
	}

	ticker := time.NewTicker(slackTickInterval)
	defer ticker.Stop()

	for {
		if err := b.reconcile(ctx, b.runtime.Snapshot()); err != nil {
			b.logger.Warn("slack reconcile failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (b *Bridge) seed(snapshot orchestrator.Snapshot) {
	for requestID := range b.state.Approvals {
		// Existing pending approval messages should not be reposted on restart.
		_ = requestID
	}
	for _, entry := range snapshot.ApprovalHistory {
		b.seenApprovalHistory[historyKey(entry)] = struct{}{}
	}
	for _, event := range snapshot.RecentEvents {
		b.seenEvents[eventKey(event)] = struct{}{}
	}
	for _, run := range snapshot.ActiveRuns {
		b.runMeta[run.ID] = runContext{
			RunID:           run.ID,
			AgentType:       run.AgentType,
			AgentName:       run.AgentName,
			SourceName:      run.SourceName,
			IssueIdentifier: run.Issue.Identifier,
			IssueURL:        run.Issue.URL,
			IssueTitle:      run.Issue.Title,
		}
	}
}

func (b *Bridge) reconcile(ctx context.Context, snapshot orchestrator.Snapshot) error {
	for _, run := range snapshot.ActiveRuns {
		meta := runContext{
			RunID:           run.ID,
			AgentType:       run.AgentType,
			AgentName:       run.AgentName,
			SourceName:      run.SourceName,
			IssueIdentifier: run.Issue.Identifier,
			IssueURL:        run.Issue.URL,
			IssueTitle:      run.Issue.Title,
		}
		b.mu.Lock()
		b.runMeta[run.ID] = meta
		b.mu.Unlock()
		if _, err := b.ensureThread(ctx, meta); err != nil {
			b.logger.Warn("ensure slack thread failed", "run_id", run.ID, "error", err)
		}
	}

	for _, approval := range snapshot.PendingApprovals {
		b.mu.Lock()
		_, posted := b.state.Approvals[approval.RequestID]
		b.mu.Unlock()
		if posted {
			continue
		}
		if err := b.postApprovalRequest(ctx, approval); err != nil {
			b.logger.Warn("post slack approval failed", "request_id", approval.RequestID, "error", err)
		}
	}

	for _, entry := range snapshot.ApprovalHistory {
		key := historyKey(entry)
		b.mu.Lock()
		_, seen := b.seenApprovalHistory[key]
		b.mu.Unlock()
		if seen {
			continue
		}
		if err := b.applyApprovalHistory(ctx, entry); err != nil {
			b.logger.Warn("apply slack approval history failed", "request_id", entry.RequestID, "error", err)
		}
		b.mu.Lock()
		b.seenApprovalHistory[key] = struct{}{}
		b.mu.Unlock()
	}

	for i := len(snapshot.RecentEvents) - 1; i >= 0; i-- {
		event := snapshot.RecentEvents[i]
		key := eventKey(event)
		b.mu.Lock()
		_, seen := b.seenEvents[key]
		b.mu.Unlock()
		if seen || !isNotifiableEvent(event.Message) {
			continue
		}
		if err := b.postEvent(ctx, event); err != nil {
			b.logger.Warn("post slack event failed", "run_id", event.RunID, "error", err)
		}
		b.mu.Lock()
		b.seenEvents[key] = struct{}{}
		b.mu.Unlock()
	}

	return nil
}

func (b *Bridge) postApprovalRequest(ctx context.Context, approval orchestrator.ApprovalView) error {
	meta, ok := b.lookupRunContext(approval.RunID, approval.AgentName, approval.IssueIdentifier)
	if !ok {
		return nil
	}
	thread, err := b.ensureThread(ctx, meta)
	if err != nil {
		return err
	}
	channel := b.channels[b.agentChannels[meta.AgentType]]
	toolInput := clipSlackText(approval.ToolInput, 900)
	text := redact.String(fmt.Sprintf("Approval needed for %s on %s", approval.ToolName, approval.IssueIdentifier))
	blocks := []any{
		slackSection(fmt.Sprintf("*Approval needed*\n*Issue:* %s\n*Agent:* %s\n*Tool:* `%s`", slackIssueLink(meta.IssueURL, approval.IssueIdentifier), meta.AgentName, approval.ToolName)),
	}
	if strings.TrimSpace(toolInput) != "" {
		blocks = append(blocks, slackSection(fmt.Sprintf("```%s```", toolInput)))
	}
	if strings.TrimSpace(meta.IssueURL) != "" {
		blocks = append(blocks, slackActions(
			slackLinkButton("View issue", meta.IssueURL),
		))
	}
	blocks = append(blocks, slackActions(
		slackButton("Approve", "maestro_approve", approval.RequestID, "primary"),
		slackButton("Reject", "maestro_reject", approval.RequestID, "danger"),
	))

	posted, err := channel.client.PostMessage(ctx, thread.ChannelID, thread.ThreadTS, text, blocks)
	if err != nil {
		return err
	}

	b.mu.Lock()
	b.state.Approvals[approval.RequestID] = slackMessageRef{
		ChannelName: channel.name,
		ChannelID:   posted.ChannelID,
		MessageTS:   posted.MessageTS,
		ThreadTS:    thread.ThreadTS,
	}
	b.mu.Unlock()
	return b.saveState()
}

func (b *Bridge) applyApprovalHistory(ctx context.Context, entry orchestrator.ApprovalHistoryEntry) error {
	b.mu.Lock()
	messageRef, ok := b.state.Approvals[entry.RequestID]
	if ok {
		delete(b.state.Approvals, entry.RequestID)
	}
	b.mu.Unlock()

	if !ok {
		return b.saveState()
	}

	channel := b.channels[messageRef.ChannelName]
	if channel == nil {
		return b.saveState()
	}

	text := redact.String(fmt.Sprintf("Approval %s for %s", entry.Decision, entry.IssueIdentifier))
	blocks := []any{
		slackSection(fmt.Sprintf("*Approval %s*\n*Issue:* %s\n*Tool:* `%s`", entry.Decision, entry.IssueIdentifier, entry.ToolName)),
		slackContext(fmt.Sprintf("%s · %s", strings.ToUpper(entry.Outcome), entry.DecidedAt.Format(time.RFC3339))),
	}
	if err := channel.client.UpdateMessage(ctx, messageRef.ChannelID, messageRef.MessageTS, text, blocks); err != nil {
		return err
	}
	return b.saveState()
}

func (b *Bridge) postEvent(ctx context.Context, event orchestrator.Event) error {
	meta, ok := b.lookupRunContext(event.RunID, "", event.Issue)
	if !ok {
		return nil
	}
	thread, err := b.ensureThread(ctx, meta)
	if err != nil {
		return err
	}
	channel := b.channels[b.agentChannels[meta.AgentType]]
	if channel == nil {
		return nil
	}

	text := redact.String(event.Message)
	_, err = channel.client.PostMessage(ctx, thread.ChannelID, thread.ThreadTS, text, []any{
		slackSection(fmt.Sprintf("*%s*\n%s", strings.ToUpper(event.Level), text)),
	})
	return err
}

func (b *Bridge) handleAction(ctx context.Context, channelName string, payload blockActionPayload) {
	if payload.Type != "block_actions" || len(payload.Actions) == 0 {
		return
	}
	action := payload.Actions[0]
	decision := ""
	switch action.ActionID {
	case "maestro_approve":
		decision = "approve"
	case "maestro_reject":
		decision = "reject"
	case "maestro_stop_run":
		b.handleStopAction(ctx, channelName, payload, action.Value)
		return
	default:
		return
	}

	channel := b.channels[channelName]
	if channel == nil {
		return
	}

	text := ""
	blocks := []any{}
	if err := b.runtime.ResolveApproval(action.Value, decision); err != nil {
		text = redact.String(fmt.Sprintf("Approval %s failed: %v", decision, err))
		blocks = []any{slackSection(fmt.Sprintf("*Approval %s failed*\n%s", decision, text))}
	} else {
		text = fmt.Sprintf("Approval %s", decision)
		blocks = []any{
			slackSection(fmt.Sprintf("*Approval %s*", decision)),
			slackContext(fmt.Sprintf("Resolved at %s", time.Now().Format(time.RFC3339))),
		}
		b.mu.Lock()
		delete(b.state.Approvals, action.Value)
		b.mu.Unlock()
		_ = b.saveState()
	}
	_ = channel.client.UpdateMessage(ctx, payload.Channel.ID, payload.Container.MessageTS, text, blocks)
}

func (b *Bridge) handleStopAction(ctx context.Context, channelName string, payload blockActionPayload, runID string) {
	channel := b.channels[channelName]
	if channel == nil {
		return
	}

	text := ""
	blocks := []any{}
	if err := b.runtime.StopRun(runID, "stopped from Slack"); err != nil {
		text = redact.String(fmt.Sprintf("Stop failed: %v", err))
		blocks = []any{slackSection(fmt.Sprintf("*Stop failed*\n%s", text))}
	} else {
		text = "Workflow stop requested"
		blocks = []any{
			slackSection("*Workflow stop requested*"),
			slackContext(fmt.Sprintf("Run %s · %s", runID, time.Now().Format(time.RFC3339))),
		}
	}
	_ = channel.client.UpdateMessage(ctx, payload.Channel.ID, payload.Container.MessageTS, text, blocks)
}

func (b *Bridge) ensureThread(ctx context.Context, meta runContext) (slackThreadRef, error) {
	key := issueKey(meta.SourceName, meta.IssueIdentifier)
	b.mu.Lock()
	if ref, ok := b.state.Threads[key]; ok {
		b.mu.Unlock()
		return ref, nil
	}
	b.mu.Unlock()

	channelName := b.agentChannels[meta.AgentType]
	channel := b.channels[channelName]
	if channel == nil {
		return slackThreadRef{}, fmt.Errorf("no slack channel for agent type %q", meta.AgentType)
	}
	channelID, err := channel.client.ResolveChannel(ctx)
	if err != nil {
		return slackThreadRef{}, err
	}

	text := redact.String(fmt.Sprintf("%s is working %s", meta.AgentName, meta.IssueIdentifier))
	body := fmt.Sprintf("*%s*\n%s", meta.AgentName, slackIssueLink(meta.IssueURL, meta.IssueIdentifier))
	if strings.TrimSpace(meta.IssueTitle) != "" {
		body += fmt.Sprintf("\n%s", redact.String(meta.IssueTitle))
	}
	blocks := []any{
		slackSection(body),
		slackContext(fmt.Sprintf("Workflow %s", meta.SourceName)),
	}
	actions := make([]map[string]any, 0, 2)
	if strings.TrimSpace(meta.IssueURL) != "" {
		actions = append(actions, slackLinkButton("View issue", meta.IssueURL))
	}
	actions = append(actions, slackButton("Stop workflow", "maestro_stop_run", meta.RunID, "danger"))
	blocks = append(blocks, slackActions(actions...))

	posted, err := channel.client.PostMessage(ctx, channelID, "", text, blocks)
	if err != nil {
		return slackThreadRef{}, err
	}

	ref := slackThreadRef{
		ChannelName: channelName,
		ChannelID:   posted.ChannelID,
		ThreadTS:    posted.MessageTS,
	}
	b.mu.Lock()
	b.state.Threads[key] = ref
	b.mu.Unlock()
	return ref, b.saveState()
}

func (b *Bridge) lookupRunContext(runID string, agentName string, issueIdentifier string) (runContext, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if runID != "" {
		if meta, ok := b.runMeta[runID]; ok {
			return meta, true
		}
	}
	for _, meta := range b.runMeta {
		if agentName != "" && meta.AgentName != agentName {
			continue
		}
		if issueIdentifier != "" && meta.IssueIdentifier != issueIdentifier {
			continue
		}
		return meta, true
	}
	return runContext{}, false
}

func issueKey(sourceName string, issueIdentifier string) string {
	return strings.TrimSpace(sourceName) + "|" + strings.TrimSpace(issueIdentifier)
}

func historyKey(entry orchestrator.ApprovalHistoryEntry) string {
	return entry.RequestID + "|" + entry.DecidedAt.Format(time.RFC3339Nano) + "|" + entry.Decision
}

func eventKey(event orchestrator.Event) string {
	return event.Time.Format(time.RFC3339Nano) + "|" + event.RunID + "|" + event.Message
}

func isNotifiableEvent(message string) bool {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "run ") && strings.Contains(lower, " completed"):
		return true
	case strings.Contains(lower, "run ") && strings.Contains(lower, " failed"):
		return true
	case strings.Contains(lower, "retry ") && strings.Contains(lower, "scheduled"):
		return true
	case strings.Contains(lower, "stopped:"):
		return true
	default:
		return false
	}
}

func emptySlackState() slackBridgeState {
	return slackBridgeState{
		Threads:   map[string]slackThreadRef{},
		Approvals: map[string]slackMessageRef{},
	}
}

func (b *Bridge) loadState() error {
	raw, err := os.ReadFile(b.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			b.state = emptySlackState()
			return nil
		}
		return err
	}
	state := emptySlackState()
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}
	if state.Threads == nil {
		state.Threads = map[string]slackThreadRef{}
	}
	if state.Approvals == nil {
		state.Approvals = map[string]slackMessageRef{}
	}
	b.state = state
	return nil
}

func (b *Bridge) saveState() error {
	if err := os.MkdirAll(filepath.Dir(b.statePath), 0o755); err != nil {
		return err
	}
	b.mu.Lock()
	raw, err := json.MarshalIndent(b.state, "", "  ")
	b.mu.Unlock()
	if err != nil {
		return err
	}
	tempPath := b.statePath + ".tmp"
	if err := os.WriteFile(tempPath, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tempPath, b.statePath)
}

func loadSlackChannelConfig(channel config.ChannelConfig) (slackChannelConfig, error) {
	mode := channelConfigValue(channel.Config, "mode")
	if mode == "" {
		mode = "dm"
	}
	cfg := slackChannelConfig{
		Mode:      mode,
		BotToken:  os.Getenv(channelConfigValue(channel.Config, "token_env")),
		AppToken:  os.Getenv(channelConfigValue(channel.Config, "app_token_env")),
		UserID:    firstNonEmpty(channelConfigValue(channel.Config, "user_id"), os.Getenv(channelConfigValue(channel.Config, "user_id_env"))),
		ChannelID: firstNonEmpty(channelConfigValue(channel.Config, "channel_id"), os.Getenv(channelConfigValue(channel.Config, "channel_id_env"))),
	}
	if strings.TrimSpace(cfg.BotToken) == "" {
		return slackChannelConfig{}, fmt.Errorf("missing slack bot token")
	}
	if strings.TrimSpace(cfg.AppToken) == "" {
		return slackChannelConfig{}, fmt.Errorf("missing slack app token")
	}
	switch cfg.Mode {
	case "dm":
		if strings.TrimSpace(cfg.UserID) == "" {
			return slackChannelConfig{}, fmt.Errorf("slack dm mode requires user_id or user_id_env")
		}
	case "channel":
		if strings.TrimSpace(cfg.ChannelID) == "" {
			return slackChannelConfig{}, fmt.Errorf("slack channel mode requires channel_id or channel_id_env")
		}
	default:
		return slackChannelConfig{}, fmt.Errorf("unsupported slack mode %q", cfg.Mode)
	}
	return cfg, nil
}

func channelConfigValue(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func clipSlackText(value string, limit int) string {
	value = redact.String(strings.TrimSpace(value))
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func slackIssueLink(urlValue string, identifier string) string {
	if strings.TrimSpace(urlValue) == "" || strings.TrimSpace(identifier) == "" {
		return redact.String(identifier)
	}
	return fmt.Sprintf("<%s|%s>", urlValue, redact.String(identifier))
}

func slackSection(text string) map[string]any {
	return map[string]any{
		"type": "section",
		"text": map[string]any{
			"type": "mrkdwn",
			"text": redact.String(text),
		},
	}
}

func slackContext(text string) map[string]any {
	return map[string]any{
		"type": "context",
		"elements": []map[string]any{{
			"type": "mrkdwn",
			"text": redact.String(text),
		}},
	}
}

func slackActions(elements ...map[string]any) map[string]any {
	return map[string]any{
		"type":     "actions",
		"elements": elements,
	}
}

func slackButton(label string, actionID string, value string, style string) map[string]any {
	button := map[string]any{
		"type":      "button",
		"action_id": actionID,
		"text": map[string]any{
			"type":  "plain_text",
			"text":  label,
			"emoji": true,
		},
		"value": value,
	}
	if style != "" {
		button["style"] = style
	}
	return button
}

func slackLinkButton(label string, targetURL string) map[string]any {
	return map[string]any{
		"type": "button",
		"text": map[string]any{
			"type":  "plain_text",
			"text":  label,
			"emoji": true,
		},
		"url": redact.String(strings.TrimSpace(targetURL)),
	}
}

func (c *slackHTTPClient) ResolveChannel(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.resolvedChannel != "" {
		channelID := c.resolvedChannel
		c.mu.Unlock()
		return channelID, nil
	}
	c.mu.Unlock()

	if c.config.Mode == "channel" {
		c.mu.Lock()
		c.resolvedChannel = c.config.ChannelID
		channelID := c.resolvedChannel
		c.mu.Unlock()
		return channelID, nil
	}

	form := url.Values{}
	form.Set("users", c.config.UserID)
	var response slackAPIResponse
	if err := c.postForm(ctx, c.endpoint("conversations.open"), c.config.BotToken, form, &response); err != nil {
		return "", err
	}
	if !response.OK {
		return "", fmt.Errorf("conversations.open: %s", response.Error)
	}
	c.mu.Lock()
	c.resolvedChannel = response.Channel.ID
	channelID := c.resolvedChannel
	c.mu.Unlock()
	return channelID, nil
}

func (c *slackHTTPClient) PostMessage(ctx context.Context, channelID string, threadTS string, text string, blocks []any) (slackPostedMessage, error) {
	request := map[string]any{
		"channel": channelID,
		"text":    redact.String(text),
	}
	if threadTS != "" {
		request["thread_ts"] = threadTS
	}
	if len(blocks) > 0 {
		request["blocks"] = blocks
	}
	var response slackAPIResponse
	if err := c.postJSON(ctx, c.endpoint("chat.postMessage"), c.config.BotToken, request, &response); err != nil {
		return slackPostedMessage{}, err
	}
	if !response.OK {
		return slackPostedMessage{}, fmt.Errorf("chat.postMessage: %s", response.Error)
	}
	return slackPostedMessage{ChannelID: response.Channel.ID, MessageTS: response.TS}, nil
}

func (c *slackHTTPClient) UpdateMessage(ctx context.Context, channelID string, messageTS string, text string, blocks []any) error {
	request := map[string]any{
		"channel": channelID,
		"ts":      messageTS,
		"text":    redact.String(text),
	}
	if len(blocks) > 0 {
		request["blocks"] = blocks
	}
	var response slackAPIResponse
	if err := c.postJSON(ctx, c.endpoint("chat.update"), c.config.BotToken, request, &response); err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("chat.update: %s", response.Error)
	}
	return nil
}

func (c *slackHTTPClient) RunSocketMode(ctx context.Context, handler func(blockActionPayload)) error {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
		socketURL, err := c.connectionURL(ctx)
		if err != nil {
			c.logger.Warn("open slack socket url failed", "error", err)
			if !sleepContext(ctx, backoff) {
				return nil
			}
			backoff = minDuration(backoff*2, 30*time.Second)
			continue
		}

		conn, _, err := c.dialer.DialContext(ctx, socketURL, nil)
		if err != nil {
			c.logger.Warn("dial slack socket failed", "error", err)
			if !sleepContext(ctx, backoff) {
				return nil
			}
			backoff = minDuration(backoff*2, 30*time.Second)
			continue
		}
		backoff = time.Second
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				_ = conn.Close()
			case <-done:
			}
		}()
		err = c.consumeSocket(ctx, conn, handler)
		close(done)
		_ = conn.Close()
		if err == nil || errors.Is(err, context.Canceled) {
			return nil
		}
		c.logger.Warn("slack socket disconnected", "error", err)
		if !sleepContext(ctx, backoff) {
			return nil
		}
		backoff = minDuration(backoff*2, 30*time.Second)
	}
}

func (c *slackHTTPClient) connectionURL(ctx context.Context) (string, error) {
	var response slackAPIResponse
	if err := c.postForm(ctx, c.endpoint("apps.connections.open"), c.config.AppToken, url.Values{}, &response); err != nil {
		return "", err
	}
	if !response.OK {
		return "", fmt.Errorf("apps.connections.open: %s", response.Error)
	}
	return response.URL, nil
}

func (c *slackHTTPClient) consumeSocket(ctx context.Context, conn *websocket.Conn, handler func(blockActionPayload)) error {
	_ = conn.SetReadDeadline(time.Time{})
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var envelope socketEnvelope
		if err := json.Unmarshal(payload, &envelope); err != nil {
			continue
		}
		if envelope.EnvelopeID != "" {
			if err := conn.WriteJSON(map[string]string{"envelope_id": envelope.EnvelopeID}); err != nil {
				return err
			}
		}
		if envelope.Type == "disconnect" {
			return fmt.Errorf("slack requested disconnect")
		}
		if len(envelope.Payload) == 0 {
			continue
		}
		var action blockActionPayload
		if err := json.Unmarshal(envelope.Payload, &action); err != nil {
			continue
		}
		if action.Type == "block_actions" {
			handler(action)
		}
	}
}

func (c *slackHTTPClient) postJSON(ctx context.Context, endpoint string, token string, requestBody any, target any) error {
	raw, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	return c.do(request, target)
}

func (c *slackHTTPClient) postForm(ctx context.Context, endpoint string, token string, values url.Values, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(request, target)
}

func (c *slackHTTPClient) do(request *http.Request, target any) error {
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return fmt.Errorf("slack api %s: %s", request.URL.Path, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(response.Body).Decode(target)
}

func (c *slackHTTPClient) endpoint(method string) string {
	base := strings.TrimRight(c.apiBaseURL, "/")
	if base == "" {
		base = "https://slack.com/api"
	}
	return base + "/" + strings.TrimLeft(method, "/")
}

func minDuration(left time.Duration, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func sleepContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
