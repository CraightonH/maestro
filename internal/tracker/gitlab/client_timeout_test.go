package gitlab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNewClientSetsDefaultTimeout(t *testing.T) {
	client, err := NewClient("https://gitlab.example.com", "token")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	if got, want := client.httpClient.Timeout, defaultHTTPClientTimeout; got != want {
		t.Fatalf("client timeout = %s, want %s", got, want)
	}
}

func TestClientRequestTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	client.httpClient.Timeout = 50 * time.Millisecond

	var payload []map[string]any
	_, err = client.getJSON(context.Background(), "/api/v4/projects/demo/issues", url.Values{}, &payload)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "Client.Timeout") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientRecordsRateLimitHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "600")
		w.Header().Set("X-RateLimit-Remaining", "590")
		w.Header().Set("X-RateLimit-Reset", "1710003600")
		w.Header().Set("Retry-After", "30")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	var payload []map[string]any
	if _, err := client.getJSON(context.Background(), "/api/v4/projects/demo/issues", url.Values{}, &payload); err != nil {
		t.Fatalf("get json: %v", err)
	}

	rateLimit := client.RateLimit()
	if rateLimit == nil {
		t.Fatal("rate limit = nil, want parsed headers")
	}
	if rateLimit.Limit == nil || *rateLimit.Limit != 600 {
		t.Fatalf("limit = %#v, want 600", rateLimit.Limit)
	}
	if rateLimit.Remaining == nil || *rateLimit.Remaining != 590 {
		t.Fatalf("remaining = %#v, want 590", rateLimit.Remaining)
	}
	if rateLimit.RetryAfterSeconds == nil || *rateLimit.RetryAfterSeconds != 30 {
		t.Fatalf("retry_after_seconds = %#v, want 30", rateLimit.RetryAfterSeconds)
	}
	if rateLimit.ResetAt.Unix() != 1710003600 {
		t.Fatalf("reset_at = %v, want unix 1710003600", rateLimit.ResetAt)
	}
}
