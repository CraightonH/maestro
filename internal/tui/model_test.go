package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/orchestrator"
)

type staticSnapshotProvider struct {
	snapshot orchestrator.Snapshot
}

func (s staticSnapshotProvider) Snapshot() orchestrator.Snapshot {
	return s.snapshot
}

func (s staticSnapshotProvider) ResolveApproval(requestID string, decision string) error {
	return nil
}

func (s staticSnapshotProvider) ResolveMessage(requestID string, reply string, resolvedVia string) error {
	return nil
}

func (s staticSnapshotProvider) RequestForcePoll(sourceName string) (orchestrator.ForcePollResult, error) {
	return orchestrator.ForcePollResult{
		Scope: "source",
		Results: []orchestrator.ForcePollSourceResult{{
			Source: sourceName,
			Status: orchestrator.ForcePollCompleted,
		}},
	}, nil
}

type forcePollSnapshotProvider struct {
	snapshot orchestrator.Snapshot
	polls    []string
}

func (s *forcePollSnapshotProvider) Snapshot() orchestrator.Snapshot {
	return s.snapshot
}

func (s *forcePollSnapshotProvider) ResolveApproval(requestID string, decision string) error {
	return nil
}

func (s *forcePollSnapshotProvider) ResolveMessage(requestID string, reply string, resolvedVia string) error {
	return nil
}

func (s *forcePollSnapshotProvider) RequestForcePoll(sourceName string) (orchestrator.ForcePollResult, error) {
	s.polls = append(s.polls, sourceName)
	scope := "all"
	if sourceName != "" {
		scope = "source"
	}
	return orchestrator.ForcePollResult{
		Scope: scope,
		Results: []orchestrator.ForcePollSourceResult{{
			Source: map[bool]string{true: sourceName, false: "all"}[sourceName != ""],
			Status: orchestrator.ForcePollCompleted,
		}},
	}, nil
}

// stripANSI removes ANSI escape codes from a string for test assertions.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func viewContains(view, want string) bool {
	return strings.Contains(stripANSI(view), want)
}

func int64Ptr(value int64) *int64 {
	return &value
}

func float64Ptr(value float64) *float64 {
	return &value
}

func TestViewGroupsSourcesAndShowsTags(t *testing.T) {
	snapshot := orchestrator.Snapshot{
		SourceName: "epic-a, project-a, linear-a",
		SourceSummaries: []orchestrator.SourceSummary{
			{Name: "project-a", DisplayGroup: "Delivery", Tracker: "gitlab", Tags: []string{"backend", "prod"}, LastPollAt: time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)},
			{Name: "epic-a", DisplayGroup: "Planning", Tracker: "gitlab-epic", Tags: []string{"platform"}, LastPollAt: time.Date(2026, 3, 16, 10, 0, 1, 0, time.UTC)},
			{Name: "linear-a", Tracker: "linear", Tags: []string{"triage"}, LastPollAt: time.Date(2026, 3, 16, 10, 0, 2, 0, time.UTC)},
		},
		RecentEvents: []orchestrator.Event{
			{Level: "INFO", Time: time.Date(2026, 3, 16, 10, 0, 3, 0, time.UTC), Source: "project-a", Message: "polled 1 candidate issues from project-a"},
		},
	}
	model := NewModel(staticSnapshotProvider{snapshot: snapshot})

	view := model.View()
	for _, want := range []string{
		"Sources",
		"project-a",
		"gitlab",
		"epic-a",
		"gitlab-epic",
		"linear-a",
		"linear",
		"Source Detail",
		"project-a  [OK]  gitlab",
		"tags:backend,prod",
		"Events:",
		"polled 1 candidate issues from project-a",
	} {
		if !viewContains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, stripANSI(view))
		}
	}
}

