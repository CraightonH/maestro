package state_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tjohnson/maestro/internal/domain"
	"github.com/tjohnson/maestro/internal/state"
)

func TestStoreSaveAndLoad(t *testing.T) {
	store := state.NewStore(t.TempDir())
	now := time.Now().UTC().Round(time.Second)

	want := state.Snapshot{
		Finished: map[string]state.TerminalIssue{
			"issue-1": {
				IssueID:        "issue-1",
				Identifier:     "TAN-1",
				Status:         domain.RunStatusDone,
				Attempt:        1,
				IssueUpdatedAt: now,
				FinishedAt:     now,
			},
		},
		RetryQueue: map[string]state.RetryEntry{
			"issue-2": {
				IssueID:        "issue-2",
				Identifier:     "TAN-2",
				Attempt:        2,
				DueAt:          now.Add(time.Minute),
				Error:          "boom",
				IssueUpdatedAt: now,
			},
		},
		ActiveRun: &state.PersistedRun{
			RunID:          "run-1",
			IssueID:        "issue-3",
			Identifier:     "TAN-3",
			Status:         domain.RunStatusActive,
			Attempt:        1,
			WorkspacePath:  filepath.Join(t.TempDir(), "workspace"),
			StartedAt:      now,
			LastActivityAt: now,
			IssueUpdatedAt: now,
		},
		PendingApprovals: []state.PersistedApprovalRequest{
			{
				RequestID:       "req-1",
				RunID:           "run-1",
				IssueID:         "issue-3",
				IssueIdentifier: "TAN-3",
				AgentName:       "coder",
				ToolName:        "shell",
				ToolInput:       "rm -rf",
				ApprovalPolicy:  "manual",
				RequestedAt:     now,
				Resolvable:      true,
			},
		},
		ApprovalHistory: []state.PersistedApprovalDecision{
			{
				RequestID:       "req-0",
				RunID:           "run-0",
				IssueID:         "issue-0",
				IssueIdentifier: "TAN-0",
				AgentName:       "coder",
				ToolName:        "shell",
				ApprovalPolicy:  "manual",
				Decision:        "approve",
				RequestedAt:     now.Add(-time.Minute),
				DecidedAt:       now,
				Outcome:         "resolved",
			},
		},
	}

	if err := store.Save(want); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if got.Version == 0 {
		t.Fatal("expected persisted version")
	}
	if got.Finished["issue-1"].Identifier != "TAN-1" {
		t.Fatalf("finished identifier = %q, want TAN-1", got.Finished["issue-1"].Identifier)
	}
	if got.RetryQueue["issue-2"].Attempt != 2 {
		t.Fatalf("retry attempt = %d, want 2", got.RetryQueue["issue-2"].Attempt)
	}
	if got.ActiveRun == nil || got.ActiveRun.RunID != "run-1" {
		t.Fatalf("active run = %+v, want run-1", got.ActiveRun)
	}
	if len(got.PendingApprovals) != 1 || got.PendingApprovals[0].RequestID != "req-1" {
		t.Fatalf("pending approvals = %+v, want req-1", got.PendingApprovals)
	}
	if len(got.ApprovalHistory) != 1 || got.ApprovalHistory[0].Outcome != "resolved" {
		t.Fatalf("approval history = %+v, want resolved entry", got.ApprovalHistory)
	}
}

func TestStoreLoadMissingFileReturnsEmptySnapshot(t *testing.T) {
	store := state.NewStore(t.TempDir())

	got, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Finished) != 0 {
		t.Fatalf("finished entries = %d, want 0", len(got.Finished))
	}
	if len(got.RetryQueue) != 0 {
		t.Fatalf("retry entries = %d, want 0", len(got.RetryQueue))
	}
}

