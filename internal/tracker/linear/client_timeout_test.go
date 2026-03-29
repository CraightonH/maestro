package linear

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClientSetsDefaultTimeout(t *testing.T) {
	client, err := NewClient("", "token")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	if got, want := client.httpClient.Timeout, defaultHTTPClientTimeout; got != want {
		t.Fatalf("client timeout = %s, want %s", got, want)
	}
}

func TestClientQueryTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	client.httpClient.Timeout = 50 * time.Millisecond

	err = client.query(context.Background(), "query { viewer { id } }", nil, &map[string]any{})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "Client.Timeout") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientRecordsRateLimitHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Requests-Limit", "5000")
		w.Header().Set("X-RateLimit-Requests-Remaining", "4988")
		w.Header().Set("X-RateLimit-Requests-Reset", "1710003600000")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	if err := client.query(context.Background(), "query { viewer { id } }", nil, &map[string]any{}); err != nil {
		t.Fatalf("query: %v", err)
	}

	rateLimit := client.RateLimit()
	if rateLimit == nil {
		t.Fatal("rate limit = nil, want parsed headers")
	}
	if rateLimit.Limit == nil || *rateLimit.Limit != 5000 {
		t.Fatalf("limit = %#v, want 5000", rateLimit.Limit)
	}
	if rateLimit.Remaining == nil || *rateLimit.Remaining != 4988 {
		t.Fatalf("remaining = %#v, want 4988", rateLimit.Remaining)
	}
	if rateLimit.ResetAt.UnixMilli() != 1710003600000 {
		t.Fatalf("reset_at = %v, want unix milli 1710003600000", rateLimit.ResetAt)
	}
}