func TestViewShowsSourceActiveOccupancy(t *testing.T) {
	snapshot := orchestrator.Snapshot{
		SourceName: "project-a",
		SourceSummaries: []orchestrator.SourceSummary{
			{
				Name:                   "project-a",
				Tracker:                "gitlab",
				ActiveRunCount:         2,
				MaxActiveRuns:          3,
				AgentMaxConcurrent:     4,
				GlobalMaxConcurrent:    10,
				EffectiveMaxConcurrent: 3,
				Metrics: domain.RunMetrics{
					TokensIn:    int64Ptr(120),
					TokensOut:   int64Ptr(30),
					TotalTokens: int64Ptr(150),
				},
				LastPollAt:             time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC),
			},
		},
	}
	model := NewModel(staticSnapshotProvider{snapshot: snapshot})

	view := stripANSI(model.View())
	if !strings.Contains(view, "2/3 active") {
		t.Fatalf("view missing source occupancy: %s", view)
	}
	if !strings.Contains(view, "source 3 · agent 4 · global 10 · effective 3") {
		t.Fatalf("view missing concurrency hierarchy: %s", view)
	}
	if !strings.Contains(view, "Metrics: 120 in  30 out  150 total") {
		t.Fatalf("view missing source metrics: %s", view)
	}
}

