package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tjohnson/maestro/internal/domain"
)

const defaultHTTPClientTimeout = 30 * time.Second

type Client struct {
	baseURL      *url.URL
	privateToken string
	httpClient   *http.Client
	rateLimitMu  sync.RWMutex
	rateLimit    *domain.TrackerRateLimit
}

func NewClient(baseURL string, privateToken string) (*Client, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, err
	}

	return &Client{
		baseURL:      parsed,
		privateToken: privateToken,
		httpClient:   &http.Client{Timeout: defaultHTTPClientTimeout},
	}, nil
}

func (c *Client) getJSON(ctx context.Context, apiPath string, query url.Values, dst any) (*http.Response, error) {
	return c.doJSON(ctx, http.MethodGet, apiPath, query, nil, dst)
}

func (c *Client) postForm(ctx context.Context, apiPath string, form url.Values, dst any) (*http.Response, error) {
	return c.doForm(ctx, http.MethodPost, apiPath, form, dst)
}

func (c *Client) putForm(ctx context.Context, apiPath string, form url.Values, dst any) (*http.Response, error) {
	return c.doForm(ctx, http.MethodPut, apiPath, form, dst)
}

type RequestError struct {
	StatusCode int
	URL        string
	Body       string
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("gitlab request %s: unexpected status %d", e.URL, e.StatusCode)
}

func (c *Client) doJSON(ctx context.Context, method string, apiPath string, query url.Values, body io.Reader, dst any) (*http.Response, error) {
	rawURL := strings.TrimRight(c.baseURL.String(), "/") + apiPath
	endpoint, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.privateToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	c.recordRateLimit(resp.Header)

	if resp.StatusCode >= 300 {
		rawBody, _ := io.ReadAll(resp.Body)
		return resp, &RequestError{
			StatusCode: resp.StatusCode,
			URL:        endpoint.String(),
			Body:       string(rawBody),
		}
	}

	if dst != nil {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return resp, err
		}
	}

	return resp, nil
}

func (c *Client) doForm(ctx context.Context, method string, apiPath string, form url.Values, dst any) (*http.Response, error) {
	rawURL := strings.TrimRight(c.baseURL.String(), "/") + apiPath
	endpoint, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.privateToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	c.recordRateLimit(resp.Header)

	if resp.StatusCode >= 300 {
		rawBody, _ := io.ReadAll(resp.Body)
		return resp, &RequestError{
			StatusCode: resp.StatusCode,
			URL:        endpoint.String(),
			Body:       string(rawBody),
		}
	}

	if dst != nil {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return resp, err
		}
	}

	return resp, nil
}

func (c *Client) RateLimit() *domain.TrackerRateLimit {
	c.rateLimitMu.RLock()
	defer c.rateLimitMu.RUnlock()
	return domain.CloneTrackerRateLimit(c.rateLimit)
}

func (c *Client) recordRateLimit(headers http.Header) {
	limit := headerInt64(headers, "RateLimit-Limit", "X-RateLimit-Limit")
	remaining := headerInt64(headers, "RateLimit-Remaining", "X-RateLimit-Remaining")
	resetAt := headerUnix(headers, "RateLimit-Reset", "X-RateLimit-Reset")
	retryAfter := headerInt64(headers, "Retry-After")
	if limit == nil && remaining == nil && resetAt.IsZero() && retryAfter == nil {
		return
	}
	c.rateLimitMu.Lock()
	c.rateLimit = &domain.TrackerRateLimit{
		Limit:             limit,
		Remaining:         remaining,
		ResetAt:           resetAt,
		RetryAfterSeconds: retryAfter,
		UpdatedAt:         time.Now(),
	}
	c.rateLimitMu.Unlock()
}

func headerInt64(headers http.Header, keys ...string) *int64 {
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

func headerUnix(headers http.Header, keys ...string) time.Time {
	for _, key := range keys {
		raw := strings.TrimSpace(headers.Get(key))
		if raw == "" {
			continue
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			continue
		}
		if value > 1_000_000_000_000 {
			return time.UnixMilli(value).UTC()
		}
		return time.Unix(value, 0).UTC()
	}
	return time.Time{}
}
