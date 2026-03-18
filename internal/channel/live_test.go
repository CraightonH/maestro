package channel

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/tjohnson/maestro/internal/testutil"
)

func TestLiveSlackClientDMThreadLifecycle(t *testing.T) {
	testutil.RequireFlag(t, "MAESTRO_TEST_LIVE_SLACK")
	env := testutil.RequireEnv(
		t,
		"MAESTRO_TEST_SLACK_BOT_TOKEN",
		"MAESTRO_TEST_SLACK_APP_TOKEN",
		"MAESTRO_TEST_SLACK_USER_ID",
	)

	client := &slackHTTPClient{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		http:   &http.Client{Timeout: 15 * time.Second},
		dialer: websocket.DefaultDialer,
		config: slackChannelConfig{
			Mode:     "dm",
			BotToken: env["MAESTRO_TEST_SLACK_BOT_TOKEN"],
			AppToken: env["MAESTRO_TEST_SLACK_APP_TOKEN"],
			UserID:   env["MAESTRO_TEST_SLACK_USER_ID"],
		},
		apiBaseURL: "https://slack.com/api",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	channelID, err := client.ResolveChannel(ctx)
	if err != nil {
		t.Fatalf("resolve channel: %v", err)
	}
	if strings.TrimSpace(channelID) == "" {
		t.Fatal("resolved channel id is empty")
	}

	connectionURL, err := client.connectionURL(ctx)
	if err != nil {
		t.Fatalf("open socket mode connection url: %v", err)
	}
	if !strings.HasPrefix(connectionURL, "wss://") {
		t.Fatalf("connection url = %q, want wss://", connectionURL)
	}

	posted, err := client.PostMessage(ctx, channelID, "", "Maestro Slack live test: root message", []any{
		slackSection("*Maestro Slack live test*\nPosting a disposable root message"),
	})
	if err != nil {
		t.Fatalf("post root message: %v", err)
	}
	if strings.TrimSpace(posted.MessageTS) == "" {
		t.Fatal("root message timestamp is empty")
	}

	reply, err := client.PostMessage(ctx, channelID, posted.MessageTS, "Maestro Slack live test: reply message", []any{
		slackSection("Disposable threaded reply"),
	})
	if err != nil {
		t.Fatalf("post thread reply: %v", err)
	}
	if strings.TrimSpace(reply.MessageTS) == "" {
		t.Fatal("reply message timestamp is empty")
	}

	if err := client.UpdateMessage(ctx, channelID, reply.MessageTS, "Maestro Slack live test: reply updated", []any{
		slackSection("Disposable threaded reply (updated)"),
	}); err != nil {
		t.Fatalf("update thread reply: %v", err)
	}
}