func TestViewShowsActiveRunsInSourceDetail(t *testing.T) {
	snapshot := orchestrator.Snapshot{
		SourceName: "project-a",
		SourceSummaries: []orchestrator.SourceSummary{
			{
				Name:           "project-a",
				Tracker:        "gitlab",
				ActiveRunCount: 2,
				MaxActiveRuns:  3,
			},
		},
		ActiveRuns: []domain.AgentRun{
			{
				ID:             "run-1",
				SourceName:     "project-a",
				Issue:          domain.Issue{Identifier: "team/project#1"},
				Status:         domain.RunStatusActive,
				CurrentTurn:    1,
				MaxTurns:       3,
				LastActivityAt: time.Now().Add(-10 * time.Second),
			},
			{
				ID:             "run-2",
				SourceName:     "project-a",
				Issue:          domain.Issue{Identifier: "team/project#2"},
				Status:         domain.RunStatusAwaiting,
				CurrentTurn:    2,
				MaxTurns:       4,
				LastActivityAt: time.Now().Add(-20 * time.Second),
			},
		},
	}
	model := NewModel(staticSnapshotProvider{snapshot: snapshot})

	view := stripANSI(model.View())
	for _, want := range []string{
		"Active runs:",
		"team/project#1",
		"team/project#2",
		"turn 1/3",
		"turn 2/4",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestViewAppliesGroupFilterAndSearch(t *testing.T) {
	snapshot := orchestrator.Snapshot{
		SourceName: "epic-a, project-a, linear-a",
		SourceSummaries: []orchestrator.SourceSummary{
			{Name: "project-a", DisplayGroup: "Delivery", Tracker: "gitlab", Tags: []string{"backend"}},
			{Name: "epic-a", DisplayGroup: "Planning", Tracker: "gitlab-epic", Tags: []string{"platform"}},
		},
		ActiveRuns: []domain.AgentRun{
			{AgentName: "coder", SourceName: "project-a", Issue: domain.Issue{Identifier: "team/project#1", Title: "Backend work"}},
			{AgentName: "triage", SourceName: "epic-a", Issue: domain.Issue{Identifier: "team/project#2", Title: "Platform work"}},
		},
	}
	model := Model{
		service:     staticSnapshotProvider{snapshot: snapshot},
		snapshot:    snapshot,
		groupFilter: "Planning",
		searchQuery: "platform",
		focus:       focusSources,
		runSort:     runSortStallRisk,
		quickFilter: quickFilterAll,
		width:       80,
		height:      24,
	}

	view := model.View()
	plain := stripANSI(view)
	if strings.Contains(plain, "● project-a") {
		t.Fatalf("expected filtered view to hide project-a source row:\n%s", plain)
	}
	if !viewContains(view, "epic-a") {
		t.Fatalf("expected filtered view to show epic-a:\n%s", plain)
	}
	if !viewContains(view, "Filters: group=Planning search=platform") {
		t.Fatalf("expected filter summary in view:\n%s", plain)
	}
}

func TestUpdateCyclesGroupFilterAndSearchMode(t *testing.T) {
	snapshot := orchestrator.Snapshot{
		SourceSummaries: []orchestrator.SourceSummary{
			{Name: "project-a", DisplayGroup: "Delivery", Tracker: "gitlab"},
			{Name: "epic-a", DisplayGroup: "Planning", Tracker: "gitlab-epic"},
		},
	}
	model := NewModel(staticSnapshotProvider{snapshot: snapshot})
	if model.focus != focusSources {
		t.Fatalf("initial focus = %q", model.focus)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	gotModel := updated.(Model)
	if gotModel.groupFilter != "Delivery" {
		t.Fatalf("group filter after first cycle = %q", gotModel.groupFilter)
	}

	updated, _ = gotModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	gotModel = updated.(Model)
	if !gotModel.searchMode {
		t.Fatal("expected search mode to be enabled")
	}

	updated, _ = gotModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	gotModel = updated.(Model)
	if gotModel.searchQuery != "a" {
		t.Fatalf("search query = %q", gotModel.searchQuery)
	}
}

func TestViewShowsSelectedRunDetails(t *testing.T) {
	startedAt := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	lastActivity := startedAt.Add(2 * time.Minute)
	snapshot := orchestrator.Snapshot{
		SourceSummaries: []orchestrator.SourceSummary{
			{
				Name:         "project-a",
				DisplayGroup: "Delivery",
				Tracker:      "gitlab",
				Execution: &orchestrator.ExecutionSummary{
					Mode:              "docker",
					Image:             "maestro-agent:latest",
					Network:           "bridge",
					NetworkPolicyMode: "allowlist",
					NetworkAllow:      []string{"api.openai.com", "*.anthropic.com"},
					CPUs:              2,
					Memory:            "4g",
					PIDsLimit:         256,
					AuthSource:        "env",
					SecurityPreset:    "default",
					EnvCount:          2,
					SecretMountCount:  1,
					ToolMountCount:    1,
				},
			},
			{Name: "project-b", DisplayGroup: "Delivery", Tracker: "gitlab"},
		},
		ActiveRuns: []domain.AgentRun{
			{
				ID:             "run-1",
				AgentName:      "coder",
				AgentType:      "code-pr",
				HarnessKind:    "claude-code",
				SourceName:     "project-a",
				Status:         domain.RunStatusActive,
				CurrentTurn:    2,
				MaxTurns:       4,
				Attempt:        1,
				ApprovalPolicy: "auto",
				ApprovalState:  domain.ApprovalStateApproved,
				WorkspacePath:  "/tmp/workspace-a",
				StartedAt:      startedAt,
				LastActivityAt: lastActivity,
				Metrics: domain.RunMetrics{
					TokensIn:                  int64Ptr(7398146),
					TokensOut:                 int64Ptr(28858),
					TotalTokens:               int64Ptr(7427004),
					DurationMS:                int64Ptr(20000),
					ThroughputTokensPerSecond: float64Ptr(986.4),
					UpdatedAt:                 startedAt.Add(3 * time.Minute),
				},
				Issue: domain.Issue{
					Identifier: "team/project#1",
					Title:      "Backend work",
					URL:        "https://gitlab.example.com/team/project/-/issues/1",
				},
			},
			{
				ID:             "run-2",
				AgentName:      "triage",
				AgentType:      "triage",
				HarnessKind:    "claude-code",
				SourceName:     "project-b",
				Status:         domain.RunStatusAwaiting,
				ApprovalPolicy: "manual",
				ApprovalState:  domain.ApprovalStateAwaiting,
				StartedAt:      startedAt.Add(5 * time.Minute),
				Issue: domain.Issue{
					Identifier: "team/project#2",
					Title:      "Triage work",
				},
			},
		},
		RecentEvents: []orchestrator.Event{
			{Level: "INFO", Time: startedAt.Add(30 * time.Second), Source: "project-a", RunID: "run-1", Issue: "team/project#1", Message: "agent coder started for team/project#1"},
			{Level: "WARN", Time: startedAt.Add(45 * time.Second), Source: "project-b", RunID: "run-2", Issue: "team/project#2", Message: "approval requested for run-2 (Write)"},
		},
	}
	model := NewModel(staticSnapshotProvider{snapshot: snapshot})
	model.focus = focusRuns
	model.runSort = runSortOldest

	view := model.View()
	for _, want := range []string{
		"Active Runs",
		"coder",
		"team/project#1",
		"Run Detail",
		"Run: run-1",
		"Agent: coder (code-pr)  Harness: claude-code  Source: project-a  Execution:",
		"docker · image=maestro-agent:latest",
		"Turn: 2/4",
		"Last output:",
		"Last metrics:",
		"policy=allowlist",
		"allow=api.openai.com,*.anthropic.com",
		"auth=env",
		"security=default",
		"env=2",
		"secrets=1",
		"tools=1",
		"Metrics: 7,398,146 in  28,858 out  7,427,004 total  20s  986.4 tok/s",
		"Workspace: /tmp/workspace-a",
		"agent coder started for",
	} {
		if !viewContains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, stripANSI(view))
		}
	}
}

func TestUpdateTabSwitchesFocusAndRunSelection(t *testing.T) {
	snapshot := orchestrator.Snapshot{
		SourceSummaries: []orchestrator.SourceSummary{
			{Name: "project-a", DisplayGroup: "Delivery", Tracker: "gitlab"},
			{Name: "project-b", DisplayGroup: "Delivery", Tracker: "gitlab"},
			{Name: "project-c", DisplayGroup: "Delivery", Tracker: "gitlab"},
		},
		ActiveRuns: []domain.AgentRun{
			{ID: "run-1", AgentName: "coder", SourceName: "project-a", Issue: domain.Issue{Identifier: "team/project#1"}},
			{ID: "run-2", AgentName: "triage", SourceName: "project-b", Issue: domain.Issue{Identifier: "team/project#2"}},
		},
		Retries: []orchestrator.RetryView{
			{IssueIdentifier: "team/project#3", SourceName: "project-c", Attempt: 2, DueAt: time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)},
		},
		PendingApprovals: []orchestrator.ApprovalView{
			{RequestID: "approval-1", IssueIdentifier: "team/project#2", ToolName: "Write", ApprovalPolicy: "manual"},
		},
	}
	model := NewModel(staticSnapshotProvider{snapshot: snapshot})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	gotModel := updated.(Model)
	if gotModel.selectedSource != 1 {
		t.Fatalf("selected source = %d", gotModel.selectedSource)
	}

	updated, _ = gotModel.Update(tea.KeyMsg{Type: tea.KeyTab})
	gotModel = updated.(Model)
	if gotModel.focus != focusRuns {
		t.Fatalf("focus = %q", gotModel.focus)
	}

	updated, _ = gotModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	gotModel = updated.(Model)
	if gotModel.selectedRun != 1 {
		t.Fatalf("selected run = %d", gotModel.selectedRun)
	}

	updated, _ = gotModel.Update(tea.KeyMsg{Type: tea.KeyTab})
	gotModel = updated.(Model)
	if gotModel.focus != focusRetries {
		t.Fatalf("focus = %q", gotModel.focus)
	}

	updated, _ = gotModel.Update(tea.KeyMsg{Type: tea.KeyTab})
	gotModel = updated.(Model)
	if gotModel.focus != focusApprovals {
		t.Fatalf("focus = %q", gotModel.focus)
	}
}

