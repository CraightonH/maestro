package orchestrator

import (
	"testing"
	"time"

	"github.com/tjohnson/maestro/internal/domain"
)

func TestApprovalStateConversionsRoundTripFields(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	view := ApprovalView{
		RequestID:       "apr-1",
		RunID:           "run-1",
		IssueID:         "123",
		IssueIdentifier: "GL-123",
		AgentName:       "coder",
		ToolName:        "Bash",
		ToolInput:       `{"command":"git status"}`,
		ApprovalPolicy:  "manual",
		RequestedAt:     now,
		Resolvable:      true,
	}
	history := ApprovalHistoryEntry{
		RequestID:       "apr-1",
		RunID:           "run-1",
		IssueID:         "123",
		IssueIdentifier: "GL-123",
		AgentName:       "coder",
		ToolName:        "Bash",
		ApprovalPolicy:  "manual",
		Decision:        "approve",
		Reason:          "looks good",
		RequestedAt:     now,
		DecidedAt:       now.Add(time.Minute),
		Outcome:         "resolved",
	}

	gotView := approvalViewFromPersisted(persistedApprovalRequestFromView(view))
	if gotView != view {
		t.Fatalf("approval view round-trip = %#v, want %#v", gotView, view)
	}

	gotHistory := approvalHistoryEntryFromPersisted(persistedApprovalDecisionFromHistory(history))
	if gotHistory != history {
		t.Fatalf("approval history round-trip = %#v, want %#v", gotHistory, history)
	}
}

func TestMessageStateConversionsRoundTripFields(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	view := MessageView{
		RequestID:       "msg-1",
		RunID:           "run-1",
		IssueID:         "123",
		IssueIdentifier: "GL-123",
		SourceName:      "platform-dev",
		AgentName:       "coder",
		Kind:            "before_work_review",
		Summary:         "Need confirmation",
		Body:            "Proceed with migration?",
		RequestedAt:     now,
		Resolvable:      true,
	}
	history := MessageHistoryEntry{
		RequestID:       "msg-1",
		RunID:           "run-1",
		IssueID:         "123",
		IssueIdentifier: "GL-123",
		SourceName:      "platform-dev",
		AgentName:       "coder",
		Kind:            "before_work_review",
		Summary:         "Need confirmation",
		Body:            "Proceed with migration?",
		Reply:           "yes",
		ResolvedVia:     "slack",
		RequestedAt:     now,
		RepliedAt:       now.Add(time.Minute),
		Outcome:         "resolved",
	}

	gotView := messageViewFromPersisted(persistedMessageRequestFromView(view))
	if gotView != view {
		t.Fatalf("message view round-trip = %#v, want %#v", gotView, view)
	}

	gotHistory := messageHistoryEntryFromPersisted(persistedMessageReplyFromHistory(history))
	if gotHistory != history {
		t.Fatalf("message history round-trip = %#v, want %#v", gotHistory, history)
	}
}

func TestPersistedRunFromAgentRunCopiesExpectedFields(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	tokensIn := int64(120)
	tokensOut := int64(30)
	run := &domain.AgentRun{
		ID:             "run-1",
		AgentName:      "coder",
		AgentType:      "code-pr",
		SourceName:     "platform-dev",
		HarnessKind:    "claude-code",
		WorkspacePath:  "/tmp/workspace",
		Status:         domain.RunStatusActive,
		Attempt:        2,
		StartedAt:      now,
		LastActivityAt: now.Add(time.Minute),
		Metrics: domain.RunMetrics{
			TokensIn:  &tokensIn,
			TokensOut: &tokensOut,
		},
		Issue: domain.Issue{
			ID:         "123",
			Identifier: "GL-123",
			UpdatedAt:  now.Add(2 * time.Minute),
		},
	}

	got := persistedRunFromAgentRun(run)
	if got == nil {
		t.Fatal("persisted run = nil")
	}
	if got.RunID != run.ID || got.IssueID != run.Issue.ID || got.Identifier != run.Issue.Identifier {
		t.Fatalf("persisted run identity mismatch: %#v", got)
	}
	if got.Status != run.Status || got.Attempt != run.Attempt || got.WorkspacePath != run.WorkspacePath {
		t.Fatalf("persisted run state mismatch: %#v", got)
	}
	if !got.StartedAt.Equal(run.StartedAt) || !got.LastActivityAt.Equal(run.LastActivityAt) || !got.IssueUpdatedAt.Equal(run.Issue.UpdatedAt) {
		t.Fatalf("persisted run timestamps mismatch: %#v", got)
	}
	if got.Metrics.TokensIn == nil || *got.Metrics.TokensIn != tokensIn {
		t.Fatalf("persisted run tokens_in = %#v, want %d", got.Metrics.TokensIn, tokensIn)
	}
	if got.Metrics.TotalTokens == nil || *got.Metrics.TotalTokens != tokensIn+tokensOut {
		t.Fatalf("persisted run total_tokens = %#v, want %d", got.Metrics.TotalTokens, tokensIn+tokensOut)
	}
}
