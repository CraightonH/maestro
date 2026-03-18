package tui

import (
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
		"Source status:",
		"  Delivery:",
		"> [OK] project-a [gitlab] tags=backend,prod",
		"  Planning:",
		"    [OK] epic-a [gitlab-epic] tags=platform",
		"  linear:",
		"    [OK] linear-a [linear] tags=triage",
		"Selected source:",
		"Name: project-a",
		"Health: OK",
		"Selected source events:",
		"polled 1 candidate issues from project-a",
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
	}

	view := model.View()
	if strings.Contains(view, "project-a [gitlab]") {
		t.Fatalf("expected filtered view to hide project-a:\n%s", view)
	}
	if !strings.Contains(view, "epic-a [gitlab-epic] tags=platform") {
		t.Fatalf("expected filtered view to show epic-a:\n%s", view)
	}
	if !strings.Contains(view, "Filters: group=Planning search=platform") {
		t.Fatalf("expected filter summary in view:\n%s", view)
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
			{Name: "project-a", DisplayGroup: "Delivery", Tracker: "gitlab"},
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
				Attempt:        1,
				ApprovalPolicy: "auto",
				ApprovalState:  domain.ApprovalStateApproved,
				WorkspacePath:  "/tmp/workspace-a",
				StartedAt:      startedAt,
				LastActivityAt: lastActivity,
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
		"Overview: sources=2 active=2 approvals=0 messages=0 retries=0 focus=runs run-sort=oldest",
		"Active runs:",
		"> [RUN] coder on team/project#1 [active]",
		"Source: project-a | Title: Backend work",
		"Selected run:",
		"Run: run-1",
		"Agent: coder (code-pr)",
		"Harness: claude-code",
		"Workspace: /tmp/workspace-a",
		"Selected run output:",
		"Selected run events:",
		"agent coder started for team/project#1",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
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
		"Retries (sort=due-soonest):",
		"> team/project#1 attempt=2 due=2026-03-16T12:00:00Z",
		"Selected retry:",
		"Source: project-a",
		"Issue: team/project#1",
		"Error: network timeout",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestUpdateCyclesSortModesAndCompactView(t *testing.T) {
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

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("O")})
	got = updated.(Model)
	if got.retrySort != retrySortOverdue {
		t.Fatalf("retry sort = %q", got.retrySort)
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	got = updated.(Model)
	if !got.compact {
		t.Fatal("expected compact mode to be enabled")
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
	if strings.Contains(view, "project-a [gitlab]") {
		t.Fatalf("expected project-a to be hidden by attention filter:\n%s", view)
	}
	for _, want := range []string{
		"project-b [gitlab]",
		"team/project#2",
		"quick=attention",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
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
	if strings.Contains(view, "team/project#1") {
		t.Fatalf("expected non-awaiting run to be hidden:\n%s", view)
	}
	if strings.Contains(view, "team/project#3") {
		t.Fatalf("expected retries to be hidden under awaiting filter:\n%s", view)
	}
	for _, want := range []string{
		"team/project#2",
		"quick=awaiting-approval",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
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
		"Selected run output:",
		"Stdout:",
		"step 1",
		"step 2",
		"Stderr:",
		"warning line",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}