func TestHeaderShowsInstanceMetrics(t *testing.T) {
	snapshot := orchestrator.Snapshot{
		LastPollAt: time.Date(2026, 3, 29, 22, 0, 0, 0, time.UTC),
		SourceSummaries: []orchestrator.SourceSummary{
			{Name: "project-a", Tracker: "gitlab"},
		},
		InstanceMetrics: domain.RunMetrics{
			TokensIn:    int64Ptr(7398146),
			TokensOut:   int64Ptr(28858),
			TotalTokens: int64Ptr(7427004),
		},
	}
	model := NewModel(staticSnapshotProvider{snapshot: snapshot})

	view := model.View()
	if !viewContains(view, "Metrics: 7,398,146 in  28,858 out  7,427,004 total") {
		t.Fatalf("view missing header metrics:\n%s", stripANSI(view))
	}
}

func TestUpdateRequestsForcePollForSelectedSource(t *testing.T) {
	provider := &forcePollSnapshotProvider{
		snapshot: orchestrator.Snapshot{
			SourceSummaries: []orchestrator.SourceSummary{
				{Name: "project-a", Tracker: "gitlab"},
				{Name: "project-b", Tracker: "gitlab"},
			},
		},
	}
	model := NewModel(provider)
	model.selectedSource = 1

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	gotModel := updated.(Model)

	if got := strings.Join(provider.polls, ","); got != "project-b" {
		t.Fatalf("force poll requests = %q, want project-b", got)
	}
	if gotModel.notice != "" {
		t.Fatalf("notice = %q", gotModel.notice)
	}
}

