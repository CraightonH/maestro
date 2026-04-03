package linear

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/tjohnson/maestro/internal/config"
)

// testConnection returns a SourceConnection pointing at a local httptest server.
func testConnection(serverURL, token, project string) config.SourceConnection {
	u, _ := url.Parse(serverURL)
	return config.SourceConnection{
		Domain:   u.Host,
		Protocol: u.Scheme,
		Token:    token,
		Project:  project,
	}
}

func TestPollNormalizesIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"issues":{"nodes":[{"id":"issue-1","identifier":"TAN-83","title":"Linear issue","description":"Tracked in Linear","url":"https://linear.app/tan/issue/TAN-83","createdAt":"2026-03-15T22:39:16.516Z","updatedAt":"2026-03-15T22:39:16.516Z","labels":{"nodes":[{"name":"Agent:Ready"}]},"state":{"name":"Todo","type":"unstarted"},"assignee":{"name":"Operator","email":"operator@example.com"},"project":{"id":"project-1","name":"Maestro Testbed"},"team":{"id":"team-1","key":"TAN","name":"Example Team"}}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`))
	}))
	defer server.Close()

	adapter, err := NewAdapter(config.SourceConfig{
		Name:      "live-linear",
		Tracker:   "linear",
		Repo:      "https://gitlab.example.com/team/maestro-testbed.git",
		AgentType: "code-pr",
		Connection: testConnection(server.URL, "secret", "project-1"),
		Filter: config.FilterConfig{
			States: []string{"Todo"},
			Labels: []string{"agent:ready"},
		},
	})
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	issues, err := adapter.Poll(context.Background())
	if err != nil {
		t.Fatalf("poll issues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("issues len = %d", len(issues))
	}

	issue := issues[0]
	if issue.ID != "linear:issue-1" {
		t.Fatalf("id = %q", issue.ID)
	}
	if issue.Identifier != "TAN-83" {
		t.Fatalf("identifier = %q", issue.Identifier)
	}
	if issue.State != "todo" {
		t.Fatalf("state = %q", issue.State)
	}
	if issue.Meta["repo_url"] != "https://gitlab.example.com/team/maestro-testbed.git" {
		t.Fatalf("repo_url = %q", issue.Meta["repo_url"])
	}
	if !strings.EqualFold(issue.Assignee, "operator@example.com") {
		t.Fatalf("assignee = %q", issue.Assignee)
	}
}

func TestPollResolvesProjectNameToProjectID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		switch {
		case strings.Contains(body.Query, "projects(first: 1"):
			if got := body.Variables["name"]; got != "Maestro Testbed" {
				t.Fatalf("lookup name = %v, want Maestro Testbed", got)
			}
			_, _ = w.Write([]byte(`{"data":{"projects":{"nodes":[{"id":"project-1","name":"Maestro Testbed"}]}}}`))
		case strings.Contains(body.Query, "issues(first: 50"):
			if got := body.Variables["projectId"]; got != "project-1" {
				t.Fatalf("issues projectId = %v, want project-1", got)
			}
			_, _ = w.Write([]byte(`{"data":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`))
		default:
			t.Fatalf("unexpected query: %s", body.Query)
		}
	}))
	defer server.Close()

	adapter, err := NewAdapter(config.SourceConfig{
		Name:      "linear-name",
		Tracker:   "linear",
		AgentType: "code-pr",
		Connection: testConnection(server.URL, "secret", "Maestro Testbed"),
	})
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	if _, err := adapter.Poll(context.Background()); err != nil {
		t.Fatalf("poll issues: %v", err)
	}
}

func TestLifecycleOperationsUseGraphQLIDs(t *testing.T) {
	assertNoStringID := func(name string, query string, patterns ...string) {
		for _, pattern := range patterns {
			if strings.Contains(query, pattern) {
				t.Fatalf("%s should use GraphQL ID variables, found %q in %s", name, pattern, query)
			}
		}
	}

	assertNoStringID("issueLabelsQuery", issueLabelsQuery, "$teamId: String!")
}

func TestGetNormalizesBlockingRelations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"issue":{"id":"issue-1","identifier":"TAN-83","title":"Linear issue","description":"Tracked in Linear","url":"https://linear.app/tan/issue/TAN-83","createdAt":"2026-03-15T22:39:16.516Z","updatedAt":"2026-03-15T22:39:16.516Z","labels":{"nodes":[{"name":"Agent:Ready"}]},"state":{"name":"Todo","type":"unstarted"},"assignee":{"name":"Operator","email":"operator@example.com"},"project":{"id":"project-1","name":"Maestro Testbed"},"team":{"id":"team-1","key":"TAN","name":"Example Team"},"inverseRelations":{"nodes":[{"type":"blocks","issue":{"id":"issue-9","identifier":"TAN-9","title":"Dependency","url":"https://linear.app/tan/issue/TAN-9","state":{"name":"In Progress","type":"started"},"team":{"id":"team-1","key":"TAN","name":"Example Team"}}}]}}}}`))
	}))
	defer server.Close()

	adapter, err := NewAdapter(config.SourceConfig{
		Name:      "live-linear",
		Tracker:   "linear",
		AgentType: "code-pr",
		Connection: testConnection(server.URL, "secret", "project-1"),
	})
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	issue, err := adapter.Get(context.Background(), "linear:issue-1")
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if len(issue.Blockers) != 1 {
		t.Fatalf("blockers = %+v, want one blocker", issue.Blockers)
	}
	if got := issue.Blockers[0].Identifier; got != "TAN-9" {
		t.Fatalf("blocker identifier = %q, want TAN-9", got)
	}
	if got := issue.Blockers[0].Meta["state_type"]; got != "started" {
		t.Fatalf("blocker state_type = %q, want started", got)
	}
}

func TestLinearQueriesDoNotUseUnsupportedInverseRelationsFilter(t *testing.T) {
	if strings.Contains(issuesQuery, "inverseRelations(first: 50, filter:") {
		t.Fatalf("issuesQuery uses unsupported inverseRelations filter argument")
	}
	if strings.Contains(issueQuery, "inverseRelations(first: 50, filter:") {
		t.Fatalf("issueQuery uses unsupported inverseRelations filter argument")
	}
}
