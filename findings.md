# Maestro Full Repo Review

## Critical

### 1. Web API has zero authentication
**`internal/api/server.go:301-330`** — All endpoints (approve actions, stop runs, save config, write packs, reply to agents) are unauthenticated. Binds to `127.0.0.1` by default, but any local process or browser tab can hit it (CSRF). If rebound to `0.0.0.0`, everything is network-exposed.

**Status:** Done.

**Implemented fix:** Non-loopback binds now require an API key, loopback stays open by default, and the dashboard can bootstrap auth with `?api_key=...`.

### 2. Pack save endpoint allows path traversal
**`internal/api/server.go:1200-1205`** — Pack name from request body is used directly in `filepath.Join(base, name)`. A name like `../../etc` writes files outside the agents directory.

**Status:** Done.

**Implemented fix:** New-pack path resolution now uses canonicalized base paths plus `filepath.Rel` containment checks, and rejects absolute or escaping paths.

---

## High

### 3. Codex readLoop EOF exits without signaling `doneCh`
**`internal/harness/codex/adapter.go:399-403`** — On stdout EOF, `readLoop` returns without calling `r.finish()`. If `turn/completed` was never sent, `Wait()` blocks forever.

**Status:** Done.

**Implemented fix:** EOF before turn completion is treated as an error, pending RPC waiters are drained, and the run is finished so `Start`/`Wait` cannot hang.

### 4. `ResolveMessage` race — read-then-act without lock
**`internal/orchestrator/messages.go:71-73, 88-90`** — Reads `s.messages[requestID]` under `RLock`, releases it, then re-acquires write lock later. Two goroutines can double-resolve, calling `harness.Reply()` twice and double-closing the waiter channel. Compare with `ResolveApproval` which properly uses `claimApprovalResolution`.

**Status:** Done.

**Implemented fix:** Message resolution now uses the same claim/restore pattern as approvals, so only one resolver can proceed and failures roll back cleanly.

### 5. Codex `request()` blocks forever on process death
**`internal/harness/codex/adapter.go:611`** — If the codex process dies before responding to an RPC, the pending channel is never resolved. The calling goroutine hangs forever on `<-respCh`.

**Status:** Done.

**Implemented fix:** Process exit now drains all pending RPC channels with a synthetic error, so callers fail fast instead of hanging.

### 6. `os.Setenv` race in config validation
**`internal/api/server.go:1294-1330`** — `withValidationEnv` temporarily mutates process-global env vars. Despite a mutex, any concurrent goroutine (orchestrator, harness subprocess) that reads env during this window sees placeholder values.

**Status:** Done.

**Implemented fix:** Config loading now supports injected env lookup for validation, so API validation no longer mutates process-global environment variables.

### 7. Service is a god object
**`internal/orchestrator/service.go:184-213`** — 17 fields, behavior across 10+ files. Owns polling, dispatch, approval routing, message routing, stall detection, state persistence, hooks, output buffering, retry scheduling, and events.

**Status:** Done.

**Implemented fix:** Orchestrator behavior now lives behind focused internal components and files:
- `stateManager` for restore/save/snapshot/retry bookkeeping
- `approvalRouter` for approval watch/register/resolve/timeout handling
- `messageRouter` for message watch/register/resolve/history handling
- `runManager` for dispatch/start/finalize lifecycle

The physical file layout now matches those boundaries, with run lifecycle split into `run_manager.go` and `run_finalize.go`, and the large service test file split into approval/message/recovery-focused test files.

**Why this matters:** This is the main maintainability risk in the orchestrator. Behavior is already spread across multiple files, but ownership and mutable state are still centralized in `Service`, which makes changes harder to reason about and test in isolation.

**Recommended plan:**
1. Extract pure state helpers first, before moving behavior.
   - Pull retry and lifecycle transition calculations into small stateless helpers.
   - Keep `Service` as the only owner of locks and mutable maps.
2. Introduce focused coordinator types around existing state, not new copies of state.
   - `runManager`: dispatch, reconcile, finalize, stall checks
   - `approvalRouter`: register, watch, resolve, expire approvals
   - `messageRouter`: register, watch, resolve, expire messages
   - `stateManager`: load, save, restore, retry queue bookkeeping
3. Inject narrow dependencies instead of the full `Service`.
   - Pass only the tracker/harness/state/logger pieces each coordinator needs.
   - Avoid back-references that re-create a god object under another name.
4. Keep locking centralized during the first pass.
   - Coordinators should call back into shared lock-owning helpers instead of each owning their own mutex.
   - This reduces refactor risk and avoids introducing fresh race conditions.
5. Split tests alongside the refactor.
   - Move approval/message focused tests out of the broad service test file when the logic has a real home.
   - Keep one or two end-to-end service tests as integration coverage.