func TestFooterShowsForcePollKeyHints(t *testing.T) {
	model := NewModel(staticSnapshotProvider{})
	plain := stripANSI(model.renderFooter())
	for _, want := range []string{"p poll", "P poll all"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("footer missing %q:\n%s", want, plain)
		}
	}
}

func TestViewShowsRetriesPane(t *testing.T) {
	dueAt := time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)
	snapshot := orchestrator.Snapshot{
		SourceSummaries: []orchestrator.SourceSummary{
			{Name: "project-a", DisplayGroup: "Delivery", Tracker: "gitlab", RetryCount: 1},
		},
		Retries: []orchestrator.RetryView{
			{
				IssueID:         "issue-1",
				IssueIdentifier: "team/project#1",
				SourceName:      "project-a",
				Attempt:         2,
				DueAt:           dueAt,
				Error:           "network timeout",
			},
		},
	}
	model := NewModel(staticSnapshotProvider{snapshot: snapshot})
	model.focus = focusRetries

	view := model.View()
	for _, want := range []string{
		"Retry Queue",
		"team/project#1",
		"project-a",
		"2",
		"Retry Detail",
		"Source: project-a",
		"Issue: team/project#1",
		"Error: network timeout",
	} {
		if !viewContains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, stripANSI(view))
		}
	}
}

func TestUpdateCyclesRunSortMode(t *testing.T) {
	model := NewModel(staticSnapshotProvider{snapshot: orchestrator.Snapshot{
		SourceSummaries: []orchestrator.SourceSummary{{Name: "project-a", Tracker: "gitlab"}},
		ActiveRuns: []domain.AgentRun{
			{ID: "run-1", AgentName: "coder", SourceName: "project-a", Issue: domain.Issue{Identifier: "team/project#1"}},
		},
		Retries: []orchestrator.RetryView{
			{IssueIdentifier: "team/project#2", SourceName: "project-a", Attempt: 1, DueAt: time.Now()},
		},
	}})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	got := updated.(Model)
	if got.runSort != runSortApprovalFirst {
		t.Fatalf("run sort = %q", got.runSort)
	}
}

