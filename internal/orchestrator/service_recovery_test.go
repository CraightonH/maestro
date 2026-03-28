package orchestrator_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tjohnson/maestro/internal/orchestrator"
	"github.com/tjohnson/maestro/internal/state"
	"github.com/tjohnson/maestro/internal/testutil"
	"github.com/tjohnson/maestro/internal/workspace"
)

func TestServiceStartsWithEmptyRecoveryStateWhenRunsFileIsCorrupt(t *testing.T) {
	root := t.TempDir()
	cfg := testConfigWithRoot(t, root)
	if err := os.MkdirAll(cfg.State.Dir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	runsPath := filepath.Join(cfg.State.Dir, "runs.json")
	if err := os.WriteFile(runsPath, []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}

	svc, err := orchestrator.NewServiceWithDeps(cfg, testLogger(), orchestrator.Dependencies{
		Tracker:    &testutil.FakeTracker{},
		Harness:    &testutil.FakeHarness{},
		Workspace:  workspace.NewManager(cfg.Workspace.Root),
		StateStore: state.NewStore(cfg.State.Dir),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	snapshot := svc.Snapshot()
	if snapshot.ActiveRun != nil {
		t.Fatalf("active run = %+v, want nil", snapshot.ActiveRun)
	}
	if len(snapshot.Retries) != 0 || len(snapshot.PendingApprovals) != 0 {
		t.Fatalf("snapshot = %+v, want empty recovery state", snapshot)
	}

	archived, err := filepath.Glob(runsPath + ".corrupt.*")
	if err != nil {
		t.Fatalf("glob corrupt archive: %v", err)
	}
	if len(archived) != 1 {
		t.Fatalf("archived runs files = %v, want 1", archived)
	}
	if _, statErr := os.Stat(runsPath); !os.IsNotExist(statErr) {
		t.Fatalf("runs.json stat error = %v, want not exists", statErr)
	}
}