**Recommended stopping point for phase 1:** `Service` still owns lifecycle and goroutine startup, but approval/message/run/state logic each live behind a focused type with clear tests.

**Concrete implementation sequence:**
1. Consolidate state/persistence behavior by file first.
   - Move `restoreState`, `saveStateBestEffort`, `snapshotState`, retry helpers, and stale-record helpers into `internal/orchestrator/state_manager.go`.
   - Keep them as `Service` methods in phase 1 so no call sites change shape yet.
2. Consolidate approval behavior next.
   - Move approval watch/register/resolve/expire helpers into `internal/orchestrator/approval_router.go`.
   - Keep approval state in `Service`; the new file is a behavior boundary, not a new owner of state.
3. Consolidate message behavior after approvals.
   - Move message watch/register/resolve/history helpers into `internal/orchestrator/message_router.go`.
   - Keep before-work gate orchestration in `dispatch.go`, but route message lifecycle operations through the extracted helpers.
4. Extract run lifecycle behavior last.
   - Move dispatch/start/prepare logic into `internal/orchestrator/run_manager.go`.
   - Move completion/failure/finalization into `internal/orchestrator/run_finalize.go`.
   - Leave `tick` and top-level shutdown flow in `loop.go`.
5. Reduce `service.go` to composition/root responsibilities.
   - Keep only struct definitions, constructors, top-level snapshot/public entrypoints, and shared utility types there.
6. Split tests along the same boundaries.
   - approval tests → `approval_router_test.go`
   - message tests → `message_router_test.go`
   - state/retry/restore tests → `state_manager_test.go`
   - run lifecycle tests → `run_manager_test.go`
   - keep a smaller `service_test.go` for integration coverage

**File-by-file target layout:**
- `internal/orchestrator/service.go`
  - constructors, `Service` type, public snapshot/view types, shared small utilities
- `internal/orchestrator/loop.go`
  - `Run`, `tick`, `shutdown`
- `internal/orchestrator/state_manager.go`
  - restore/save/snapshot/retry bookkeeping
- `internal/orchestrator/approval_router.go`
  - approval registration, resolution, timeout expiry
- `internal/orchestrator/message_router.go`
  - message registration, resolution, history
- `internal/orchestrator/run_manager.go`
  - dispatch, prepare, execute, workspace/prompt/harness start
- `internal/orchestrator/run_finalize.go`
  - complete/fail/finalize transitions

**Success criteria for #7:**
- `service.go` stops being the place where most orchestrator behavior lives
- each major behavior area has one primary file and one primary test file
- `Service` remains the single mutable state owner during phase 1, so refactor risk stays bounded

### 8. Triple-duplicated approval/message types
**`internal/orchestrator/state.go`, `internal/api/server.go`** — Approval/message view types exist in orchestrator, state, and API layers with field-by-field copying. Every new field must be added in 3 places.

**Status:** Done.

**Implemented fix:** Conversion logic is now centralized instead of scattered:
- `internal/orchestrator/state_convert.go` owns orchestrator ↔ persisted state mapping
- `internal/api/view_convert.go` owns orchestrator snapshot → API JSON mapping

`restoreState`, `snapshotState`, and the API snapshot/status encoding paths now all use those shared conversion helpers, eliminating the duplicated field-by-field approval/message copying.

**Why this matters:** This is mostly a maintenance problem, but it also creates drift risk when fields get added to one representation and not the others.

**Recommended plan:**
1. Separate canonical internal state from wire representations.
   - Do not force one struct to serve orchestrator, persistence, and API equally.
   - Pick one internal canonical type for approvals and one for messages inside the orchestrator.
2. Reuse persisted snapshot structs where the fields are truly identical.
   - The persisted/store layer is the best candidate for shared embedded snapshots because it is already the durable shape.
   - API-specific response wrappers should stay API-local if they need extra presentation fields.
3. Replace field-by-field copying with explicit mappers in one place.
   - `toStoredApproval`, `toStoredMessage`
   - `toAPIApproval`, `toAPIMessage`
   - Keep these in a small conversion file instead of scattering assignments through handlers and restore logic.
4. Consolidate shared fields into embedded snapshot structs if the shapes remain aligned.
   - Example: shared `ApprovalSnapshot` / `MessageSnapshot` with storage and API wrappers embedding them.
   - Avoid embedding live orchestrator-only fields like channels or resolvable/in-flight state into persisted or API types.
5. Add a regression test whenever a new field is introduced.
   - The goal is to make drift obvious immediately rather than relying on manual review.

**Recommended stopping point for phase 1:** one canonical conversion path per concept, no scattered field-by-field copies in handlers or restore code, and clear separation between live state and serialized/API shapes.

**Concrete implementation sequence:**
1. Keep orchestrator view types canonical.
   - `ApprovalView`, `ApprovalHistoryEntry`, `MessageView`, and `MessageHistoryEntry` remain the internal source of truth.
   - Do not try to make state-store or API types become the canonical live types.