func TestUpdateTogglesQuickFilters(t *testing.T) {
	model := NewModel(staticSnapshotProvider{snapshot: orchestrator.Snapshot{
		SourceSummaries: []orchestrator.SourceSummary{{Name: "project-a", Tracker: "gitlab", PendingApprovals: 1}},
	}})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	got := updated.(Model)
	if got.quickFilter != quickFilterAttention {
		t.Fatalf("quick filter = %q", got.quickFilter)
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	got = updated.(Model)
	if got.quickFilter != quickFilterAll {
		t.Fatalf("quick filter = %q", got.quickFilter)
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	got = updated.(Model)
	if got.quickFilter != quickFilterAwaiting {
		t.Fatalf("quick filter = %q", got.quickFilter)
	}
}

func TestViewAppliesQuickAttentionFilter(t *testing.T) {
	snapshot := orchestrator.Snapshot{
		SourceSummaries: []orchestrator.SourceSummary{
			{Name: "project-a", DisplayGroup: "Delivery", Tracker: "gitlab", LastPollAt: time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)},
			{Name: "project-b", DisplayGroup: "Delivery", Tracker: "gitlab", RetryCount: 1, LastPollAt: time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)},
		},
		ActiveRuns: []domain.AgentRun{
			{ID: "run-1", AgentName: "coder", SourceName: "project-a", Issue: domain.Issue{Identifier: "team/project#1"}},
			{ID: "run-2", AgentName: "reviewer", SourceName: "project-b", Status: domain.RunStatusAwaiting, ApprovalState: domain.ApprovalStateAwaiting, Issue: domain.Issue{Identifier: "team/project#2"}},
		},
		Retries: []orchestrator.RetryView{
			{IssueIdentifier: "team/project#2", SourceName: "project-b", Attempt: 2, DueAt: time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)},
		},
	}
	model := NewModel(staticSnapshotProvider{snapshot: snapshot})
	model.quickFilter = quickFilterAttention
	model.focus = focusSources

	view := model.View()
	plain := stripANSI(view)
	// project-a should be hidden (OK health, no attention needed)
	// but project-b should appear (RETRY health)
	if !strings.Contains(plain, "project-b") {
		t.Fatalf("view missing project-b:\n%s", plain)
	}
	if !strings.Contains(plain, "team/project#2") {
		t.Fatalf("view missing team/project#2:\n%s", plain)
	}
	if !viewContains(view, "quick=attention") {
		t.Fatalf("view missing quick=attention:\n%s", plain)
	}
}

func TestViewAppliesQuickAwaitingFilter(t *testing.T) {
	snapshot := orchestrator.Snapshot{
		SourceSummaries: []orchestrator.SourceSummary{
			{Name: "project-a", Tracker: "gitlab"},
			{Name: "project-b", Tracker: "gitlab", PendingApprovals: 1},
		},
		ActiveRuns: []domain.AgentRun{
			{ID: "run-1", AgentName: "coder", SourceName: "project-a", Issue: domain.Issue{Identifier: "team/project#1"}},
			{ID: "run-2", AgentName: "reviewer", SourceName: "project-b", Status: domain.RunStatusAwaiting, ApprovalState: domain.ApprovalStateAwaiting, Issue: domain.Issue{Identifier: "team/project#2"}},
		},
		Retries: []orchestrator.RetryView{
			{IssueIdentifier: "team/project#3", SourceName: "project-b", Attempt: 2, DueAt: time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)},
		},
		PendingApprovals: []orchestrator.ApprovalView{
			{RequestID: "approval-1", IssueIdentifier: "team/project#2", ToolName: "Write", ApprovalPolicy: "manual"},
		},
	}
	model := NewModel(staticSnapshotProvider{snapshot: snapshot})
	model.quickFilter = quickFilterAwaiting
	model.focus = focusRuns

	view := model.View()
	plain := stripANSI(view)
	if strings.Contains(plain, "team/project#1") {
		t.Fatalf("expected non-awaiting run to be hidden:\n%s", plain)
	}
	if strings.Contains(plain, "team/project#3") {
		t.Fatalf("expected retries to be hidden under awaiting filter:\n%s", plain)
	}
	for _, want := range []string{
		"team/project#2",
		"quick=awaiting-approval",
	} {
		if !viewContains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, plain)
		}
	}
}