func TestStoreSaveRotatesBackups(t *testing.T) {
	store := state.NewStore(t.TempDir())

	snapshots := []state.Snapshot{
		{
			Finished: map[string]state.TerminalIssue{
				"issue-1": {IssueID: "issue-1", Identifier: "TAN-1", Status: domain.RunStatusDone, FinishedAt: time.Now().UTC()},
			},
		},
		{
			Finished: map[string]state.TerminalIssue{
				"issue-2": {IssueID: "issue-2", Identifier: "TAN-2", Status: domain.RunStatusDone, FinishedAt: time.Now().UTC()},
			},
		},
		{
			Finished: map[string]state.TerminalIssue{
				"issue-3": {IssueID: "issue-3", Identifier: "TAN-3", Status: domain.RunStatusDone, FinishedAt: time.Now().UTC()},
			},
		},
	}

	for _, snapshot := range snapshots {
		if err := store.Save(snapshot); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	current, err := store.Load()
	if err != nil {
		t.Fatalf("load current: %v", err)
	}
	if _, ok := current.Finished["issue-3"]; !ok {
		t.Fatalf("current finished = %+v, want issue-3", current.Finished)
	}

	backupOne := filepath.Join(filepath.Dir(store.Path()), "runs.json.1")
	rawOne, err := os.ReadFile(backupOne)
	if err != nil {
		t.Fatalf("read backup one: %v", err)
	}
	if !strings.Contains(string(rawOne), "issue-2") {
		t.Fatalf("backup one = %s, want issue-2 snapshot", string(rawOne))
	}

	backupTwo := filepath.Join(filepath.Dir(store.Path()), "runs.json.2")
	rawTwo, err := os.ReadFile(backupTwo)
	if err != nil {
		t.Fatalf("read backup two: %v", err)
	}
	if !strings.Contains(string(rawTwo), "issue-1") {
		t.Fatalf("backup two = %s, want issue-1 snapshot", string(rawTwo))
	}
}

func TestStoreLoadArchivesCorruptStateAndReturnsEmptySnapshot(t *testing.T) {
	root := t.TempDir()
	store := state.NewStore(root)
	if err := os.WriteFile(store.Path(), []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}

	snapshot, err := store.Load()
	if err == nil {
		t.Fatal("expected corruption warning")
	}
	var corruptErr *state.CorruptStateError
	if !errors.As(err, &corruptErr) {
		t.Fatalf("load error = %T, want CorruptStateError", err)
	}
	if corruptErr.ArchivedPath == "" {
		t.Fatalf("archived path = %q, want non-empty", corruptErr.ArchivedPath)
	}
	if _, statErr := os.Stat(corruptErr.ArchivedPath); statErr != nil {
		t.Fatalf("archived corrupt file stat error = %v", statErr)
	}
	if _, statErr := os.Stat(store.Path()); !os.IsNotExist(statErr) {
		t.Fatalf("state path stat error = %v, want not exists", statErr)
	}
	if len(snapshot.Finished) != 0 || len(snapshot.RetryQueue) != 0 {
		t.Fatalf("snapshot = %+v, want empty", snapshot)
	}
}

func TestStoreLoadReadOnlyLeavesCorruptStateInPlace(t *testing.T) {
	root := t.TempDir()
	store := state.NewStore(root)
	if err := os.WriteFile(store.Path(), []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}

	snapshot, err := store.LoadReadOnly()
	if err == nil {
		t.Fatal("expected corruption warning")
	}
	var corruptErr *state.CorruptStateError
	if !errors.As(err, &corruptErr) {
		t.Fatalf("load error = %T, want CorruptStateError", err)
	}
	if corruptErr.ArchivedPath != "" {
		t.Fatalf("archived path = %q, want empty", corruptErr.ArchivedPath)
	}
	if _, statErr := os.Stat(store.Path()); statErr != nil {
		t.Fatalf("state path stat error = %v, want file to remain in place", statErr)
	}
	if len(snapshot.Finished) != 0 || len(snapshot.RetryQueue) != 0 {
		t.Fatalf("snapshot = %+v, want empty", snapshot)
	}
}