2. Centralize persistence conversions.
   - Add `internal/orchestrator/state_convert.go`.
   - Move all `orchestrator <-> state` field mapping there:
     - approvals pending/history
     - messages pending/history
     - active run snapshot
   - Exact functions to add:
     - `approvalViewFromPersisted(state.PersistedApprovalRequest) ApprovalView`
     - `persistedApprovalRequestFromView(ApprovalView) state.PersistedApprovalRequest`
     - `approvalHistoryFromPersisted(state.PersistedApprovalDecision) ApprovalHistoryEntry`
     - `persistedApprovalDecisionFromHistory(ApprovalHistoryEntry) state.PersistedApprovalDecision`
     - `messageViewFromPersisted(state.PersistedMessageRequest) MessageView`
     - `persistedMessageRequestFromView(MessageView) state.PersistedMessageRequest`
     - `messageHistoryFromPersisted(state.PersistedMessageReply) MessageHistoryEntry`
     - `persistedMessageReplyFromHistory(MessageHistoryEntry) state.PersistedMessageReply`
     - `persistedRunFromAgentRun(*domain.AgentRun) *state.PersistedRun`
   - Exact call sites to replace:
     - the inline approval/message restore loops in `restoreState`
     - the inline approval/message snapshot loops in `snapshotState`
3. Centralize API conversions.
   - Add `internal/api/view_convert.go`.
   - Move all `orchestrator Snapshot -> API response` field mapping there instead of inlining it in handlers.
   - Exact functions to add:
     - `apiApproval(orchestrator.ApprovalView) approvalJSON`
     - `apiApprovalHistory(orchestrator.ApprovalHistoryEntry) approvalHistoryJSON`
     - `apiMessage(orchestrator.MessageView) messageJSON`
     - `apiMessageHistory(orchestrator.MessageHistoryEntry) messageHistoryJSON`
     - `apiRetry(orchestrator.RetryView) retryJSON`
     - `apiRunOutput(orchestrator.RunOutputView) runOutputJSON`
     - `apiRun(domain.AgentRun, map[string]runOutputJSON) runJSON`
     - `apiSourceSummary(orchestrator.SourceSummary) sourceSummaryJSON`
     - `apiEvent(orchestrator.Event) eventJSON`
     - `apiSnapshot(orchestrator.Snapshot) snapshotJSON`
   - Exact call sites to replace:
     - `encodeSnapshot`
     - `encodeApprovals`
     - `encodeMessages`
     - `encodeApprovalHistory`
     - `encodeMessageHistory`
     - any retry/run/source summary/event inline mapping in `server.go`
4. Only introduce shared embedded snapshot structs where the data is truly identical.
   - If persisted approval/message request/history shapes remain aligned, add small shared snapshot structs for those data-only fields.
   - Do not merge in live-only fields like channels, locks, or in-flight resolution semantics.
5. Add propagation tests for every conversion boundary.
   - restore/save round-trip test for approvals/messages in orchestrator
   - API snapshot test proving all approval/message fields are exposed after conversion cleanup

**First patch scope I would actually implement:**
- create `internal/orchestrator/state_convert.go`
- switch `restoreState` and `snapshotState` to those helpers
- no API changes yet
- verify with:
  - `go test ./internal/orchestrator`
  - `go test ./...`

**Second patch scope:**
- create `internal/api/view_convert.go`
- replace `encodeSnapshot` and related encode helpers with thin wrappers around the new conversion functions
- add one API snapshot regression test that exercises approvals, messages, retries, runs, and histories together

**File-by-file target layout:**
- `internal/orchestrator/service.go`
  - canonical orchestrator view types only
- `internal/orchestrator/state_convert.go`
  - orchestrator ↔ persisted state conversion helpers
- `internal/orchestrator/state_manager.go`
  - uses conversion helpers instead of inline field copying
- `internal/api/view_convert.go`
  - orchestrator snapshot → API response helpers
- `internal/api/server.go`
  - handlers call conversion helpers instead of doing field-by-field copying inline

**Success criteria for #8:**
- adding a new approval/message field requires updating one canonical type plus one conversion helper per boundary
- restore/save and API snapshot code no longer contain scattered manual struct population
- live state and wire/storage shapes stay intentionally separate where semantics differ

---

## Medium

### 9. Full parent env inherited by agent subprocesses
**`internal/harness/env.go:5-11`** — `MergeEnv` starts with `os.Environ()`. All parent secrets (`GITLAB_TOKEN`, `SLACK_BOT_TOKEN`) are passed to every Claude/Codex subprocess.

**Recommendation:** Allowlist env vars for child processes.

### 10. GitLab token visible in process args
**`internal/workspace/manager.go:310-323`** — Token passed as `-c http.extraHeader=...` git argument, visible via `ps aux`.