func TestViewShowsSelectedRunOutputTail(t *testing.T) {
	snapshot := orchestrator.Snapshot{
		SourceSummaries: []orchestrator.SourceSummary{
			{Name: "project-a", Tracker: "gitlab"},
		},
		ActiveRuns: []domain.AgentRun{
			{
				ID:             "run-1",
				AgentName:      "coder",
				AgentType:      "code-pr",
				HarnessKind:    "claude-code",
				SourceName:     "project-a",
				Status:         domain.RunStatusActive,
				ApprovalPolicy: "auto",
				ApprovalState:  domain.ApprovalStateApproved,
				Issue:          domain.Issue{Identifier: "team/project#1", Title: "Backend work"},
			},
		},
		RunOutputs: []orchestrator.RunOutputView{
			{
				RunID:           "run-1",
				SourceName:      "project-a",
				IssueIdentifier: "team/project#1",
				StdoutTail:      "step 1\nstep 2",
				StderrTail:      "warning line",
				UpdatedAt:       time.Date(2026, 3, 16, 10, 0, 5, 0, time.UTC),
			},
		},
	}
	model := NewModel(staticSnapshotProvider{snapshot: snapshot})
	model.focus = focusRuns

	view := model.View()
	for _, want := range []string{
		"Stdout:",
		"step 1",
		"step 2",
		"Stderr:",
		"warning line",
	} {
		if !viewContains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, stripANSI(view))
		}
	}
}

func TestViewHandlesWindowSizeMsg(t *testing.T) {
	model := NewModel(staticSnapshotProvider{snapshot: orchestrator.Snapshot{}})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	got := updated.(Model)
	if got.width != 120 {
		t.Fatalf("width = %d, want 120", got.width)
	}
	if got.height != 40 {
		t.Fatalf("height = %d, want 40", got.height)
	}
}

func TestUpdateShowsShutdownProgressAndCompletionInTUI(t *testing.T) {
	model := NewModel(
		staticSnapshotProvider{snapshot: orchestrator.Snapshot{}},
		WithShutdown(func() error { return nil }),
	)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := updated.(Model)
	if !got.shuttingDown {
		t.Fatal("expected shutdown mode to start")
	}
	if got.shutdownComplete {
		t.Fatal("expected shutdown to still be in progress")
	}
	if !viewContains(got.View(), "Shutting down Maestro") {
		t.Fatalf("shutdown view missing progress state:\n%s", stripANSI(got.View()))
	}
	if cmd == nil {
		t.Fatal("expected shutdown command")
	}

	msg := cmd()
	finished, ok := msg.(shutdownFinishedMsg)
	if !ok {
		t.Fatalf("shutdown command returned %T", msg)
	}

	updated, cmd = got.Update(finished)
	got = updated.(Model)
	if !got.shutdownComplete {
		t.Fatal("expected shutdown completion state")
	}
	if !viewContains(got.View(), "Shutdown complete.") {
		t.Fatalf("shutdown view missing completion state:\n%s", stripANSI(got.View()))
	}
	if cmd == nil {
		t.Fatal("expected delayed exit command")
	}
	exitMsg := cmd()
	if _, ok := exitMsg.(shutdownExitMsg); !ok {
		t.Fatalf("exit command returned %T", exitMsg)
	}
}

func TestUpdateRequiresQuitConfirmationForQ(t *testing.T) {
	model := NewModel(staticSnapshotProvider{snapshot: orchestrator.Snapshot{}})

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	got := updated.(Model)
	if cmd != nil {
		t.Fatal("expected no quit command on first q")
	}
	if !got.quitConfirm {
		t.Fatal("expected quit confirmation mode after first q")
	}
	if !viewContains(got.View(), "press q or enter again to quit, esc to cancel") {
		t.Fatalf("quit confirmation notice missing:\n%s", stripANSI(got.View()))
	}

	updated, cmd = got.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got = updated.(Model)
	if cmd != nil {
		t.Fatal("expected no command when cancelling quit")
	}
	if got.quitConfirm {
		t.Fatal("expected quit confirmation mode to clear after esc")
	}
	if !viewContains(got.View(), "quit cancelled") {
		t.Fatalf("quit cancelled notice missing:\n%s", stripANSI(got.View()))
	}

	updated, cmd = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	got = updated.(Model)
	if cmd != nil {
		t.Fatal("expected no quit command on re-arming q")
	}
	updated, cmd = got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected quit command after enter confirmation")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected enter to confirm quit")
	}
}
