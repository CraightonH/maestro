package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tjohnson/maestro/internal/domain"
)

const defaultGraphQLEndpoint = "https://api.linear.app/graphql"
const defaultHTTPClientTimeout = 30 * time.Second

type Client struct {
	endpoint    string
	token       string
	httpClient  *http.Client
	rateLimitMu sync.RWMutex
	rateLimit   *domain.TrackerRateLimit
}

func NewClient(baseURL string, token string) (*Client, error) {
	endpoint, err := normalizeEndpoint(baseURL)
	if err != nil {
		return nil, err
	}

	return &Client{
		endpoint:   endpoint,
		token:      token,
		httpClient: &http.Client{Timeout: defaultHTTPClientTimeout},
	}, nil
}

func (c *Client) query(ctx context.Context, query string, variables map[string]any, dst any) error {
	body, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	c.recordRateLimit(resp.Header)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("linear graphql: unexpected status %d", resp.StatusCode)
	}

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("linear graphql: %s", envelope.Errors[0].Message)
	}
	if dst != nil {
		if err := json.Unmarshal(envelope.Data, dst); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) RateLimit() *domain.TrackerRateLimit {
	c.rateLimitMu.RLock()
	defer c.rateLimitMu.RUnlock()
	return domain.CloneTrackerRateLimit(c.rateLimit)
}

func (c *Client) recordRateLimit(headers http.Header) {
	limit := linearHeaderInt64(headers, "X-RateLimit-Endpoint-Requests-Limit", "X-RateLimit-Requests-Limit")
	remaining := linearHeaderInt64(headers, "X-RateLimit-Endpoint-Requests-Remaining", "X-RateLimit-Requests-Remaining")
	resetAt := linearHeaderEpochMillis(headers, "X-RateLimit-Endpoint-Requests-Reset", "X-RateLimit-Requests-Reset")
	if limit == nil && remaining == nil && resetAt.IsZero() {
		return
	}
	c.rateLimitMu.Lock()
	c.rateLimit = &domain.TrackerRateLimit{
		Limit:     limit,
		Remaining: remaining,
		ResetAt:   resetAt,
		UpdatedAt: time.Now(),
	}
	c.rateLimitMu.Unlock()
}

func linearHeaderInt64(headers http.Header, keys ...string) *int64 {
	for _, key := range keys {
		raw := strings.TrimSpace(headers.Get(key))
		if raw == "" {
			continue
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err == nil {
			return &value
		}
	}
	return nil
}

func linearHeaderEpochMillis(headers http.Header, keys ...string) time.Time {
	for _, key := range keys {
		raw := strings.TrimSpace(headers.Get(key))
		if raw == "" {
			continue
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err == nil {
			return time.UnixMilli(value).UTC()
		}
	}
	return time.Time{}
}

func normalizeEndpoint(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultGraphQLEndpoint, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/graphql"
	}
	return parsed.String(), nil
}