**Recommendation:** Use `GIT_CONFIG_COUNT`/`GIT_CONFIG_KEY_*`/`GIT_CONFIG_VALUE_*` env vars.

### 11. Supervisor error routing via string matching
**`internal/orchestrator/supervisor.go:187-199`** — `strings.Contains(err.Error(), "not found")` is fragile.

**Fix:** Define `var ErrNotFound` sentinel error and use `errors.Is`.

### 12. Codex `turnNumber` data race
**`internal/harness/codex/adapter.go:432, 446`** — Read from `readLoop` goroutine, written from spawned continuation goroutine without synchronization.

### 13. `reconcileActiveRun` shallow-copies Issue with shared map/slice
**`internal/orchestrator/tracker_sync.go:14-20`** — Value copy of `AgentRun` shares `Issue.Meta` map and `Labels` slice with the original. `snapshotIssue` exists but isn't used here.

### 14. `sanitizeError` breaks error chains
**`internal/orchestrator/sanitize.go:12-17`** — `fmt.Errorf("%s", ...)` kills `errors.Is/As` matching. Consider a `SanitizedError` type that wraps the original.

### 15. No `Channel` interface abstraction
**`internal/channel/bridge.go`** — Tightly coupled to Slack. No interface for plugging in Teams/GitLab channels.

### 16. Slack inbound messages don't verify user authorization
**`internal/channel/bridge.go`** — Any workspace member who can see the channel can approve/reject agent actions.

### 17. `context.Background()` in stop/approve/reply paths
**`internal/orchestrator/approvals.go:91`, `messages.go:93`, `stop.go:28`** — Operations survive parent context cancellation. `Stop` with `context.Background()` could hang during shutdown.

### 18. `completeRun`/`failRun` extensive duplication
**`internal/orchestrator/dispatch.go:325-501`** — ~175 lines of near-identical logic.

**Fix:** Extract a unified `finalizeRun(runID string, err error)`.

---

## Low

### 19. State store `copyFile` double-closes output
**`internal/state/store.go:313-319`** — `defer output.Close()` + explicit `return output.Close()`.

### 20. `StallTimeout` zero causes immediate stall detection
**`internal/orchestrator/stall.go:26`** — If zero (unconfigured), `time.Since(...) < 0` is always false, triggering stall on first tick.

### 21. `PollInterval` zero would panic `time.NewTicker`
**`internal/orchestrator/loop.go:20`** — Config validation should catch this, but no defensive check.

### 22. Duplicated `isZeroFilter` implementations
**`internal/config/types.go:330`** and **`internal/tracker/gitlab/adapter.go:520`** — Identical functions.

### 23. GitLab `ensureProject` not thread-safe
**`internal/tracker/gitlab/adapter.go:382-394`** — Reads/writes `a.project` without synchronization. Low risk today but would fail the race detector.

---

## Test Coverage Gaps

191 tests passing, 30 test files, 14 live integration tests. Overall quality is good — behavioral, well-isolated, proper use of fakes.

| Missing Test | Risk |
|---|---|
| **Stall detection** (`stall.go`) | High — only defense against hung agents, zero coverage |
| **Limiter** (`limiter.go`) | Medium — semaphore + composite limiter only tested indirectly |
| **Tail buffer overflow** (`activity.go`) | Medium — 4096-byte truncation untested |
| **Hook failure blocking run** (`hooks.go`) | Medium — only best-effort path exercised |
| **`ops/manage.go`** | Medium — `RunsSummary`, `ResetIssue` have no tests |
| **`beforeWorkGate` cancellation** | Low — "run stopped before work began" path untested |
| Duplicate helpers in `supervisor_test.go` vs `service_test.go` | Maintenance — should consolidate into `testutil` |
| Sleep-then-assert anti-pattern in ~4 tests | Flakiness risk under load |

---

## What's Done Well

- **Minimal dependencies** — 5 direct deps in go.mod
- **Interface design** — `Harness` (7 methods), `Tracker` (7 methods), `Runtime` (4 methods) are clean and consumer-defined
- **Config validation** — `ValidateMVP` at 234 lines is thorough, including cross-reference and domain rule checks
- **State persistence** — atomic writes (temp + rename), backup rotation, corrupt file archival
- **Credential defense-in-depth** — `redact` package + `redactingHandler` in logging + `sanitizeError/Output` in orchestrator + system preamble in prompts
- **Test infrastructure** — `FakeTracker`/`FakeHarness` are well-designed, mutex-protected, channel-based synchronization
- **Path safety** — `WorkspaceKey` sanitizes identifiers, `copyOptionalDir` rejects symlinks, repo pack paths guarded against traversal
- **CLI arguments** — All exec calls use discrete string slices, never shell interpolation
