# Ringer Go Plan 4 — HUD Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `ringer hud` serves the persistent Ringside dashboard on 127.0.0.1:8700 — all frozen endpoints (`/`, `/api/runs`, `/api/models`, `/api/library`, `/api/open-folder`, `/artifacts/<path>`, `/logs/<run_id>/<task_key>`) — reading the **Go-authoritative** `runs/<id>.json` schema and adapting it into the shape `ringside.html` already expects, so the HUD half-visibility gap demonstrated in Plan 2 is closed.

**Architecture:** A new `internal/hud` package owns a `chi` router bound to a single fixed port (fail-if-taken, as upstream). `ringside.html` is embedded via `go:embed` with the "Models" tab **baked into the committed asset** (upstream injected it at serve time for Tauri reasons that no longer exist). `/api/runs` runs a **schema adapter**: the Go run-state (`state.RunState`, `done:bool`) is transformed into the Python-era run shape (`state`/`pass`/`fail`/`elapsed_s`, task `status` buckets) that `ringside.html`'s `normalizeRuns` reads by name. A new `internal/artifact` package holds the `library.json` types + reader + dead-run reconciliation (shared with the Plan-4b writer later). `run`/`demo` gain ensure-HUD-running (probe → spawn detached → open browser once).

**Tech Stack:** Go 1.26, `github.com/go-chi/chi/v5` (new dep, the project's router per house stack), `net/http` + `net/http/httptest`, `go:embed`, existing `internal/{state,config,logging}` packages. No templ in this plan (artifact HTML rendering is Plan 4b).

## Scope boundary (read this first)

Design spec §8 ("HUD and artifacts") covers two separable subsystems joined only by the on-disk artifacts tree + `library.json` contract. This plan is the **HUD server** half — it *serves* whatever artifact tree exists (Python-era runs already wrote one to `~/.ringer/artifacts`, and the Go runner already writes `runs/<id>.json`), so it is shippable and testable standalone via `httptest` + fixture trees. The **artifact rendering** half (the zero-LLM templ pages, `library.json` *writing*, deliverables harvest, wired into the runner's flush tick) is **Plan 4b** — see "Out of scope → Plan 4b" at the end. This split follows the writing-plans scope-check (two independent subsystems → separate plans, each shippable on its own).

`/api/models` is served here with a **valid but empty** payload shape; the model-log aggregation (scoreboard tiers, catalog enrichment, identity) is the **Plan 5 (analytics)** subsystem. Preserving the shape now keeps `ringside.html`'s Models tab from erroring; Plan 5 fills the data from the SQLite store.

## Global Constraints

- Build/test ONLY via `./build.sh --test` (never raw `go build`/`go test`); `gofmt` clean (build.sh enforces).
- **Frozen HTTP API (spec §9.5), bound to 127.0.0.1:8700, single fixed port, fail if taken (as upstream):** `/` (ringside HTML), `/api/runs`, `/api/models`, `/api/library`, `/api/open-folder`, `/artifacts/<path>`, `/logs/<run_id>/<task_key>` (64KB tail). Field names in JSON responses are a frozen contract consumed by `ringside.html`'s `normalizeRuns`/`normalizeLibrary`/`normalizeArtifactState` by name — do not rename.
- **§9.4 on-disk schemas:** `runs/<id>.json` is **Go-authoritative** (the Plan-2 `state.RunState` schema, `done:bool`); the HUD reads it and adapts. `active-runs.json` keeps Python parity (5-field entries incl. `workdir`; pid-prune-on-read). `library.json` schema is frozen and unversioned (no `schema_version` field — keyed purely by field names).
- **`/api/open-folder` is a spec-mandated FIX:** upstream is macOS-only (`open`) and returns 501 on Linux. The Go version MUST use `xdg-open` on Linux, `open` on macOS (spec §8: "fix: `xdg-open` on Linux, `open` on macOS").
- Errors route through the injected `logging.Logger`; never `_ =` an error silently. HTTP handler failures return the right status code AND log at Warn/Error; per-request access logging is suppressed (upstream overrides `log_message` to a no-op).
- Responses: JSON endpoints set `Content-Type: application/json; charset=utf-8` + `Cache-Control: no-store`, serialized with sorted keys (`json.Marshal` on a map sorts keys; for structs, field order is the contract). `/` sets `text/html; charset=utf-8` (no no-store). `/logs/...` sets `text/plain; charset=utf-8` + no-store. Artifact files set a per-extension content type + no-store.
- New dependency allowed: `github.com/go-chi/chi/v5` only.
- `WORKER_LOG_TAIL_BYTES = 64 * 1024`. `DEFAULT_HUD_PORT = 8700`.
- The HUD serves ONLY from `<state_dir>/artifacts` (the canonical tree); custom `[artifact]` output templates are not consulted by the server.
- Tests are black-box `httptest` over the router with `t.TempDir()` state dirs; no test binds a real port except one ensure-running integration test that uses a probe against an ephemeral port.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/artifact/paths.go` (new) | canonical artifact-tree path helpers (`ArtifactsDir`, `LibraryPath`, `DeliverablesDir`, `sanitizeName`) |
| `internal/artifact/library.go` (new) | `library.json` types (`Library`, `Entry`, `Version`), `ReadLibrary`, `WriteLibrary` |
| `internal/artifact/reconcile.go` (new) | `ReconcileDeadRuns` (live→died via active-runs pid liveness) |
| `internal/state/state.go` (modify) | `TaskView` gains `StartedAt`/`EndedAt` for per-task elapsed |
| `internal/runner/runner.go` (modify) | populate the new per-task timestamps |
| `internal/hud/embed.go` (new) | `//go:embed assets/ringside.html` + accessor |
| `internal/hud/assets/ringside.html` (new) | committed Ringside asset with the Models tab **baked in** |
| `internal/hud/server.go` (new) | `Server` struct, chi router wiring, `New`, `ListenAndServe` (fail-if-taken), `Handler()` for tests |
| `internal/hud/runs.go` (new) | `/api/runs` handler + the Go-run-state→ringside adapter |
| `internal/hud/logs.go` (new) | `/logs/<run_id>/<task_key>` handler (64KB byte-tail) |
| `internal/hud/artifacts.go` (new) | `/artifacts/<path>` + `/api/open-folder` handlers |
| `internal/hud/library.go` (new) | `/api/library` handler (reconcile + serve) |
| `internal/hud/models.go` (new) | `/api/models` handler (valid empty shape) |
| `cmd/ringer/hud.go` (new) | the `hud` subcommand (bind, block forever, `--port`/`--no-open`) |
| `cmd/ringer/ensurehud.go` (new) | `ensureHUDRunning`: probe → spawn detached → open browser once |
| `cmd/ringer/run.go` (modify) | call `ensureHUDRunning` unless `--no-dashboard` |
| `cmd/ringer/demo.go` (modify) | same hook |

Task order: on-disk contract types first (`internal/artifact`), then the per-task timing the adapter needs, then the server skeleton + embed + models-tab bake, then each endpoint, then ensure-running last (it depends on the server existing).

---

### Task 1: `internal/artifact` — canonical paths + `library.json` types + reader

**Files:**
- Create: `internal/artifact/paths.go`, `internal/artifact/library.go`
- Test: `internal/artifact/library_test.go`

**Interfaces:**
- Consumes: nothing (leaf package; stdlib only).
- Produces:
  - `ArtifactsDir(stateDir string) string` → `<stateDir>/artifacts`; `LibraryPath(stateDir string) string` → `<stateDir>/artifacts/library.json`; `DeliverablesDir(stateDir, runID, taskKey string) string`; `SanitizeName(s string) string`.
  - Types `Library{Artifacts map[string]Entry}`, `Entry{LivePath, State, Identity, CurrentRunID, UpdatedAt string; Versions []Version}`, `Version{RunID, Path string; ReportPath *string; FinishedAt, Outcome string; TasksPass, TasksFail int; Deliverables []Deliverable}`, `Deliverable{TaskKey, Name, Path string; Bytes int64}`.
  - `ReadLibrary(stateDir string) Library` (malformed/missing → `{Artifacts: map[string]Entry{}}`), `WriteLibrary(stateDir string, lib Library) error` (atomic).

**Why:** the `library.json` schema is a frozen, unversioned contract (Explore map §B.3) read by `ringside.html`'s `normalizeLibrary` by field name. These types are the shared vocabulary the HUD reads now and the Plan-4b writer produces later. Defining them once here (not twice) is the DRY seam.

- [ ] **Step 1: Write the failing test**

Create `internal/artifact/library_test.go`:

```go
package artifact

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPathsCanonical(t *testing.T) {
	if got := ArtifactsDir("/s"); got != "/s/artifacts" {
		t.Fatalf("ArtifactsDir = %q", got)
	}
	if got := LibraryPath("/s"); got != "/s/artifacts/library.json" {
		t.Fatalf("LibraryPath = %q", got)
	}
	if got := DeliverablesDir("/s", "run 1", "task/key"); got != "/s/artifacts/deliverables/run-1/task-key" {
		t.Fatalf("DeliverablesDir = %q (name sanitization)", got)
	}
}

func TestReadLibraryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	report := "/s/artifacts/versions/demo/rid-report.html"
	lib := Library{Artifacts: map[string]Entry{
		"demo": {
			LivePath: "/s/artifacts/live/demo.html", State: "pass",
			Identity: "jim", CurrentRunID: "demo-123", UpdatedAt: "2026-07-09T00:00:00Z",
			Versions: []Version{{
				RunID: "demo-123", Path: "/s/artifacts/versions/demo/demo-123.html",
				ReportPath: &report, FinishedAt: "2026-07-09T00:00:00Z", Outcome: "pass",
				TasksPass: 3, TasksFail: 0,
				Deliverables: []Deliverable{{TaskKey: "alpha", Name: "out.txt", Path: "/x/out.txt", Bytes: 12}},
			}},
		},
	}}
	if err := WriteLibrary(dir, lib); err != nil {
		t.Fatal(err)
	}
	got := ReadLibrary(dir)
	if got.Artifacts["demo"].State != "pass" || got.Artifacts["demo"].CurrentRunID != "demo-123" {
		t.Fatalf("entry round-trip lost fields: %+v", got.Artifacts["demo"])
	}
	if len(got.Artifacts["demo"].Versions) != 1 || got.Artifacts["demo"].Versions[0].TasksPass != 3 {
		t.Fatalf("version round-trip lost fields: %+v", got.Artifacts["demo"].Versions)
	}
	// Frozen field names: assert the raw JSON keys, since ringside reads by name.
	raw, _ := os.ReadFile(LibraryPath(dir))
	var probe map[string]any
	_ = json.Unmarshal(raw, &probe)
	arts, ok := probe["artifacts"].(map[string]any)
	if !ok {
		t.Fatalf("top-level key must be \"artifacts\": %s", raw)
	}
	entry := arts["demo"].(map[string]any)
	for _, k := range []string{"live_path", "state", "identity", "current_run_id", "updated_at", "versions"} {
		if _, ok := entry[k]; !ok {
			t.Fatalf("entry missing frozen key %q: %v", k, entry)
		}
	}
}

func TestReadLibraryMissingIsEmpty(t *testing.T) {
	lib := ReadLibrary(t.TempDir())
	if lib.Artifacts == nil || len(lib.Artifacts) != 0 {
		t.Fatalf("missing library must read as empty non-nil map, got %+v", lib)
	}
	// A garbage file also degrades to empty, never panics.
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Dir(LibraryPath(dir)), 0o755)
	_ = os.WriteFile(LibraryPath(dir), []byte("{ not json"), 0o644)
	if lib := ReadLibrary(dir); len(lib.Artifacts) != 0 {
		t.Fatalf("garbage library must read as empty, got %+v", lib)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — package `internal/artifact` does not exist.

- [ ] **Step 3: Implement paths**

Create `internal/artifact/paths.go`:

```go
// Package artifact holds the on-disk artifact-tree contract shared by the
// HUD (which serves it) and the Plan-4b renderer (which writes it): the
// canonical path layout under <state_dir>/artifacts and the frozen,
// unversioned library.json schema that ringside.html consumes by field name.
package artifact

import (
	"path/filepath"
	"regexp"
	"strings"
)

// ArtifactsDir is the canonical artifact tree; the HUD serves only from here.
func ArtifactsDir(stateDir string) string { return filepath.Join(stateDir, "artifacts") }

// LibraryPath is the frozen library.json location.
func LibraryPath(stateDir string) string { return filepath.Join(ArtifactsDir(stateDir), "library.json") }

// DeliverablesDir is where a task's harvested deliverables are copied.
func DeliverablesDir(stateDir, runID, taskKey string) string {
	return filepath.Join(ArtifactsDir(stateDir), "deliverables", SanitizeName(runID), SanitizeName(taskKey))
}

var unsafeNameRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// SanitizeName maps a run_id/run_name/task_key into a single safe path
// component: runs of anything outside [A-Za-z0-9._-] collapse to a single
// dash, and leading/trailing dashes are trimmed. Mirrors upstream
// sanitize_artifact_name so on-disk paths match between eras.
func SanitizeName(s string) string {
	cleaned := unsafeNameRe.ReplaceAllString(s, "-")
	cleaned = strings.Trim(cleaned, "-")
	if cleaned == "" {
		return "unnamed"
	}
	return cleaned
}
```

- [ ] **Step 4: Implement the library types + reader/writer**

Create `internal/artifact/library.go`:

```go
package artifact

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Library is the top-level library.json shape: {"artifacts": {<run_name>: entry}}.
type Library struct {
	Artifacts map[string]Entry `json:"artifacts"`
}

// Entry is one run_name's live status plus its version history.
type Entry struct {
	LivePath     string    `json:"live_path"`
	State        string    `json:"state"` // live | died | pass | fail
	Identity     string    `json:"identity"`
	CurrentRunID string    `json:"current_run_id"`
	UpdatedAt    string    `json:"updated_at"`
	Versions     []Version `json:"versions"`
}

// Version is one finished run's frozen artifact record.
type Version struct {
	RunID        string        `json:"run_id"`
	Path         string        `json:"path"`
	ReportPath   *string       `json:"report_path"` // null when identical to Path
	FinishedAt   string        `json:"finished_at"`
	Outcome      string        `json:"outcome"` // live | died | pass | fail
	TasksPass    int           `json:"tasks_pass"`
	TasksFail    int           `json:"tasks_fail"`
	Deliverables []Deliverable `json:"deliverables"`
}

// Deliverable is one harvested file recorded on a version.
type Deliverable struct {
	TaskKey string `json:"task_key"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Bytes   int64  `json:"bytes"`
}

// ReadLibrary loads library.json, degrading a missing or malformed file to an
// empty (non-nil) map — never an error, never a panic. Mirrors upstream
// read_artifact_library's {"artifacts": {}} fallback.
func ReadLibrary(stateDir string) Library {
	lib := Library{Artifacts: map[string]Entry{}}
	data, err := os.ReadFile(LibraryPath(stateDir))
	if err != nil {
		return lib
	}
	var parsed Library
	if err := json.Unmarshal(data, &parsed); err != nil || parsed.Artifacts == nil {
		return lib
	}
	return parsed
}

// WriteLibrary atomically writes library.json (tmp + rename).
func WriteLibrary(stateDir string, lib Library) error {
	if lib.Artifacts == nil {
		lib.Artifacts = map[string]Entry{}
	}
	path := LibraryPath(stateDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(lib, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".library-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS (all three new tests + the rest of the suite).

- [ ] **Step 6: Commit**

```bash
git add internal/artifact
git commit -m "feat(artifact): canonical paths + frozen library.json types + reader/writer"
```

---

### Task 2: Dead-run reconciliation (`ReconcileDeadRuns`)

**Files:**
- Create: `internal/artifact/reconcile.go`
- Test: `internal/artifact/reconcile_test.go`

**Interfaces:**
- Consumes: `ReadLibrary`/`WriteLibrary` (Task 1); `state.ReadActiveRuns(stateDir string) (map[string]state.ActiveRun, error)` (Plan 2/3 — prunes dead pids on read).
- Produces: `ReconcileDeadRuns(stateDir, nowISO string) (changed bool, err error)`. The HUD's `/api/library` handler (Task 8) calls this before serving.

**Why:** a run whose orchestrator process died leaves its `library.json` entry stuck at `state:"live"`. Upstream flips `live → died` on every `/api/library` poll by checking whether the entry's `current_run_id` still appears in the pid-pruned `active-runs.json` (Explore map §B.5). Liveness truth is pid liveness of the recorded process, already implemented in `state.ReadActiveRuns` (Plan 2/3, which prunes dead pids incl. the pid≤0 guard from Plan 3 Task 4). `nowISO` is passed in (not read from a clock) so the function is deterministic under test — the caller stamps the time.

- [ ] **Step 1: Write the failing test**

Create `internal/artifact/reconcile_test.go`:

```go
package artifact

import (
	"os"
	"testing"

	"github.com/corruptmemory/ringer/internal/state"
)

func TestReconcileFlipsDeadLiveEntries(t *testing.T) {
	dir := t.TempDir()
	// A live entry whose run IS registered+alive, and one whose run is not.
	if err := WriteLibrary(dir, Library{Artifacts: map[string]Entry{
		"alive": {State: "live", CurrentRunID: "alive-1", Identity: "j"},
		"gone":  {State: "live", CurrentRunID: "gone-1", Identity: "j"},
		"done":  {State: "pass", CurrentRunID: "done-1", Identity: "j"}, // not live: untouched
	}}); err != nil {
		t.Fatal(err)
	}
	// Register only "alive-1" with our own (live) pid.
	if err := state.RegisterActiveRun(dir, "alive-1", "j", "alive", "/wd", os.Getpid(), "2026-07-09T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	changed, err := ReconcileDeadRuns(dir, "2026-07-09T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected a change (gone-1 not registered)")
	}
	lib := ReadLibrary(dir)
	if lib.Artifacts["alive"].State != "live" {
		t.Fatalf("registered-live entry must stay live: %+v", lib.Artifacts["alive"])
	}
	if lib.Artifacts["gone"].State != "died" {
		t.Fatalf("unregistered live entry must flip to died: %+v", lib.Artifacts["gone"])
	}
	if lib.Artifacts["gone"].UpdatedAt != "2026-07-09T12:00:00Z" {
		t.Fatalf("died flip must stamp the passed-in time: %+v", lib.Artifacts["gone"])
	}
	if lib.Artifacts["done"].State != "pass" {
		t.Fatalf("non-live entry must be untouched: %+v", lib.Artifacts["done"])
	}
}

func TestReconcileNoChangeWhenAllAccounted(t *testing.T) {
	dir := t.TempDir()
	if err := WriteLibrary(dir, Library{Artifacts: map[string]Entry{
		"done": {State: "pass", CurrentRunID: "d-1"},
	}}); err != nil {
		t.Fatal(err)
	}
	changed, err := ReconcileDeadRuns(dir, "2026-07-09T12:00:00Z")
	if err != nil || changed {
		t.Fatalf("no live entries → no change; got changed=%v err=%v", changed, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — `undefined: ReconcileDeadRuns`.

- [ ] **Step 3: Implement**

Create `internal/artifact/reconcile.go`:

```go
package artifact

import (
	"github.com/corruptmemory/ringer/internal/state"
)

// ReconcileDeadRuns flips every library entry still marked state:"live" to
// "died" when its current_run_id is no longer present in the pid-pruned
// active-runs registry — a run whose orchestrator process exited without a
// clean finish. Mirrors upstream reconcile_artifact_library_dead_runs
// (only inspects live entries; liveness truth is state.ReadActiveRuns's pid
// prune). nowISO is stamped onto flipped entries. Returns whether anything
// changed; only rewrites library.json when it did.
func ReconcileDeadRuns(stateDir, nowISO string) (bool, error) {
	active, err := state.ReadActiveRuns(stateDir)
	if err != nil {
		return false, err
	}
	lib := ReadLibrary(stateDir)
	changed := false
	for name, entry := range lib.Artifacts {
		if entry.State != "live" {
			continue
		}
		if _, ok := active[entry.CurrentRunID]; entry.CurrentRunID != "" && ok {
			continue
		}
		entry.State = "died"
		entry.UpdatedAt = nowISO
		lib.Artifacts[name] = entry
		changed = true
	}
	if changed {
		if err := WriteLibrary(stateDir, lib); err != nil {
			return false, err
		}
	}
	return changed, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/artifact
git commit -m "feat(artifact): dead-run reconciliation (live->died via active-runs pid liveness)"
```

---

### Task 3: Per-task timing on the run-state schema

**Files:**
- Modify: `internal/state/state.go` (`TaskView`)
- Modify: `internal/runner/runner.go` (populate the timestamps)
- Test: `internal/state/state_test.go`, `internal/runner/runner_test.go`

**Interfaces:**
- Consumes: existing `state.TaskView` (`Key, Engine, Model, Status, Attempt, Tokens, Verified, LogPath`); the actor's `setStatus`/`setResult` in `internal/runner/actor.go`.
- Produces: `TaskView` gains `StartedAt string json:"started_at"` and `EndedAt string json:"ended_at"` (RFC3339, `""` when unset). The `/api/runs` adapter (Task 5) derives each task's `elapsed_s` from these; `ringside.html` reads `task.elapsed_s` (dashboard/ringside.html:947).

**Why:** `ringside.html` renders per-task elapsed time (`formatDuration(task.elapsed_s)`), but the Plan-2 Go `TaskView` carries no per-task timing — only run-level `StartedAt`/`UpdatedAt`. Without this, every task shows `0s`. The runner already times each attempt (`attemptStart` in `runTask`); recording the first-attempt start and the final end onto the TaskView is a small, contained addition. This is the schema-owner (Go-authoritative `runs/<id>.json`) extending its own schema to carry what the HUD needs — legitimate per §9.4.

- [ ] **Step 1: Write the failing tests**

Append to `internal/state/state_test.go`:

```go
func TestTaskViewTimingRoundTrips(t *testing.T) {
	stateDir := t.TempDir()
	s := RunState{
		RunID: "r1", RunName: "r", Identity: "j", StartedAt: "2026-07-09T00:00:00Z",
		Tasks: []TaskView{{Key: "a", Status: "passed", StartedAt: "2026-07-09T00:00:01Z", EndedAt: "2026-07-09T00:00:04Z"}},
	}
	if err := WriteRunState(stateDir, s); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "runs", "r1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]any
	_ = json.Unmarshal(data, &probe)
	task0 := probe["tasks"].([]any)[0].(map[string]any)
	for _, k := range []string{"started_at", "ended_at"} {
		if _, ok := task0[k]; !ok {
			t.Fatalf("TaskView missing frozen timing key %q: %v", k, task0)
		}
	}
}
```

Append to `internal/runner/runner_test.go` (extend the existing mock-engine E2E's assertions, or add a focused one — the passing task's TaskView must carry non-empty timing):

```go
func TestRunPopulatesTaskTiming(t *testing.T) {
	ringerBin := buildRingerBinary(t)
	workdir := filepath.Join(t.TempDir(), "w")
	stateDir := t.TempDir()
	m := &manifest.Manifest{
		RunName: "timing", Workdir: workdir,
		Tasks: []manifest.Task{
			{Key: "a", Engine: "mock", TimeoutS: 30,
				Spec: "MOCK_FILE: a.txt\nhi\nMOCK_END", Check: "test -f a.txt", ExpectFiles: []string{"a.txt"}},
		},
	}
	engines := map[string]config.EngineConfig{"mock": {Bin: ringerBin, ArgsTemplate: []string{"mock-worker", "{spec}"}}}
	res, err := Run(context.Background(), Options{
		Manifest: m, Engines: engines, StateDir: stateDir,
		Identity: "j", Stdout: io.Discard, Logger: logging.Default(),
	})
	if err != nil || !res.AllPassed {
		t.Fatalf("run: err=%v res=%+v", err, res)
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "runs", res.RunID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var s state.RunState
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.Tasks[0].StartedAt == "" || s.Tasks[0].EndedAt == "" {
		t.Fatalf("task timing not populated: %+v", s.Tasks[0])
	}
}
```

Add missing imports to the runner test (`state`, `context`, `io`, etc. — check the block).

- [ ] **Step 2: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — `TaskView` has no `StartedAt`/`EndedAt`; the runner test finds them empty.

- [ ] **Step 3: Implement the schema field**

In `internal/state/state.go`, extend `TaskView`:

```go
type TaskView struct {
	Key       string `json:"key"`
	Engine    string `json:"engine"`
	Model     string `json:"model"`
	Status    string `json:"status"` // pending|running|passed|failed|timeout
	Attempt   int    `json:"attempt"`
	Tokens    int64  `json:"tokens"`
	Verified  string `json:"verified"`
	LogPath   string `json:"log_path"`
	StartedAt string `json:"started_at"` // RFC3339, first-attempt start ("" until running)
	EndedAt   string `json:"ended_at"`   // RFC3339, final outcome time ("" until finished)
}
```

- [ ] **Step 4: Populate timing in the runner**

The actor owns the TaskView, so timing is set through it. Add two actor ops mirroring the existing ones. In `internal/runner/actor.go`, wherever `setStatus`/`setResult` mutate a task, stamp times: `setStatus` sets `StartedAt` if empty (first transition to running); `setResult` sets `EndedAt`. Since the actor's op handlers already receive the run and task, extend them to accept an RFC3339 timestamp:

In `internal/runner/runner.go` `runTask`, pass the times you already compute. Add `"time"` usage: at the first attempt's `a.setStatus(task.Key, "running", attempt)` call, the actor stamps `StartedAt` if unset; after the loop, when calling `a.setResult(...)`, stamp `EndedAt`. Concretely, change the actor's status/result command structs to carry a `ts string` and, in the `run()` switch, set `tv.StartedAt` (only if `tv.StartedAt == ""`) on the status op and `tv.EndedAt` on the result op. Then in `runTask`:

```go
		a.setStatus(task.Key, "running", attempt, time.Now().UTC().Format(time.RFC3339))
```

and at the end:

```go
	a.setResult(task.Key, verdictToStatus(verdict), tokens, task.Verified, logPath, time.Now().UTC().Format(time.RFC3339))
```

Update the `setStatus`/`setResult` method signatures and their `actorCmd`/`actorOp` payloads accordingly (add a `ts string` field to the relevant command struct; the `run()` switch assigns it). Keep the seed-at-construction path (`newActor`) leaving `StartedAt`/`EndedAt` as `""`.

(If the current `setStatus`/`setResult` signatures are referenced by tests, update those call sites too — search `setStatus(`/`setResult(` in `internal/runner`.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS. The E2E's passing task now has non-empty `StartedAt`/`EndedAt`.

- [ ] **Step 6: Commit**

```bash
git add internal/state internal/runner
git commit -m "feat(state): per-task started_at/ended_at timing for the HUD adapter"
```

---

### Task 4: HUD server skeleton — chi router, embedded (models-baked) ringside, `/`, `hud` subcommand

**Files:**
- Create: `internal/hud/assets/ringside.html` (generated — see Step 1), `internal/hud/embed.go`, `internal/hud/server.go`
- Create: `cmd/ringer/hud.go`
- Modify: `go.mod`/`go.sum` (add chi)
- Test: `internal/hud/server_test.go`

**Interfaces:**
- Consumes: `logging.Logger`.
- Produces:
  - `hud.Server` with `New(stateDir string, lg logging.Logger) *Server`, `(*Server) Handler() http.Handler` (the chi router — tests hit this via `httptest`), `(*Server) ListenAndServe(port int) error` (binds `127.0.0.1:<port>`, fails if taken).
  - `hud.RingsideHTML []byte` (the embedded baked asset).
  - CLI `ringer hud [--port N] [--no-open]` that binds and blocks forever.

**Why:** the persistent :8700 Ringside server. The asset is embedded with the Models tab **baked in** (spec §8) — upstream spliced it at serve time via `inject_models_tab_into_ringside_html` (ringer.py ~3675); the Go binary bakes it once into the committed asset. The cleanest way to produce a byte-faithful baked asset is to run the existing Python injector once and commit its output.

- [ ] **Step 1: Generate the baked asset**

Produce the models-baked Ringside HTML by running the existing Python injector against the committed source, and place it where `go:embed` will pick it up:

```bash
mkdir -p internal/hud/assets
python3 - <<'PY'
import importlib.util, pathlib
spec = importlib.util.spec_from_file_location("ringer", "ringer.py")
mod = importlib.util.module_from_spec(spec); spec.loader.exec_module(mod)
html = mod.read_ringside_html()  # reads dashboard/ringside.html + injects the Models tab
assert 'id="models-panel"' in html, "models tab not injected"
pathlib.Path("internal/hud/assets/ringside.html").write_text(html, encoding="utf-8")
print("baked", len(html), "bytes")
PY
```

If `python3 ringer.py` cannot be imported cleanly (heavy imports), fall back to reading `inject_models_tab_into_ringside_html` (ringer.py ~3675) and applying its four `str.replace(..., 1)` splices to `dashboard/ringside.html` by hand, then verify `id="models-panel"` appears exactly once. Either way, the committed `internal/hud/assets/ringside.html` is the models-baked asset. Do NOT modify or delete `dashboard/ringside.html` (Python still reads it until the Plan-5 cutover).

- [ ] **Step 2: Write the failing test**

Create `internal/hud/server_test.go`:

```go
package hud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/logging"
)

func newTestServer(t *testing.T, stateDir string) http.Handler {
	t.Helper()
	return New(stateDir, logging.Default()).Handler()
}

func TestRootServesBakedRingside(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="models-panel"`) {
		t.Fatal("served ringside is missing the baked Models tab")
	}
	if !strings.Contains(body, `id="artifacts-panel"`) {
		t.Fatal("served ringside is missing the artifacts panel (wrong asset?)")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `./build.sh --test`
Expected: FAIL — package `internal/hud` does not exist.

- [ ] **Step 4: Add chi + embed + server**

```bash
go get github.com/go-chi/chi/v5@latest
```

Create `internal/hud/embed.go`:

```go
package hud

import _ "embed"

// RingsideHTML is the committed Ringside dashboard asset with the Models tab
// baked in (spec §8). Served verbatim at GET /.
//
//go:embed assets/ringside.html
var RingsideHTML []byte
```

Create `internal/hud/server.go`:

```go
// Package hud serves the persistent Ringside dashboard on 127.0.0.1:8700.
// It reads the Go-authoritative run-state (internal/state) and the artifact
// tree (internal/artifact) and adapts them into the frozen JSON shapes
// ringside.html consumes. Single fixed port, fail if taken (as upstream).
package hud

import (
	"fmt"
	"net"
	"net/http"

	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/go-chi/chi/v5"
)

// DefaultPort is the fixed Ringside port.
const DefaultPort = 8700

// Server serves Ringside for one state directory.
type Server struct {
	stateDir string
	lg       logging.Logger
}

// New builds a Server rooted at stateDir.
func New(stateDir string, lg logging.Logger) *Server {
	if lg == nil {
		lg = logging.Default()
	}
	return &Server{stateDir: stateDir, lg: lg}
}

// Handler builds the chi router. Exposed for httptest; ListenAndServe wraps it.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Get("/", s.handleRoot)
	r.Get("/api/runs", s.handleRuns)
	r.Get("/api/models", s.handleModels)
	r.Get("/api/library", s.handleLibrary)
	r.Get("/api/open-folder", s.handleOpenFolder)
	r.Get("/artifacts/*", s.handleArtifacts)
	r.Get("/logs/*", s.handleLogs)
	return r
}

// ListenAndServe binds 127.0.0.1:<port> and serves until error. It fails
// loudly if the port is already in use (no fallback scan, as upstream).
func (s *Server) ListenAndServe(port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("could not start Ringside on %s; that port may already be in use: %w", addr, err)
	}
	s.lg.Infof("Ringside: http://%s", addr)
	return http.Serve(ln, s.Handler())
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(RingsideHTML)
}

// writeJSON serializes v as the frozen JSON contract: application/json,
// no-store, so a poller never gets a cached run snapshot.
func (s *Server) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := jsonEncode(w, v); err != nil {
		s.lg.Warnf("hud: encode json: %v", err)
	}
}
```

Add a tiny `jsonEncode` helper (in `server.go` or a shared file) so handlers share it:

```go
import "encoding/json"

func jsonEncode(w http.ResponseWriter, v any) error {
	enc := json.NewEncoder(w)
	return enc.Encode(v)
}
```

Create the stub handlers the router references, so the package compiles with just `/` working; each later task (5-9) deletes one stub and implements it for real. To keep tasks independently reviewable, define the six not-yet-implemented handlers as returning 501 for now:

```go
func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request)       { http.Error(w, "not implemented", http.StatusNotImplemented) }
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request)     { http.Error(w, "not implemented", http.StatusNotImplemented) }
func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request)    { http.Error(w, "not implemented", http.StatusNotImplemented) }
func (s *Server) handleOpenFolder(w http.ResponseWriter, r *http.Request) { http.Error(w, "not implemented", http.StatusNotImplemented) }
func (s *Server) handleArtifacts(w http.ResponseWriter, r *http.Request)  { http.Error(w, "not implemented", http.StatusNotImplemented) }
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request)       { http.Error(w, "not implemented", http.StatusNotImplemented) }
```

(Each subsequent task deletes one stub and implements it for real in its own file.)

- [ ] **Step 5: Add the `hud` subcommand**

Create `cmd/ringer/hud.go`:

```go
package main

import (
	"fmt"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/hud"
	"github.com/corruptmemory/ringer/internal/logging"
)

type hudCmd struct {
	Port   int  `long:"port" description:"Ringside port (default 8700)"`
	NoOpen bool `long:"no-open" description:"do not open a browser"`
}

func (c *hudCmd) Execute(args []string) error {
	cfgPath := opts.Config
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	lvl, err := resolveLogLevel(opts.LogLevel, cfg)
	if err != nil {
		return fmt.Errorf("--log-level: %w", err)
	}
	lg, err := logging.New(logging.Config{Level: lvl, Format: cfg.Logging.Format})
	if err != nil {
		return err
	}
	port := c.Port
	if port == 0 {
		port = hud.DefaultPort
	}
	// ListenAndServe blocks until the process is killed; a bind failure on
	// the fixed port is a hard error (as upstream — no fallback port).
	return hud.New(cfg.StateDirPath(), lg).ListenAndServe(port)
}

func init() {
	parser.AddCommand("hud", "Serve the Ringside dashboard",
		"Run the persistent Ringside HUD on 127.0.0.1:8700 (single fixed port, fails if taken).",
		&hudCmd{})
}
```

(The `--no-open` flag is accepted for parity with the detached-spawn invocation in Task 10; the `hud` command itself never opens a browser, so it's inert here — the ensure-running path opens the browser once.)

- [ ] **Step 6: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS — `TestRootServesBakedRingside` green; the four stubbed endpoints return 501 (not yet tested).

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/hud cmd/ringer/hud.go
git commit -m "feat(hud): chi server skeleton, embedded models-baked ringside, / route, hud subcommand"
```

---

### Task 5: `/api/runs` — the Go-run-state → ringside adapter

**Files:**
- Create: `internal/hud/runs.go` (replaces the `handleRuns` stub)
- Test: `internal/hud/runs_test.go`

**Interfaces:**
- Consumes: `state.RunState`/`TaskView` incl. Task 3 timing; `state.ReadActiveRuns`; the embedded router (Task 4).
- Produces: `GET /api/runs` → `{"runs": [<adapted run>...], "active": {<run_id>: {...}}}`.

**Why:** this is the task that closes the Plan-2 half-visibility gap. `ringside.html`'s `normalizeRuns` reads `run.state` (`live`/`pass`/`fail`), `run.pass`, `run.fail`, `run.elapsed_s`, `run.tasks[].status`, `run.tasks[].elapsed_s` (dashboard/ringside.html:663-693, 947) — the **Python** schema. The Go run-state has `done:bool` + task `status ∈ pending|running|passed|failed|timeout`. The adapter derives the ringside shape from the Go schema so runs render fully instead of showing `None`. Task-status mapping to ringside's `taskKind` buckets (dashboard/ringside.html:698-706): `passed→pass`, `running→working` (or `retrying` when attempt>1), `failed/timeout→fail`, `pending→waiting`.

- [ ] **Step 1: Write the failing test**

Create `internal/hud/runs_test.go`:

```go
package hud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/corruptmemory/ringer/internal/state"
)

func TestApiRunsAdaptsGoSchema(t *testing.T) {
	dir := t.TempDir()
	// A finished run: one passed, one failed → state "fail", pass=1, fail=1.
	if err := state.WriteRunState(dir, state.RunState{
		RunID: "demo-1", RunName: "demo", Identity: "jim",
		StartedAt: "2026-07-09T00:00:00Z", UpdatedAt: "2026-07-09T00:00:05Z", Done: true,
		Tasks: []state.TaskView{
			{Key: "a", Status: "passed", Tokens: 10, StartedAt: "2026-07-09T00:00:00Z", EndedAt: "2026-07-09T00:00:03Z"},
			{Key: "b", Status: "failed", StartedAt: "2026-07-09T00:00:00Z", EndedAt: "2026-07-09T00:00:05Z"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// A still-running run → state "live".
	if err := state.WriteRunState(dir, state.RunState{
		RunID: "live-1", RunName: "livey", Identity: "jim",
		StartedAt: "2026-07-09T01:00:00Z", UpdatedAt: "2026-07-09T01:00:02Z", Done: false,
		Tasks: []state.TaskView{{Key: "x", Status: "running", Attempt: 1, StartedAt: "2026-07-09T01:00:00Z"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := state.RegisterActiveRun(dir, "live-1", "jim", "livey", "/wd", os.Getpid(), "2026-07-09T01:00:00Z"); err != nil {
		t.Fatal(err)
	}

	srv := New(dir, nil).Handler()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("cache-control = %q, want no-store", cc)
	}
	var payload struct {
		Runs []struct {
			RunID    string  `json:"run_id"`
			RunName  string  `json:"run_name"`
			State    string  `json:"state"`
			Pass     int     `json:"pass"`
			Fail     int     `json:"fail"`
			ElapsedS float64 `json:"elapsed_s"`
			Tasks    []struct {
				Key      string  `json:"key"`
				Status   string  `json:"status"`
				ElapsedS float64 `json:"elapsed_s"`
			} `json:"tasks"`
		} `json:"runs"`
		Active map[string]struct {
			PID     int    `json:"pid"`
			RunName string `json:"run_name"`
			Workdir string `json:"workdir"`
		} `json:"active"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body.String())
	}
	byID := map[string]int{}
	for i, r := range payload.Runs {
		byID[r.RunID] = i
	}
	fin := payload.Runs[byID["demo-1"]]
	if fin.State != "fail" || fin.Pass != 1 || fin.Fail != 1 {
		t.Fatalf("finished run adapted wrong: %+v", fin)
	}
	if fin.ElapsedS != 5 {
		t.Fatalf("run elapsed_s = %v, want 5 (updated-started)", fin.ElapsedS)
	}
	if fin.Tasks[0].Status != "pass" {
		t.Fatalf("task status not mapped to ringside bucket: %q", fin.Tasks[0].Status)
	}
	if fin.Tasks[0].ElapsedS != 3 {
		t.Fatalf("task elapsed_s = %v, want 3", fin.Tasks[0].ElapsedS)
	}
	live := payload.Runs[byID["live-1"]]
	if live.State != "live" {
		t.Fatalf("running run must be state live: %+v", live)
	}
	if _, ok := payload.Active["live-1"]; !ok {
		t.Fatal("active map must carry the registered run")
	}
	if payload.Active["live-1"].Workdir != "/wd" {
		t.Fatal("active entry must preserve Python-parity workdir field")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./build.sh --test`
Expected: FAIL — `/api/runs` returns 501 from the stub.

- [ ] **Step 3: Implement the adapter**

Delete the `handleRuns` stub from `server.go`. Create `internal/hud/runs.go`:

```go
package hud

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/corruptmemory/ringer/internal/state"
)

// hudRunsLimit caps how many recent runs /api/runs returns (upstream: 12).
const hudRunsLimit = 12

// runView is the ringside-shaped run object normalizeRuns reads by name.
type runView struct {
	RunID     string     `json:"run_id"`
	RunName   string     `json:"run_name"`
	Identity  string     `json:"identity"`
	StartedAt string     `json:"started_at"`
	State     string     `json:"state"` // live | pass | fail
	Pass      int        `json:"pass"`
	Fail      int        `json:"fail"`
	ElapsedS  float64    `json:"elapsed_s"`
	Tokens    int64      `json:"tokens"`
	Tasks     []taskView `json:"tasks"`
}

type taskView struct {
	Key      string  `json:"key"`
	Engine   string  `json:"engine"`
	Model    string  `json:"model"`
	Status   string  `json:"status"` // ringside bucket: pass|working|retry|fail|waiting
	ElapsedS float64 `json:"elapsed_s"`
	Tokens   int64   `json:"tokens"`
	Verified string  `json:"verified"`
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	states := s.scanRunStates()
	runs := make([]runView, 0, len(states))
	for _, rs := range states {
		runs = append(runs, adaptRun(rs))
	}
	active, err := state.ReadActiveRuns(s.stateDir)
	if err != nil {
		s.lg.Warnf("hud: read active runs: %v", err)
		active = map[string]state.ActiveRun{}
	}
	s.writeJSON(w, map[string]any{"runs": runs, "active": active})
}

// scanRunStates reads <stateDir>/runs/*.json, newest-mtime first, capped.
func (s *Server) scanRunStates() []state.RunState {
	dir := filepath.Join(s.stateDir, "runs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type stamped struct {
		mod time.Time
		rs  state.RunState
	}
	var out []stamped
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		full := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		var rs state.RunState
		if err := jsonUnmarshal(data, &rs); err != nil || rs.RunID == "" {
			continue
		}
		out = append(out, stamped{info.ModTime(), rs})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mod.After(out[j].mod) })
	if len(out) > hudRunsLimit {
		out = out[:hudRunsLimit]
	}
	res := make([]state.RunState, len(out))
	for i, s := range out {
		res[i] = s.rs
	}
	return res
}

// adaptRun transforms a Go run-state into the ringside run shape.
func adaptRun(rs state.RunState) runView {
	pass, fail := 0, 0
	var tokens int64
	tasks := make([]taskView, 0, len(rs.Tasks))
	for _, t := range rs.Tasks {
		switch t.Status {
		case "passed":
			pass++
		case "failed", "timeout":
			fail++
		}
		if t.Tokens > 0 {
			tokens += t.Tokens
		}
		tasks = append(tasks, taskView{
			Key: t.Key, Engine: t.Engine, Model: t.Model,
			Status:   mapTaskStatus(t.Status, t.Attempt),
			ElapsedS: elapsed(t.StartedAt, t.EndedAt),
			Tokens:   t.Tokens, Verified: t.Verified,
		})
	}
	runState := "live"
	if rs.Done {
		if fail > 0 {
			runState = "fail"
		} else {
			runState = "pass"
		}
	}
	return runView{
		RunID: rs.RunID, RunName: rs.RunName, Identity: rs.Identity,
		StartedAt: rs.StartedAt, State: runState, Pass: pass, Fail: fail,
		ElapsedS: elapsed(rs.StartedAt, rs.UpdatedAt), Tokens: tokens, Tasks: tasks,
	}
}

// mapTaskStatus maps a Go task status into ringside's taskKind bucket
// (dashboard/ringside.html:698-706): passed→pass, running→working (or retry
// on a second attempt), failed/timeout→fail, everything else→waiting.
func mapTaskStatus(status string, attempt int) string {
	switch status {
	case "passed":
		return "pass"
	case "running":
		if attempt > 1 {
			return "retry"
		}
		return "working"
	case "failed", "timeout":
		return "fail"
	default:
		return "waiting"
	}
}

// elapsed returns end-start in seconds, or 0 when either bound is unparseable
// (a still-running run/task whose end is "" reads as 0, which ringside renders
// as a dash/0s rather than a wrong number).
func elapsed(startISO, endISO string) float64 {
	start, err1 := time.Parse(time.RFC3339, startISO)
	end, err2 := time.Parse(time.RFC3339, endISO)
	if err1 != nil || err2 != nil {
		return 0
	}
	d := end.Sub(start).Seconds()
	if d < 0 {
		return 0
	}
	return d
}
```

Add a shared `jsonUnmarshal` next to `jsonEncode` in `server.go`:

```go
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `./build.sh --test`
Expected: PASS — the finished run adapts to `state:"fail"`, pass/fail counts, run+task `elapsed_s`; the live run to `state:"live"`; `active` carries the Python-parity `workdir`.

- [ ] **Step 5: Commit**

```bash
git add internal/hud
git commit -m "feat(hud): /api/runs adapts Go run-state to the ringside schema (closes Plan-2 half-visibility)"
```

---

### Task 6: `/logs/<run_id>/<task_key>` — 64KB worker-log byte-tail

**Files:**
- Create: `internal/hud/logs.go` (replaces the `handleLogs` stub)
- Test: `internal/hud/logs_test.go`

**Interfaces:**
- Consumes: `state.RunState` (reads `runs/<run_id>.json`, finds the task, uses `TaskView.LogPath`).
- Produces: `GET /logs/<run_id>/<task_key>` → `text/plain; charset=utf-8`, last 64KB of the worker log.

**Why:** Ringside's log viewer fetches the tail of a task's worker log. The tail is a **byte** window (last `WORKER_LOG_TAIL_BYTES = 64*1024` bytes, then decode) — it can start mid-UTF8, so lossy decode is expected (Explore map §A.2). The `run_id` path component must not escape the `runs` dir (guard: the resolved run-state path's parent must be `<stateDir>/runs`). The task's log lives at `TaskView.LogPath` (Go always sets it to `<workdir>/logs/<key>.worker.log`).

- [ ] **Step 1: Write the failing test**

Create `internal/hud/logs_test.go`:

```go
package hud

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/state"
)

func TestLogsTail(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "wlogs")
	_ = os.MkdirAll(logDir, 0o755)
	logPath := filepath.Join(logDir, "a.worker.log")
	// 70KB of content; the tail must return only the last 64KB.
	head := strings.Repeat("H", 6*1024)
	tail := strings.Repeat("T", 64*1024)
	if err := os.WriteFile(logPath, []byte(head+tail), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.WriteRunState(dir, state.RunState{
		RunID: "run-1", RunName: "r",
		Tasks: []state.TaskView{{Key: "a", Status: "passed", LogPath: logPath}},
	}); err != nil {
		t.Fatal(err)
	}
	srv := New(dir, nil).Handler()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/logs/run-1/a", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type = %q", ct)
	}
	body := rec.Body.String()
	if len(body) != 64*1024 {
		t.Fatalf("tail length = %d, want 65536", len(body))
	}
	if strings.Contains(body, "H") {
		t.Fatal("tail must not include the head beyond the 64KB window")
	}

	// Unknown task / run → 404.
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/logs/run-1/nope", nil))
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("unknown task status = %d, want 404", rec2.Code)
	}

	// Path-escape run_id → 404, never reads outside the runs dir.
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, httptest.NewRequest(http.MethodGet, "/logs/..%2f..%2fetc/a", nil))
	if rec3.Code != http.StatusNotFound {
		t.Fatalf("escape run_id status = %d, want 404", rec3.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./build.sh --test`
Expected: FAIL — stub returns 501.

- [ ] **Step 3: Implement**

Delete the `handleLogs` stub. Create `internal/hud/logs.go`:

```go
package hud

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/corruptmemory/ringer/internal/state"
	"github.com/go-chi/chi/v5"
)

// workerLogTailBytes is the size of the byte-window tail served for a worker
// log (frozen: WORKER_LOG_TAIL_BYTES).
const workerLogTailBytes = 64 * 1024

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	rest := chi.URLParam(r, "*") // "<run_id>/<task_key>"
	runID, taskKey, ok := strings.Cut(rest, "/")
	if !ok || runID == "" || taskKey == "" {
		http.NotFound(w, r)
		return
	}
	logPath, ok := s.taskLogPath(runID, taskKey)
	if !ok {
		http.NotFound(w, r)
		return
	}
	tail, err := tailBytes(logPath, workerLogTailBytes)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(tail)
}

// taskLogPath resolves the worker log for (runID, taskKey) from the run-state
// file, refusing a runID that escapes the runs directory.
func (s *Server) taskLogPath(runID, taskKey string) (string, bool) {
	runsRoot, err := filepath.Abs(filepath.Join(s.stateDir, "runs"))
	if err != nil {
		return "", false
	}
	candidate, err := filepath.Abs(filepath.Join(runsRoot, runID+".json"))
	if err != nil {
		return "", false
	}
	if filepath.Dir(candidate) != runsRoot { // runID contained a separator / traversal
		return "", false
	}
	data, err := os.ReadFile(candidate)
	if err != nil {
		return "", false
	}
	var rs state.RunState
	if err := jsonUnmarshal(data, &rs); err != nil {
		return "", false
	}
	for _, t := range rs.Tasks {
		if t.Key == taskKey && t.LogPath != "" {
			return t.LogPath, true
		}
	}
	return "", false
}

// tailBytes returns the last max bytes of the file (byte window, may start
// mid-rune — the caller serves it as text/plain, matching upstream).
func tailBytes(path string, max int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, os.ErrNotExist
	}
	size := info.Size()
	start := int64(0)
	if size > int64(max) {
		start = size - int64(max)
	}
	if _, err := f.Seek(start, 0); err != nil {
		return nil, err
	}
	buf := make([]byte, size-start)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
```

(`taskLogPath` reuses `state.RunState` directly — no local mirror type — and `tailBytes` needs `io.ReadFull`; both imports are in the block above.)

- [ ] **Step 4: Run test to verify it passes**

Run: `./build.sh --test`
Expected: PASS — 64KB tail, 404 on unknown task and on escaping run_id.

- [ ] **Step 5: Commit**

```bash
git add internal/hud
git commit -m "feat(hud): /logs/<run_id>/<task_key> 64KB worker-log byte-tail"
```

---

### Task 7: `/artifacts/<path>` + `/api/open-folder` (Linux xdg-open fix)

**Files:**
- Create: `internal/hud/artifacts.go` (replaces the `handleArtifacts` + `handleOpenFolder` stubs)
- Test: `internal/hud/artifacts_test.go`

**Interfaces:**
- Consumes: `artifact.ArtifactsDir`, `artifact.SanitizeName` (Task 1).
- Produces: `GET /artifacts/<path>` (serves files under the artifact tree, traversal-guarded, per-extension content type) and `GET /api/open-folder?run=<run_id>` (opens the deliverables dir in the OS file manager).

**Why:** `/artifacts/<path>` serves the zero-LLM HTML pages and deliverables Ringside links to. The traversal guard resolves the requested path under the artifact root and rejects anything that escapes or equals the root (Explore map §A.2). `/api/open-folder` is the **spec-mandated per-OS fix** (spec §8): Linux uses `xdg-open`, macOS `open` — upstream was macOS-only. It opens `<artifacts>/deliverables/<run_id>` (falling back to the artifacts root), guarded against traversal.

- [ ] **Step 1: Write the failing test**

Create `internal/hud/artifacts_test.go`:

```go
package hud

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/artifact"
)

func TestArtifactsServeAndGuard(t *testing.T) {
	dir := t.TempDir()
	art := artifact.ArtifactsDir(dir)
	_ = os.MkdirAll(filepath.Join(art, "live"), 0o755)
	if err := os.WriteFile(filepath.Join(art, "live", "demo.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(art, "library.json"), []byte(`{"artifacts":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A secret OUTSIDE the artifact tree that traversal must never reach.
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := New(dir, nil).Handler()

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/artifacts/live/demo.html", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("html serve: code=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}

	recJSON := httptest.NewRecorder()
	srv.ServeHTTP(recJSON, httptest.NewRequest(http.MethodGet, "/artifacts/library.json", nil))
	if recJSON.Code != http.StatusOK || recJSON.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("library.json serve: code=%d ct=%q", recJSON.Code, recJSON.Header().Get("Content-Type"))
	}

	// Traversal attempt → 404, never serves the out-of-tree secret.
	recEsc := httptest.NewRecorder()
	srv.ServeHTTP(recEsc, httptest.NewRequest(http.MethodGet, "/artifacts/..%2fsecret.txt", nil))
	if recEsc.Code != http.StatusNotFound {
		t.Fatalf("traversal status = %d, want 404", recEsc.Code)
	}

	// Missing file → 404.
	recMiss := httptest.NewRecorder()
	srv.ServeHTTP(recMiss, httptest.NewRequest(http.MethodGet, "/artifacts/live/nope.html", nil))
	if recMiss.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want 404", recMiss.Code)
	}
}

func TestOpenFolderGuardsTraversal(t *testing.T) {
	dir := t.TempDir()
	srv := New(dir, nil).Handler()
	// A run id that tries to escape the deliverables dir → 404, and never
	// shells out. (We can't assert the opener ran without a real desktop, but
	// we CAN assert the guard rejects an escaping run id.)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/open-folder?run=..%2f..%2f..%2fetc", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("escaping open-folder status = %d, want 404", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./build.sh --test`
Expected: FAIL — stubs return 501.

- [ ] **Step 3: Implement**

Delete the `handleArtifacts` and `handleOpenFolder` stubs. Create `internal/hud/artifacts.go`:

```go
package hud

import (
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/corruptmemory/ringer/internal/artifact"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	rel := chi.URLParam(r, "*")
	full, ok := s.resolveArtifactPath(rel)
	if !ok {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", artifactContentType(full))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// resolveArtifactPath maps a request path under /artifacts/ to a file inside
// the artifact tree, rejecting anything that escapes or equals the root.
func (s *Server) resolveArtifactPath(rel string) (string, bool) {
	root, err := filepath.Abs(artifact.ArtifactsDir(s.stateDir))
	if err != nil {
		return "", false
	}
	// chi has already path-unescaped the wildcard; clean and re-anchor.
	candidate, err := filepath.Abs(filepath.Join(root, filepath.Clean("/"+rel)))
	if err != nil {
		return "", false
	}
	if candidate == root {
		return "", false // the bare root is not a servable file
	}
	if !strings.HasPrefix(candidate, root+string(filepath.Separator)) {
		return "", false // escaped the tree
	}
	return candidate, true
}

// artifactContentType picks a content type by extension: html/json get the
// charset-tagged types ringside expects; others fall back to mime guessing
// then octet-stream.
func artifactContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	}
	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func (s *Server) handleOpenFolder(w http.ResponseWriter, r *http.Request) {
	runID := r.URL.Query().Get("run")
	root, err := filepath.Abs(artifact.ArtifactsDir(s.stateDir))
	if err != nil {
		http.Error(w, "bad state dir", http.StatusInternalServerError)
		return
	}
	target := filepath.Join(root, "deliverables")
	if runID != "" {
		target = filepath.Join(target, artifact.SanitizeName(runID))
	}
	// Guard: the resolved target must stay within the artifact root.
	abs, err := filepath.Abs(target)
	if err != nil || (abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator))) {
		http.NotFound(w, r)
		return
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		abs = root // fall back to opening the whole artifacts tree
	}
	if err := openInFileManager(abs); err != nil {
		s.lg.Warnf("hud: open-folder %s: %v", abs, err)
		http.Error(w, "could not open folder", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// openInFileManager opens a directory in the OS file manager: xdg-open on
// Linux, open on macOS (spec §8 fix — upstream was macOS-only). The opener
// is detached; we do not wait on it.
func openInFileManager(path string) error {
	var name string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "linux":
		name = "xdg-open"
	default:
		return errUnsupportedOpen
	}
	cmd := exec.Command(name, path)
	return cmd.Start()
}

var errUnsupportedOpen = &openError{"open-folder is only supported on Linux (xdg-open) and macOS (open)"}

type openError struct{ msg string }

func (e *openError) Error() string { return e.msg }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `./build.sh --test`
Expected: PASS. (The open-folder happy path shells out to `xdg-open`; in a headless CI that returns an error which the handler surfaces as 500 — the test only asserts the traversal-guard 404, which never reaches the opener.)

- [ ] **Step 5: Commit**

```bash
git add internal/hud
git commit -m "feat(hud): /artifacts serve (traversal-guarded) + /api/open-folder xdg-open/open per-OS fix"
```

---

### Task 8: `/api/library` — reconcile then serve

**Files:**
- Create: `internal/hud/library.go` (replaces the `handleLibrary` stub)
- Test: `internal/hud/library_test.go`

**Interfaces:**
- Consumes: `artifact.ReconcileDeadRuns`, `artifact.ReadLibrary` (Tasks 1-2).
- Produces: `GET /api/library` → the (reconciled) `library.json` content as JSON.

**Why:** Ringside polls `/api/library` every ~2s; upstream reconciles dead runs on **every** poll before serving (Explore map §A.2/§B.5), so a run whose process died flips to `died` in the client without waiting for another run to start. The handler stamps the current time for the reconcile and serves the resulting library.

- [ ] **Step 1: Write the failing test**

Create `internal/hud/library_test.go`:

```go
package hud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/corruptmemory/ringer/internal/artifact"
)

func TestApiLibraryReconcilesThenServes(t *testing.T) {
	dir := t.TempDir()
	// A live entry whose run is NOT in active-runs → must be served as died.
	if err := artifact.WriteLibrary(dir, artifact.Library{Artifacts: map[string]artifact.Entry{
		"demo": {State: "live", CurrentRunID: "demo-1", Identity: "jim"},
	}}); err != nil {
		t.Fatal(err)
	}
	srv := New(dir, nil).Handler()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/library", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("cache-control = %q", cc)
	}
	var lib artifact.Library
	if err := json.Unmarshal(rec.Body.Bytes(), &lib); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if lib.Artifacts["demo"].State != "died" {
		t.Fatalf("dead run not reconciled on serve: %+v", lib.Artifacts["demo"])
	}
	// Frozen top-level key.
	var probe map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &probe)
	if _, ok := probe["artifacts"]; !ok {
		t.Fatal("response missing frozen \"artifacts\" key")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./build.sh --test`
Expected: FAIL — stub returns 501.

- [ ] **Step 3: Implement**

Delete the `handleLibrary` stub. Create `internal/hud/library.go`:

```go
package hud

import (
	"net/http"
	"time"

	"github.com/corruptmemory/ringer/internal/artifact"
)

func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	// Reconcile dead runs on every poll (upstream parity) — non-fatal: a
	// reconcile failure still serves the last-known library.
	if _, err := artifact.ReconcileDeadRuns(s.stateDir, time.Now().UTC().Format(time.RFC3339)); err != nil {
		s.lg.Warnf("hud: reconcile library: %v", err)
	}
	s.writeJSON(w, artifact.ReadLibrary(s.stateDir))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `./build.sh --test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/hud
git commit -m "feat(hud): /api/library reconciles dead runs then serves library.json"
```

---

### Task 9: `/api/models` — valid empty payload (analytics deferred to Plan 5)

**Files:**
- Create: `internal/hud/models.go` (replaces the `handleModels` stub)
- Test: `internal/hud/models_test.go`

**Interfaces:**
- Consumes: nothing (the model-log aggregation is Plan 5).
- Produces: `GET /api/models` → `{"generated_at": "<rfc3339>", "groups": [], "rollup": []}`.

**Why:** `/api/models` is a frozen HUD endpoint (spec §8/§9.5) and the Models tab is baked into the asset, so it must respond with the right SHAPE or the tab errors. The actual aggregation (scoreboard tiers, catalog enrichment, identity resolution) reads the SQLite eval store and is the **Plan 5 (analytics)** subsystem. Serving a valid empty payload now keeps the contract; `ringside.html`'s models view renders an empty state. Upstream's payload is `{"generated_at", "groups", "rollup"}` and surfaces errors in-band (still 200) — we match the success shape with empty arrays.

- [ ] **Step 1: Write the failing test**

Create `internal/hud/models_test.go`:

```go
package hud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApiModelsEmptyShape(t *testing.T) {
	srv := New(t.TempDir(), nil).Handler()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/models?t=123", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range []string{"generated_at", "groups", "rollup"} {
		if _, ok := payload[k]; !ok {
			t.Fatalf("missing frozen key %q: %v", k, payload)
		}
	}
	if groups, ok := payload["groups"].([]any); !ok || len(groups) != 0 {
		t.Fatalf("groups must be an empty array (analytics is Plan 5): %v", payload["groups"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./build.sh --test`
Expected: FAIL — stub returns 501.

- [ ] **Step 3: Implement**

Delete the `handleModels` stub. Create `internal/hud/models.go`:

```go
package hud

import (
	"net/http"
	"time"
)

// handleModels serves the frozen /api/models shape with empty aggregation.
// The model-log analytics (scoreboard tiers, catalog enrichment, identity)
// read the SQLite eval store and land in Plan 5; serving the valid empty
// shape now keeps ringside.html's Models tab from erroring.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"groups":       []any{},
		"rollup":       []any{},
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `./build.sh --test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/hud
git commit -m "feat(hud): /api/models valid empty shape (aggregation deferred to Plan 5 analytics)"
```

---

### Task 10: ensure-HUD-running — probe, spawn detached, open browser once; wire into run/demo

**Files:**
- Create: `cmd/ringer/ensurehud.go`
- Modify: `cmd/ringer/run.go`, `cmd/ringer/demo.go` (the `--no-dashboard` flag descriptions + the call)
- Test: `cmd/ringer/ensurehud_test.go`

**Interfaces:**
- Consumes: `hud.DefaultPort`; `os/exec`, `net/http`.
- Produces: `ensureHUDRunning(stateDir string, port int, lg logging.Logger, openBrowser bool)`. Called by `run`/`demo` unless `--no-dashboard`.

**Why:** upstream's `run` probes `:8700/api/runs` (0.4s timeout); if nothing answers, it spawns a **detached** `ringer hud --no-open --port N` (new session, stdout/stderr → `<stateDir>/hud.log`, stdin closed), polls up to ~3s for it to come up, then opens the browser **only if it wasn't already alive** — the "once per session" behavior is emergent from the long-lived detached process staying alive (Explore map §C). The probe/spawn/browser logic is testable via a real ephemeral-port probe; the browser-open is suppressed in tests.

- [ ] **Step 1: Write the failing test**

Create `cmd/ringer/ensurehud_test.go`:

```go
package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/corruptmemory/ringer/internal/logging"
)

func TestHudProbeDetectsAlive(t *testing.T) {
	// A live server answering /api/runs with 200 → probe true.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/runs" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	// Extract the port httptest bound.
	_, portStr, _ := net.SplitHostPort(ts.Listener.Addr().String())
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	if !hudIsAlive(port) {
		t.Fatal("probe should detect the live server")
	}

	// A port nobody is listening on → probe false.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	_, freeStr, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()
	var freePort int
	fmt.Sscanf(freeStr, "%d", &freePort)
	if hudIsAlive(freePort) {
		t.Fatal("probe should be false on a closed port")
	}
}

func TestEnsureHudRunningSpawnsWhenDead(t *testing.T) {
	// With openBrowser=false and a real ringer binary absent from PATH, this
	// exercises the probe+spawn+poll path without opening a browser. It must
	// not panic or block beyond the poll budget, and must not error out even
	// if the spawned hud never comes up (best-effort, upstream parity).
	stateDir := t.TempDir()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	_, freeStr, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()
	var port int
	fmt.Sscanf(freeStr, "%d", &port)
	// Should return promptly; nothing to assert beyond "does not hang/panic".
	ensureHUDRunning(stateDir, port, logging.Default(), false)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./build.sh --test`
Expected: FAIL — `undefined: hudIsAlive`, `undefined: ensureHUDRunning`.

- [ ] **Step 3: Implement**

Create `cmd/ringer/ensurehud.go`:

```go
package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/corruptmemory/ringer/internal/logging"
)

// hudIsAlive probes 127.0.0.1:<port>/api/runs with a short timeout; true only
// on a 200. Any connection/timeout/non-200 → false (upstream hud_is_alive).
func hudIsAlive(port int) bool {
	client := &http.Client{Timeout: 400 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/runs", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ensureHUDRunning makes the Ringside HUD available: if nothing answers the
// probe, spawn a detached `ringer hud` (new session, output to hud.log, stdin
// closed), poll up to ~3s for it to come up, then open the browser exactly
// once — only when it was not already alive. The "once per session" effect is
// emergent: the detached HUD stays alive, so later runs find it already up and
// skip the browser. Best-effort throughout: a spawn/browser failure is logged,
// never fatal (a run must not fail because the dashboard didn't start).
func ensureHUDRunning(stateDir string, port int, lg logging.Logger, openBrowser bool) {
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	alreadyAlive := hudIsAlive(port)
	if !alreadyAlive {
		if err := spawnDetachedHUD(stateDir, port); err != nil {
			lg.Warnf("hud: spawn detached: %v", err)
		} else {
			for i := 0; i < 20; i++ {
				time.Sleep(150 * time.Millisecond)
				if hudIsAlive(port) {
					break
				}
			}
		}
	}
	if openBrowser && !alreadyAlive && hudIsAlive(port) {
		if err := openInBrowser(url); err != nil {
			lg.Warnf("hud: open browser: %v", err)
		}
	}
	lg.Infof("Ringside: %s", url)
}

// spawnDetachedHUD launches `ringer hud --no-open --port N` in a new session,
// its output appended to <stateDir>/hud.log, stdin closed, surviving this
// process's exit.
func spawnDetachedHUD(stateDir string, port int) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := filepath.Join(stateDir, "hud.log")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	// Note: logFile stays open in the child; the parent closing its own copy
	// after Start is fine (the child holds its own fd).
	defer logFile.Close()
	cmd := exec.Command(self, "hud", "--no-open", "--port", fmt.Sprintf("%d", port))
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // new session — detach
	return cmd.Start()
}

// openInBrowser opens url in the user's browser: xdg-open on Linux, open on
// macOS. Detached; not waited on.
func openInBrowser(url string) error {
	var name string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "linux":
		name = "xdg-open"
	default:
		return fmt.Errorf("no browser opener for %s", runtime.GOOS)
	}
	return exec.Command(name, url).Start()
}
```

- [ ] **Step 4: Wire into run and demo**

In `cmd/ringer/run.go`, update the `NoDashboard` flag description and call `ensureHUDRunning` before the run (only on a real run, not `--dry-run`, and not when `--no-dashboard`). The `runCmd.Execute` already builds a signal context; add the ensure call inside `runManifestFile` after config/logger are built and before `runner.Run` — but `runManifestFile` doesn't currently know about `--no-dashboard`. Thread it through: add a `noDashboard bool` parameter to `runManifestFile` (both `run` and `demo` pass their flag), and near the top (after the logger is built, before the dry-run branch is fine — but skip when `dryRun`):

```go
	if !dryRun && !noDashboard {
		ensureHUDRunning(cfg.StateDirPath(), hud.DefaultPort, lg, true)
	}
```

Update the `NoDashboard` flag descriptions in both `run.go` and `demo.go` from `"accepted; always headless in Plan 2 (no HUD yet)"` to `"do not ensure the Ringside HUD is running / open a browser"`. Update `runCmd.Execute` and `demoCmd.Execute` to pass `c.NoDashboard` into `runManifestFile`, and update `runManifestFile`'s signature: `runManifestFile(ctx context.Context, manifestPath string, maxParallelOverride int, identityFlag string, dryRun, noDashboard bool) error`. Import `internal/hud` in `run.go`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS. `TestHudProbeDetectsAlive` and `TestEnsureHudRunningSpawnsWhenDead` green; the ensure path is best-effort and returns promptly.

- [ ] **Step 6: Manual smoke (optional, local)**

```bash
./ringer hud &          # bind :8700, serves ringside
curl -s 127.0.0.1:8700/api/runs | head -c 200   # {"active":{},"runs":[]}
kill %1
./ringer demo           # ensure-running spawns the HUD + (headful) opens a browser once
```

- [ ] **Step 7: Commit**

```bash
git add cmd/ringer
git commit -m "feat(cmd): ensure-HUD-running (probe -> spawn detached -> open browser once); wire into run/demo"
```

---

## Out of scope → Plan 4b (artifact rendering) and Plan 5 (analytics)

**Plan 4b — Artifact rendering (the write side of §8):** the zero-LLM HTML pages and `library.json` *writing*, wired into the runner's flush tick. Scope: templ components rendering byte-equivalent-in-contract status/report/index/wrapper pages (upstream `render_status_html`, `render_final_report_html`, `render_artifact_index_html`, `render_file_wrapper_html`), pinned with golden tests; the `StateWriter`-equivalent that writes `<run_id>.html`, `live/<run_name>.html`, `versions/<run_name>/<run_id>.html`, `<run_id>-report.html`, `index.html` on the 1Hz tick + at finish; `library.json` version-entry append + prune (`ARTIFACT_LIBRARY_MAX_VERSIONS=20`); deliverables harvest on PASS (declared `expect_files` + fallback glob, `DELIVERABLE_MAX_BYTES=20MiB`, `FALLBACK_HARVEST_MAX_FILES=8`), copied into `artifacts/deliverables/<run_id>/<task_key>/`. Adds `github.com/a-h/templ`. This is the larger, HTML-heavy half; splitting it keeps both plans reviewable.

**Plan 5 — analytics + cutover:** fills `/api/models` from the SQLite eval store (scoreboard tiers, catalog enrichment, identity), the `models`/`catalog` CLI, `db import`, and the final cutover (delete `ringer.py`, `hud/`, `dashboard/dashboard.html`, `dashboard/ringside.html`; README/SKILL sweep). The Plan-3 out-of-scope carry-forward — `allow_full_access` config gating still unenforced (spec §6 says `full_access` is gated by `allow_full_access`; Plan 2 shipped it ungated) — belongs to this pre-cutover pass.

## Plan-3 follow-ups (fold in opportunistically, not blocking)

Recorded in the Plan-3 doc's status banner; none are HUD-blocking, but a task here may touch adjacent code:
- Redundant host-toolchain double-mount in `internal/isolate/jail.go` (isolate `HostMounts` + jail `writeUnshareMounts` both mount `/usr`,`/etc`) — a one-owner cleanup.
- `<workdir>/.jail` and `<workdir>/.scratch` base dirs left as empty litter after per-key cleanup.
- These are isolation-package cleanups unrelated to the HUD; list them for the final whole-branch review to triage, do not gate Plan 4 on them.


