# Ringer Go Plan 4 — HUD (templ + htmx) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `ringer hud` serves the Ringside dashboard on 127.0.0.1:8700 as a **templ + htmx server-rendered** page — the live runs view renders straight from the Go run-state (no JSON API, no schema adapter), polled every 2s and morphed in — closing the Plan-2 HUD half-visibility gap.

**Architecture:** A new `internal/hud` package owns a `chi` router. `GET /` returns a templ-rendered page (topbar + panels); the live panels carry htmx attributes that poll `GET /hud/runs|library|models` — HTML **fragment** routes rendered by templ components straight from `state.RunState` / `artifact.Library` — and morph the result in via Idiomorph. The 980 lines of Ringside client JS are replaced by server-side rendering; the 521-line stylesheet is lifted verbatim into an embedded `ringside.css`. File-serving routes (`/artifacts`, `/logs`, `/api/open-folder`) are unchanged. `run`/`demo` gain ensure-HUD-running.

**Tech Stack:** Go 1.26, `github.com/go-chi/chi/v5` (router), `github.com/a-h/templ` (type-safe HTML; new dep + `go tool` directive), vendored `htmx` + `idiomorph` (poll+morph, no npm), `go:embed`, existing `internal/{state,config,logging}`.

## Scope boundary

This plan is the HUD **server + live dashboard**. It renders and serves; it does not *write* artifacts. The artifact-writing side (the zero-LLM templ pages, `library.json` writing, deliverables harvest, wired into the runner's flush tick) is **Plan 4b** — the run-card/task-row templ components built here are **shared** with it. `/hud/models` is a **Plan-5** stub (the model-log analytics reads the SQLite store). `dashboard/ringside.html` and `dashboard.html` are **not** deleted here — their CSS/markup is *lifted* into templ/`ringside.css`; the physical deletion is the Plan-5 cutover (§11), because `ringer.py` still reads `ringside.html` until Python is removed.

## Global Constraints

- Build/test ONLY via `./build.sh --test` (never raw `go build`/`go test`/`templ generate`); `gofmt` clean (build.sh enforces). **Edit `.templ` files only** — the generated `*_templ.go` are build artifacts.
- **Frozen (spec §8, amended 2026-07-09):** the HUD is templ+htmx server-rendered — no embedded `ringside.html`, no JSON API, no Go→Python schema adapter. Live updates = htmx polling every 2s + `hx-swap="morph"` (Idiomorph). Single fixed port 127.0.0.1:8700, fail if taken.
- **§9.4 on-disk schemas:** `runs/<id>.json` is **Go-authoritative** (`state.RunState`, `done:bool`); templ reads it directly. `active-runs.json` keeps Python parity (5-field entries incl. `workdir`; pid-prune-on-read). `library.json` schema is frozen and unversioned.
- **File-serving routes unchanged:** `/artifacts/<path>` (traversal-guarded, per-extension content type), `/logs/<run_id>/<task_key>` (last 64 KB byte-tail, `text/plain`), `/api/open-folder` (**`xdg-open` on Linux, `open` on macOS** — spec §8 fix; upstream was macOS-only). `WORKER_LOG_TAIL_BYTES = 64 * 1024`.
- Errors route through the injected `logging.Logger`; never `_ =` an error silently. Handler failures return the right status AND log at Warn/Error; per-request access logging is suppressed.
- New deps allowed: `github.com/go-chi/chi/v5`, `github.com/a-h/templ`. Vendored frontend JS (htmx, idiomorph) lives under `internal/hud/static/vendor/`, refreshed via `build.sh --refresh-htmx` / `--refresh-idiomorph`, embedded with `go:embed`.
- Keep the existing Ringside visual design: the 521-line `<style>` block (dashboard/ringside.html:8-529) is lifted verbatim into `internal/hud/static/ringside.css`; markup *structure* is ported into templ (classes survive); the 980-line inline `<script>` (dashboard/ringside.html:565-1545) is NOT ported — the server renders instead.
- Tests are black-box `httptest` over the router with `t.TempDir()` state dirs, asserting rendered HTML carries the right data + CSS classes.
- `DEFAULT_HUD_PORT = 8700`.

---

## File Structure

| File | Responsibility |
|---|---|
| `build.sh` (modify) | run `go tool templ generate` before build; `--refresh-htmx`/`--refresh-idiomorph` |
| `go.mod`/`go.sum` (modify) | add chi, templ (+ `go tool templ`) |
| `internal/artifact/paths.go` (new) | canonical artifact-tree paths + `SanitizeName` |
| `internal/artifact/library.go` (new) | `library.json` types + `ReadLibrary`/`WriteLibrary` |
| `internal/artifact/reconcile.go` (new) | `ReconcileDeadRuns` (live→died via active-runs pid liveness) |
| `internal/state/state.go` (modify) | `TaskView` gains `StartedAt`/`EndedAt` |
| `internal/runner/{actor,runner}.go` (modify) | stamp the per-task timestamps |
| `internal/hud/static/ringside.css` (new) | lifted Ringside stylesheet |
| `internal/hud/static/vendor/htmx.min.js`, `idiomorph.min.js` (new) | vendored frontend JS |
| `internal/hud/static.go` (new) | `//go:embed static` FS + `/static/*` handler |
| `internal/hud/render.go` (new) | pure Go view helpers (the ex-`normalizeRuns` derivations) |
| `internal/hud/views/layout.templ` (new) | page shell (topbar, panels, htmx wiring) |
| `internal/hud/views/runs.templ` (new) | runs panel → `runcard` → `taskrow` (shared with 4b) |
| `internal/hud/views/library.templ` (new) | artifacts/library panel |
| `internal/hud/views/models.templ` (new) | models panel (Plan-5 stub) |
| `internal/hud/server.go` (new) | `Server`, chi router, `New`, `ListenAndServe`, `Handler` |
| `internal/hud/runs.go` (new) | `GET /` + `GET /hud/runs` (reads run-state, renders) |
| `internal/hud/library.go` (new) | `GET /hud/library` (reconcile + render) |
| `internal/hud/models.go` (new) | `GET /hud/models` (stub) |
| `internal/hud/files.go` (new) | `/artifacts/*`, `/logs/*`, `/api/open-folder`, `/healthz` |
| `cmd/ringer/hud.go` (new) | `hud` subcommand |
| `cmd/ringer/ensurehud.go` (new) | probe/spawn/browser |
| `cmd/ringer/{run,demo}.go` (modify) | wire ensure-HUD-running |

Task order: build tooling first (templ must generate before anything compiles), then the on-disk contract packages (`artifact`, timing) the views read, then the server skeleton + static + layout, then the render helpers, then each fragment route, then the file-serving group, then ensure-running.

---

### Task 1: Build tooling — templ + vendored htmx/idiomorph + static embed

**Files:**
- Modify: `build.sh`, `go.mod`/`go.sum`
- Create: `internal/hud/static/vendor/htmx.min.js`, `internal/hud/static/vendor/idiomorph.min.js`, `internal/hud/static/ringside.css`, `internal/hud/static.go`
- Test: `internal/hud/static_test.go`

**Interfaces:**
- Produces: `hud.staticFS` (embedded `fs.FS` rooted at `static/`); a chi-mountable `staticHandler() http.Handler` serving `/static/*`; the `build.sh` templ-generate step every later task relies on.

**Why:** templ compiles `.templ` → `*_templ.go`, so the build must run `templ generate` before `go build` or nothing in `internal/hud/views` exists. htmx (poll) + idiomorph (morph) are the only frontend JS; per house rules they're vendored under `static/vendor/` and refreshed via `build.sh`. The Ringside stylesheet is lifted verbatim so the look is preserved. This task establishes all three and a working `/static/*` route before any view is written.

- [ ] **Step 1: Add deps + the templ tool**

```bash
go get github.com/go-chi/chi/v5@latest
go get github.com/a-h/templ@latest
go get -tool github.com/a-h/templ/cmd/templ@latest   # adds a `tool` directive (Go 1.24+)
```

Confirm `go.mod` gained a `tool github.com/a-h/templ/cmd/templ` directive and the two `require`s.

- [ ] **Step 2: Wire templ + refresh into build.sh**

Edit `build.sh`. Add `--refresh-htmx` / `--refresh-idiomorph` to the arg loop, and a `templ generate` step before `go vet`. Pin the vendored versions explicitly:

```bash
#!/usr/bin/env bash
# build.sh — the ONLY entry point for building and testing ringer.
set -euo pipefail
cd "$(dirname "$0")"

HTMX_VERSION="2.0.4"
IDIOMORPH_VERSION="0.7.3"
VENDOR_DIR="internal/hud/static/vendor"

RACE=""
RUN_TESTS=0
for arg in "$@"; do
  case "$arg" in
    --test) RUN_TESTS=1 ;;
    --race) RACE="-race" ;;
    --refresh-htmx)
      mkdir -p "$VENDOR_DIR"
      curl -fsSL "https://unpkg.com/htmx.org@${HTMX_VERSION}/dist/htmx.min.js" -o "$VENDOR_DIR/htmx.min.js"
      echo "refreshed htmx ${HTMX_VERSION}"; exit 0 ;;
    --refresh-idiomorph)
      mkdir -p "$VENDOR_DIR"
      curl -fsSL "https://unpkg.com/idiomorph@${IDIOMORPH_VERSION}/dist/idiomorph-ext.min.js" -o "$VENDOR_DIR/idiomorph.min.js"
      echo "refreshed idiomorph ${IDIOMORPH_VERSION}"; exit 0 ;;
    *) echo "usage: ./build.sh [--test [--race]] | [--refresh-htmx] | [--refresh-idiomorph]" >&2; exit 2 ;;
  esac
done

# Generate templ views (*_templ.go) before formatting/vetting/building.
go tool templ generate

UNFORMATTED=$(gofmt -l cmd internal 2>/dev/null || true)
if [ -n "$UNFORMATTED" ]; then
  echo "gofmt needed on:" >&2; echo "$UNFORMATTED" >&2; exit 1
fi

go vet ./...
CGO_ENABLED=0 go build -o ringer ./cmd/ringer

if [ "$RUN_TESTS" = "1" ]; then
  go test $RACE ./...
fi
```

Note for the implementer: `go tool templ generate` scans the module for `.templ` files. With none yet, it is a no-op and exits 0 — so build.sh stays green until Task 4 adds the first `.templ`. `gofmt -l` skips generated `*_templ.go` only if they are formatted; templ emits gofmt-clean output, so no exclusion is needed.

- [ ] **Step 3: Vendor the JS**

```bash
./build.sh --refresh-htmx
./build.sh --refresh-idiomorph
ls -la internal/hud/static/vendor/   # htmx.min.js, idiomorph.min.js present
```

(The idiomorph **ext** build registers an htmx extension so `hx-ext="morph"` + `hx-swap="morph"` work.)

- [ ] **Step 4: Lift the Ringside stylesheet**

Copy the CSS out of the committed dashboard into a first-class stylesheet — the content between (but not including) the `<style>`/`</style>` tags at `dashboard/ringside.html:8-529`:

```bash
sed -n '9,528p' dashboard/ringside.html > internal/hud/static/ringside.css
head -c 200 internal/hud/static/ringside.css   # sanity: starts with the :root/token CSS, not "<style>"
```

Do NOT modify `dashboard/ringside.html` (Python still reads it until the Plan-5 cutover).

- [ ] **Step 5: Write the failing static test**

Create `internal/hud/static_test.go`:

```go
package hud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticServesVendoredAssets(t *testing.T) {
	h := staticHandler()
	for _, tc := range []struct{ path, wantSub, wantCT string }{
		{"/static/vendor/htmx.min.js", "", "javascript"},
		{"/static/vendor/idiomorph.min.js", "", "javascript"},
		{"/static/ringside.css", ":root", "text/css"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", tc.path, rec.Code)
		}
		if tc.wantSub != "" && !strings.Contains(rec.Body.String(), tc.wantSub) {
			t.Fatalf("%s: body missing %q", tc.path, tc.wantSub)
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), tc.wantCT) {
			t.Fatalf("%s: content-type = %q, want ~%q", tc.path, rec.Header().Get("Content-Type"), tc.wantCT)
		}
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `./build.sh --test`
Expected: FAIL — `undefined: staticHandler` (package `internal/hud` has no Go yet).

- [ ] **Step 7: Implement the embed + handler**

Create `internal/hud/static.go`:

```go
// Package hud serves the Ringside dashboard (templ + htmx) on
// 127.0.0.1:8700. The live panels poll /hud/* fragment routes rendered
// straight from the Go run-state; there is no JSON API and no client-side
// schema adaptation.
package hud

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticEmbed embed.FS

// staticFS is the embedded static tree rooted at "static/".
var staticFS = mustSub(staticEmbed, "static")

func mustSub(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err) // embed path is a compile-time constant; failure is a build bug
	}
	return sub
}

// staticHandler serves the embedded static assets under /static/.
func staticHandler() http.Handler {
	return http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `./build.sh --test`
Expected: PASS. (Go's `http.FileServer` sets `Content-Type` by extension: `.js` → `text/javascript`, `.css` → `text/css`.)

- [ ] **Step 9: Commit**

```bash
git add build.sh go.mod go.sum internal/hud
git commit -m "feat(hud): build tooling — templ generate, vendored htmx+idiomorph, lifted ringside.css, /static"
```

---

### Task 2: `internal/artifact` — paths, `library.json` types + reader, dead-run reconcile

**Files:**
- Create: `internal/artifact/paths.go`, `internal/artifact/library.go`, `internal/artifact/reconcile.go`
- Test: `internal/artifact/library_test.go`, `internal/artifact/reconcile_test.go`

**Interfaces:**
- Consumes: `state.ReadActiveRuns(stateDir string) (map[string]state.ActiveRun, error)` (Plan 2/3, pid-prunes on read).
- Produces:
  - `ArtifactsDir(stateDir) string` → `<stateDir>/artifacts`; `LibraryPath(stateDir) string`; `DeliverablesDir(stateDir, runID, taskKey) string`; `SanitizeName(s) string`.
  - Types `Library{Artifacts map[string]Entry}`, `Entry{LivePath, State, Identity, CurrentRunID, UpdatedAt string; Versions []Version}`, `Version{RunID, Path string; ReportPath *string; FinishedAt, Outcome string; TasksPass, TasksFail int; Deliverables []Deliverable}`, `Deliverable{TaskKey, Name, Path string; Bytes int64}`.
  - `ReadLibrary(stateDir) Library`, `WriteLibrary(stateDir, Library) error`, `ReconcileDeadRuns(stateDir, nowISO string) (bool, error)`.

**Why:** `library.json` is a frozen, unversioned contract the HUD library panel (Task 7) reads; these types are shared with the Plan-4b writer. `ReconcileDeadRuns` flips `state:"live"` entries to `"died"` when their `current_run_id` is no longer in the pid-pruned active-runs registry — the HUD runs it on every `/hud/library` poll (upstream parity). `nowISO` is passed in (deterministic under test).

- [ ] **Step 1: Write the failing tests**

Create `internal/artifact/library_test.go`:

```go
package artifact

import (
	"encoding/json"
	"os"
	"testing"
)

func TestPathsCanonical(t *testing.T) {
	if ArtifactsDir("/s") != "/s/artifacts" || LibraryPath("/s") != "/s/artifacts/library.json" {
		t.Fatalf("paths wrong: %q %q", ArtifactsDir("/s"), LibraryPath("/s"))
	}
	if got := DeliverablesDir("/s", "run 1", "task/key"); got != "/s/artifacts/deliverables/run-1/task-key" {
		t.Fatalf("DeliverablesDir = %q (sanitization)", got)
	}
}

func TestReadWriteLibraryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	report := "/x/rid-report.html"
	lib := Library{Artifacts: map[string]Entry{
		"demo": {LivePath: "/x/live/demo.html", State: "pass", Identity: "jim", CurrentRunID: "demo-1", UpdatedAt: "2026-07-09T00:00:00Z",
			Versions: []Version{{RunID: "demo-1", Path: "/x/v/demo-1.html", ReportPath: &report, FinishedAt: "2026-07-09T00:00:00Z", Outcome: "pass", TasksPass: 3,
				Deliverables: []Deliverable{{TaskKey: "a", Name: "out.txt", Path: "/x/out.txt", Bytes: 12}}}}},
	}}
	if err := WriteLibrary(dir, lib); err != nil {
		t.Fatal(err)
	}
	got := ReadLibrary(dir)
	if got.Artifacts["demo"].State != "pass" || len(got.Artifacts["demo"].Versions) != 1 || got.Artifacts["demo"].Versions[0].TasksPass != 3 {
		t.Fatalf("round-trip lost fields: %+v", got.Artifacts["demo"])
	}
	raw, _ := os.ReadFile(LibraryPath(dir))
	var probe map[string]any
	_ = json.Unmarshal(raw, &probe)
	if _, ok := probe["artifacts"].(map[string]any)["demo"].(map[string]any)["live_path"]; !ok {
		t.Fatalf("frozen key live_path missing: %s", raw)
	}
}

func TestReadLibraryMissingOrGarbageIsEmpty(t *testing.T) {
	if lib := ReadLibrary(t.TempDir()); lib.Artifacts == nil || len(lib.Artifacts) != 0 {
		t.Fatalf("missing → empty non-nil map, got %+v", lib)
	}
	dir := t.TempDir()
	_ = os.MkdirAll(ArtifactsDir(dir), 0o755)
	_ = os.WriteFile(LibraryPath(dir), []byte("{ nope"), 0o644)
	if lib := ReadLibrary(dir); len(lib.Artifacts) != 0 {
		t.Fatalf("garbage → empty, got %+v", lib)
	}
}
```

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
	if err := WriteLibrary(dir, Library{Artifacts: map[string]Entry{
		"alive": {State: "live", CurrentRunID: "alive-1"},
		"gone":  {State: "live", CurrentRunID: "gone-1"},
		"done":  {State: "pass", CurrentRunID: "done-1"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := state.RegisterActiveRun(dir, "alive-1", "j", "alive", "/wd", os.Getpid(), "2026-07-09T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	changed, err := ReconcileDeadRuns(dir, "2026-07-09T12:00:00Z")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v, want changed=true", changed, err)
	}
	lib := ReadLibrary(dir)
	if lib.Artifacts["alive"].State != "live" || lib.Artifacts["gone"].State != "died" || lib.Artifacts["done"].State != "pass" {
		t.Fatalf("reconcile wrong: %+v", lib.Artifacts)
	}
	if lib.Artifacts["gone"].UpdatedAt != "2026-07-09T12:00:00Z" {
		t.Fatalf("died flip must stamp the passed-in time: %+v", lib.Artifacts["gone"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — package `internal/artifact` does not exist.

- [ ] **Step 3: Implement `paths.go`**

```go
// Package artifact holds the on-disk artifact-tree contract shared by the
// HUD (which serves + renders it) and the Plan-4b renderer (which writes
// it): the canonical path layout under <state_dir>/artifacts and the
// frozen, unversioned library.json schema.
package artifact

import (
	"path/filepath"
	"regexp"
	"strings"
)

func ArtifactsDir(stateDir string) string { return filepath.Join(stateDir, "artifacts") }
func LibraryPath(stateDir string) string  { return filepath.Join(ArtifactsDir(stateDir), "library.json") }

func DeliverablesDir(stateDir, runID, taskKey string) string {
	return filepath.Join(ArtifactsDir(stateDir), "deliverables", SanitizeName(runID), SanitizeName(taskKey))
}

var unsafeNameRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// SanitizeName maps a run_id/run_name/task_key to one safe path component
// (mirrors upstream sanitize_artifact_name so on-disk paths match).
func SanitizeName(s string) string {
	cleaned := strings.Trim(unsafeNameRe.ReplaceAllString(s, "-"), "-")
	if cleaned == "" {
		return "unnamed"
	}
	return cleaned
}
```

- [ ] **Step 4: Implement `library.go`**

```go
package artifact

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Library struct {
	Artifacts map[string]Entry `json:"artifacts"`
}

type Entry struct {
	LivePath     string    `json:"live_path"`
	State        string    `json:"state"` // live | died | pass | fail
	Identity     string    `json:"identity"`
	CurrentRunID string    `json:"current_run_id"`
	UpdatedAt    string    `json:"updated_at"`
	Versions     []Version `json:"versions"`
}

type Version struct {
	RunID        string        `json:"run_id"`
	Path         string        `json:"path"`
	ReportPath   *string       `json:"report_path"`
	FinishedAt   string        `json:"finished_at"`
	Outcome      string        `json:"outcome"`
	TasksPass    int           `json:"tasks_pass"`
	TasksFail    int           `json:"tasks_fail"`
	Deliverables []Deliverable `json:"deliverables"`
}

type Deliverable struct {
	TaskKey string `json:"task_key"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Bytes   int64  `json:"bytes"`
}

// ReadLibrary loads library.json, degrading a missing or malformed file to
// an empty (non-nil) map (mirrors upstream's {"artifacts": {}} fallback).
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

- [ ] **Step 5: Implement `reconcile.go`**

```go
package artifact

import "github.com/corruptmemory/ringer/internal/state"

// ReconcileDeadRuns flips library entries still marked state:"live" to
// "died" when their current_run_id is no longer in the pid-pruned
// active-runs registry (a run whose orchestrator exited without a clean
// finish). nowISO stamps flipped entries. Rewrites library.json only when
// something changed. Mirrors upstream reconcile_artifact_library_dead_runs.
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

- [ ] **Step 6: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/artifact
git commit -m "feat(artifact): canonical paths + frozen library.json types + reader + dead-run reconcile"
```

---

### Task 3: Per-task timing on the run-state schema

**Files:**
- Modify: `internal/state/state.go` (`TaskView`), `internal/runner/actor.go` + `internal/runner/runner.go`
- Test: `internal/state/state_test.go`, `internal/runner/runner_test.go`

**Interfaces:**
- Produces: `TaskView` gains `StartedAt string json:"started_at"` and `EndedAt string json:"ended_at"` (RFC3339, `""` when unset). The runs view (Task 6) derives each task's elapsed from these.

**Why:** the templ runs view renders per-task elapsed time, but the Plan-2 `TaskView` carries no per-task timing. The runner already times each attempt; recording the first-attempt start and the final end onto the TaskView is the minimal addition. The Go-authoritative `runs/<id>.json` extends its own schema (§9.4).

- [ ] **Step 1: Write the failing tests**

Append to `internal/state/state_test.go`:

```go
func TestTaskViewTimingRoundTrips(t *testing.T) {
	dir := t.TempDir()
	s := RunState{RunID: "r1", RunName: "r", StartedAt: "2026-07-09T00:00:00Z",
		Tasks: []TaskView{{Key: "a", Status: "passed", StartedAt: "2026-07-09T00:00:01Z", EndedAt: "2026-07-09T00:00:04Z"}}}
	if err := WriteRunState(dir, s); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "runs", "r1.json"))
	var probe map[string]any
	_ = json.Unmarshal(data, &probe)
	task0 := probe["tasks"].([]any)[0].(map[string]any)
	for _, k := range []string{"started_at", "ended_at"} {
		if _, ok := task0[k]; !ok {
			t.Fatalf("TaskView missing timing key %q: %v", k, task0)
		}
	}
}
```

Append to `internal/runner/runner_test.go`:

```go
func TestRunPopulatesTaskTiming(t *testing.T) {
	ringerBin := buildRingerBinary(t)
	stateDir := t.TempDir()
	m := &manifest.Manifest{RunName: "timing", Workdir: filepath.Join(t.TempDir(), "w"),
		Tasks: []manifest.Task{{Key: "a", Engine: "mock", TimeoutS: 30, Spec: "MOCK_FILE: a.txt\nhi\nMOCK_END", Check: "test -f a.txt", ExpectFiles: []string{"a.txt"}}}}
	engines := map[string]config.EngineConfig{"mock": {Bin: ringerBin, ArgsTemplate: []string{"mock-worker", "{spec}"}}}
	res, err := Run(context.Background(), Options{Manifest: m, Engines: engines, StateDir: stateDir, Identity: "j", Stdout: io.Discard, Logger: logging.Default()})
	if err != nil || !res.AllPassed {
		t.Fatalf("run: err=%v res=%+v", err, res)
	}
	data, _ := os.ReadFile(filepath.Join(stateDir, "runs", res.RunID+".json"))
	var s state.RunState
	_ = json.Unmarshal(data, &s)
	if s.Tasks[0].StartedAt == "" || s.Tasks[0].EndedAt == "" {
		t.Fatalf("task timing not populated: %+v", s.Tasks[0])
	}
}
```

(Check the runner test's import block; add `state` if missing.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — `TaskView` has no `StartedAt`/`EndedAt`; the runner test finds them empty.

- [ ] **Step 3: Extend the schema**

In `internal/state/state.go`, add to `TaskView`:

```go
	StartedAt string `json:"started_at"` // RFC3339, first-attempt start ("" until running)
	EndedAt   string `json:"ended_at"`   // RFC3339, final outcome time ("" until finished)
```

- [ ] **Step 4: Stamp timing in the actor + runner**

The actor owns the TaskView. Extend the `setStatus` and `setResult` ops to carry an RFC3339 timestamp:
- `setStatus(key, status string, attempt int, ts string)`: in the `run()` switch, set `tv.StartedAt = ts` **only if** `tv.StartedAt == ""` (first transition to running).
- `setResult(key, status string, tokens int64, verified, logPath, ts string)`: set `tv.EndedAt = ts`.

Update the command structs (`actorCmd`/`actorOp` payloads) to include the `ts` field, and the two call sites in `runTask`:

```go
		a.setStatus(task.Key, "running", attempt, time.Now().UTC().Format(time.RFC3339))
```
```go
	a.setResult(task.Key, verdictToStatus(verdict), tokens, task.Verified, logPath, time.Now().UTC().Format(time.RFC3339))
```

Search `setStatus(`/`setResult(` across `internal/runner` (incl. tests) and update every call to the new signature. `newActor`'s seed path leaves `StartedAt`/`EndedAt` as `""`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/state internal/runner
git commit -m "feat(state): per-task started_at/ended_at timing for the HUD runs view"
```

---

### Task 4: Server skeleton + layout templ + `/` + `hud` subcommand + `/healthz`

**Files:**
- Create: `internal/hud/views/layout.templ`, `internal/hud/server.go`, `internal/hud/runs.go` (just `handleRoot` for now), `cmd/ringer/hud.go`
- Test: `internal/hud/server_test.go`

**Interfaces:**
- Consumes: `logging.Logger`; `hud.staticHandler()` (Task 1).
- Produces:
  - `views.Layout() templ.Component` — the page shell (topbar, three panels wired with htmx poll+morph).
  - `hud.Server` with `New(stateDir string, lg logging.Logger) *Server`, `(*Server) Handler() http.Handler`, `(*Server) ListenAndServe(port int) error` (binds `127.0.0.1:<port>`, fails if taken).
  - CLI `ringer hud [--port N] [--no-open]`; `GET /healthz` → 200.

**Why:** the page shell + the router. The layout links the vendored htmx/idiomorph and `ringside.css`, and each live panel carries `hx-get`, `hx-trigger="load, every 2s"`, `hx-swap="morph"` so it self-populates on load and refreshes every 2s, morphed in — preserving scroll/expanded state. `GET /` renders the shell; the fragment routes (Tasks 6-7) fill it. `/healthz` is the cheap ensure-running probe.

**Note on package layout (avoids an import cycle):** the templ views live in package `views` (`internal/hud/views/`) together with their render helpers (Task 5). The server (package `hud`) imports `views`; `views` imports `internal/state` + `internal/artifact` (leaves). Never the reverse.

- [ ] **Step 1: Write the layout templ**

Create `internal/hud/views/layout.templ`:

```templ
package views

// Layout is the Ringside page shell. The live panels poll their fragment
// routes on load and every 2s, morphing the result in (idiomorph) so scroll
// position and expanded tasks survive each refresh. The panels render empty
// here; htmx fills them.
templ Layout() {
	<!DOCTYPE html>
	<html lang="en">
		<head>
			<meta charset="utf-8"/>
			<meta name="viewport" content="width=device-width, initial-scale=1"/>
			<title>Ringside</title>
			<link rel="stylesheet" href="/static/ringside.css"/>
			<script src="/static/vendor/htmx.min.js"></script>
			<script src="/static/vendor/idiomorph.min.js"></script>
		</head>
		<body hx-ext="morph">
			<div class="page">
				<header class="corner topbar">
					<span id="top-dot" class="live-dot" aria-hidden="true"></span>
					<div class="topbar-main">
						<span class="eyebrow wordmark"><b>Ringside</b></span>
					</div>
					<time id="clock" class="clock mono"></time>
				</header>
				<main>
					<section
						id="runs-panel"
						class="panel"
						hx-get="/hud/runs"
						hx-trigger="load, every 2s"
						hx-swap="morph"
					></section>
					<section
						id="library-panel"
						class="panel"
						hx-get="/hud/library"
						hx-trigger="load, every 2s"
						hx-swap="morph"
					></section>
					<section
						id="models-panel"
						class="panel"
						hx-get="/hud/models"
						hx-trigger="load"
						hx-swap="morph"
					></section>
				</main>
			</div>
		</body>
	</html>
}
```

(The topbar's machine-strip and live-clock were driven by client JS upstream; the clock/live-dot become progressively-enhanced niceties — out of scope here, the panels carry the live data. Reference `dashboard/ringside.html:531-564` for the exact class names the lifted CSS expects.)

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

func testServer(t *testing.T, stateDir string) http.Handler {
	t.Helper()
	return New(stateDir, logging.Default()).Handler()
}

func TestRootRendersShell(t *testing.T) {
	srv := testServer(t, t.TempDir())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	for _, want := range []string{
		`href="/static/ringside.css"`,
		`src="/static/vendor/htmx.min.js"`,
		`hx-get="/hud/runs"`,
		`hx-swap="morph"`,
		`id="runs-panel"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("shell missing %q", want)
		}
	}
}

func TestHealthz(t *testing.T) {
	srv := testServer(t, t.TempDir())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d", rec.Code)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `./build.sh --test`
Expected: FAIL — `undefined: New` (and `templ generate` produces `layout_templ.go`, so `views.Layout` exists once the server references it).

- [ ] **Step 4: Implement the server**

Create `internal/hud/server.go`:

```go
package hud

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"

	"github.com/a-h/templ"
	"github.com/corruptmemory/ringer/internal/hud/views"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/go-chi/chi/v5"
)

// DefaultPort is the fixed Ringside port.
const DefaultPort = 8700

// Server serves the Ringside dashboard for one state directory.
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

// Handler builds the chi router. Exposed for httptest.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Get("/", s.handleRoot)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/hud/runs", s.handleRuns)
	r.Get("/hud/library", s.handleLibrary)
	r.Get("/hud/models", s.handleModels)
	r.Handle("/static/*", staticHandler())
	r.Get("/artifacts/*", s.handleArtifacts)
	r.Get("/logs/*", s.handleLogs)
	r.Get("/api/open-folder", s.handleOpenFolder)
	return r
}

// ListenAndServe binds 127.0.0.1:<port> and serves until error, failing
// loudly if the port is in use (no fallback scan, as upstream).
func (s *Server) ListenAndServe(port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("could not start Ringside on %s; that port may already be in use: %w", addr, err)
	}
	s.lg.Infof("Ringside: http://%s", addr)
	return http.Serve(ln, s.Handler())
}

// renderComponent writes a templ component as an HTML response, logging a
// render error (a half-written body is the best we can do once bytes flow).
// templ.Component is the interface every generated view satisfies.
func (s *Server) renderComponent(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		s.lg.Warnf("hud: render: %v", err)
	}
}

// jsonUnmarshal is the shared decode helper for run-state files (Tasks 6, 8).
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
```

Create `internal/hud/runs.go` with just the (real) root handler:

```go
package hud

import (
	"net/http"

	"github.com/corruptmemory/ringer/internal/hud/views"
)

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	s.renderComponent(w, r, views.Layout())
}
```

Create `internal/hud/stubs.go` with the six not-yet-built handlers so the router compiles — each later task **deletes its own** handler from this file (never rewrites a sibling's) and implements the real one in its own file. When the last one is removed, delete `stubs.go`:

```go
package hud

import "net/http"

// Handlers stubbed until their task lands. Task 6 removes handleRuns;
// Task 7 removes handleLibrary + handleModels; Task 8 removes the last three
// and this file with them.
func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request)       { http.Error(w, "not implemented", http.StatusNotImplemented) }
func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request)    { http.Error(w, "not implemented", http.StatusNotImplemented) }
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request)     { http.Error(w, "not implemented", http.StatusNotImplemented) }
func (s *Server) handleArtifacts(w http.ResponseWriter, r *http.Request)  { http.Error(w, "not implemented", http.StatusNotImplemented) }
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request)       { http.Error(w, "not implemented", http.StatusNotImplemented) }
func (s *Server) handleOpenFolder(w http.ResponseWriter, r *http.Request) { http.Error(w, "not implemented", http.StatusNotImplemented) }
```

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
	NoOpen bool `long:"no-open" description:"do not open a browser (accepted for the detached-spawn path)"`
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
	return hud.New(cfg.StateDirPath(), lg).ListenAndServe(port) // blocks until killed
}

func init() {
	parser.AddCommand("hud", "Serve the Ringside dashboard",
		"Run the Ringside HUD on 127.0.0.1:8700 (templ+htmx; single fixed port, fails if taken).",
		&hudCmd{})
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `./build.sh --test`
Expected: PASS — `build.sh` runs `templ generate` (creates `layout_templ.go`), then the server compiles; `TestRootRendersShell` + `TestHealthz` green.

- [ ] **Step 7: Commit**

```bash
git add internal/hud cmd/ringer/hud.go
git commit -m "feat(hud): chi server + templ layout shell, / route, hud subcommand, /healthz"
```

---

### Task 5: Render helpers (the ex-`normalizeRuns` derivations, pure Go)

**Files:**
- Create: `internal/hud/views/render.go`
- Test: `internal/hud/views/render_test.go`

**Interfaces:**
- Consumes: `state.RunState`/`state.TaskView`.
- Produces (package `views`, called from the templ components): `RunState(rs) string` (live|pass|fail), `PassCount(rs) int`, `FailCount(rs) int`, `RunElapsed(rs) float64`, `TaskKind(t) string` (pass|working|retry|fail|waiting), `TaskElapsed(t) float64`, `FormatDuration(sec float64) string`.

**Why:** the live/pass/fail/elapsed derivations that lived in `ringside.html`'s `normalizeRuns`/`taskKind`/`formatDuration` JS (dashboard/ringside.html:663-706, ~610) move server-side as pure funcs the templ calls. Pure funcs are trivially unit-testable — the behavior the JS did implicitly is now pinned by tests. `TaskKind` maps the Go status to the same bucket names the lifted CSS styles (`.state.pass`, `.state.working`, `.state.retry`, `.state.fail`, `.state.waiting`).

- [ ] **Step 1: Write the failing tests**

Create `internal/hud/views/render_test.go`:

```go
package views

import (
	"testing"

	"github.com/corruptmemory/ringer/internal/state"
)

func TestRunDerivations(t *testing.T) {
	live := state.RunState{Done: false, StartedAt: "2026-07-09T00:00:00Z", UpdatedAt: "2026-07-09T00:00:05Z",
		Tasks: []state.TaskView{{Status: "running"}}}
	if RunState(live) != "live" {
		t.Fatalf("running run → %q, want live", RunState(live))
	}
	if RunElapsed(live) != 5 {
		t.Fatalf("elapsed = %v, want 5", RunElapsed(live))
	}
	finFail := state.RunState{Done: true, Tasks: []state.TaskView{{Status: "passed"}, {Status: "failed"}}}
	if RunState(finFail) != "fail" || PassCount(finFail) != 1 || FailCount(finFail) != 1 {
		t.Fatalf("finished-with-fail wrong: state=%q pass=%d fail=%d", RunState(finFail), PassCount(finFail), FailCount(finFail))
	}
	finPass := state.RunState{Done: true, Tasks: []state.TaskView{{Status: "passed"}}}
	if RunState(finPass) != "pass" {
		t.Fatalf("all-pass finished → %q, want pass", RunState(finPass))
	}
}

func TestTaskKind(t *testing.T) {
	cases := []struct {
		status  string
		attempt int
		want    string
	}{
		{"passed", 1, "pass"}, {"running", 1, "working"}, {"running", 2, "retry"},
		{"failed", 1, "fail"}, {"timeout", 1, "fail"}, {"pending", 0, "waiting"},
	}
	for _, c := range cases {
		if got := TaskKind(state.TaskView{Status: c.status, Attempt: c.attempt}); got != c.want {
			t.Errorf("TaskKind(%q, a%d) = %q, want %q", c.status, c.attempt, got, c.want)
		}
	}
}

func TestTaskElapsedAndFormat(t *testing.T) {
	tv := state.TaskView{StartedAt: "2026-07-09T00:00:00Z", EndedAt: "2026-07-09T00:01:03Z"}
	if TaskElapsed(tv) != 63 {
		t.Fatalf("task elapsed = %v, want 63", TaskElapsed(tv))
	}
	// A still-running task (no end) reads as 0, not a wrong number.
	if TaskElapsed(state.TaskView{StartedAt: "2026-07-09T00:00:00Z"}) != 0 {
		t.Fatal("unfinished task elapsed must be 0")
	}
	if got := FormatDuration(63); got != "1m 03s" {
		t.Fatalf("FormatDuration(63) = %q, want \"1m 03s\"", got)
	}
	if got := FormatDuration(9); got != "9s" {
		t.Fatalf("FormatDuration(9) = %q, want \"9s\"", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — `undefined: RunState` etc.

- [ ] **Step 3: Implement**

Create `internal/hud/views/render.go`:

```go
package views

import (
	"fmt"
	"time"

	"github.com/corruptmemory/ringer/internal/state"
)

// RunState derives the ringside run bucket from the Go run-state: "live"
// while running, else "fail" if any task failed/timed out, else "pass".
func RunState(rs state.RunState) string {
	if !rs.Done {
		return "live"
	}
	if FailCount(rs) > 0 {
		return "fail"
	}
	return "pass"
}

// PassCount / FailCount count terminal task outcomes.
func PassCount(rs state.RunState) int {
	n := 0
	for _, t := range rs.Tasks {
		if t.Status == "passed" {
			n++
		}
	}
	return n
}

func FailCount(rs state.RunState) int {
	n := 0
	for _, t := range rs.Tasks {
		if t.Status == "failed" || t.Status == "timeout" {
			n++
		}
	}
	return n
}

// RunElapsed is updated-started in seconds (0 if either is unparseable).
func RunElapsed(rs state.RunState) float64 { return elapsed(rs.StartedAt, rs.UpdatedAt) }

// TaskElapsed is a task's ended-started in seconds; a still-running task
// (no ended_at) reads as 0.
func TaskElapsed(t state.TaskView) float64 { return elapsed(t.StartedAt, t.EndedAt) }

// TaskKind maps a Go task status to the ringside bucket the lifted CSS
// styles: passed→pass, running→working (retry on a 2nd attempt),
// failed/timeout→fail, else waiting.
func TaskKind(t state.TaskView) string {
	switch t.Status {
	case "passed":
		return "pass"
	case "running":
		if t.Attempt > 1 {
			return "retry"
		}
		return "working"
	case "failed", "timeout":
		return "fail"
	default:
		return "waiting"
	}
}

// FormatDuration renders seconds as "9s" or "1m 03s" (mirrors ringside
// formatDuration's minute:zero-padded-second shape).
func FormatDuration(sec float64) string {
	s := int(sec + 0.5)
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm %02ds", s/60, s%60)
}

func elapsed(startISO, endISO string) float64 {
	start, err1 := time.Parse(time.RFC3339, startISO)
	end, err2 := time.Parse(time.RFC3339, endISO)
	if err1 != nil || err2 != nil {
		return 0
	}
	if d := end.Sub(start).Seconds(); d > 0 {
		return d
	}
	return 0
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/hud/views/render.go internal/hud/views/render_test.go
git commit -m "feat(hud): pure Go view helpers (ex-normalizeRuns live/pass/fail/elapsed/taskKind)"
```

---

### Task 6: `/hud/runs` — the live runs fragment (run-card → task-row templ)

**Files:**
- Create: `internal/hud/views/runs.templ`, `internal/hud/views/text.go`
- Modify: `internal/hud/runs.go` (real `handleRuns` + `scanRunStates`)
- Test: `internal/hud/runs_test.go`

**Interfaces:**
- Consumes: `state.RunState`; the Task-5 helpers.
- Produces: `views.RunsPanel(runs []state.RunState) templ.Component`, plus the shared `views.runCard`/`views.taskRow` components (Plan-4b reuses `runCard`/`taskRow` for the artifact pages); `OutcomeText(rs) string`, `TaskStateText(kind) string`. `GET /hud/runs` renders the panel from the newest 12 run-state files.

**Why:** the deliverable that closes the Plan-2 half-visibility gap — Go runs render live. The panel renders **straight from `[]state.RunState`** via the Task-5 helpers; there is no JSON and no schema adapter. htmx polls it every 2s and morphs it in. `runCard`/`taskRow` are written here as the **shared** components Plan-4b's artifact pages reuse.

- [ ] **Step 1: Write the runs templ**

Create `internal/hud/views/runs.templ`:

```templ
package views

import (
	"fmt"
	"net/url"

	"github.com/corruptmemory/ringer/internal/state"
)

// RunsPanel renders the live runs list (newest first). Reference
// dashboard/ringside.html:851-963 for the exact class structure the lifted
// CSS styles; the data derivations are the Task-5 helpers.
templ RunsPanel(runs []state.RunState) {
	<div id="runs" class="agents">
		if len(runs) == 0 {
			<div class="artifact-error">No runs yet.</div>
		}
		for _, rs := range runs {
			@runCard(rs)
		}
	</div>
}

// runCard is shared with the Plan-4b artifact pages.
templ runCard(rs state.RunState) {
	<section class={ "run", RunState(rs) }>
		<header class="corner">
			<span class={ "live-dot", templ.KV("live", RunState(rs) == "live") } aria-hidden="true"></span>
			<span class="eyebrow">{ rs.RunName } · { rs.Identity }</span>
			<span class="mono">{ OutcomeText(rs) } · { FormatDuration(RunElapsed(rs)) }</span>
		</header>
		<div class="work-list">
			for _, t := range rs.Tasks {
				@taskRow(rs.RunID, t)
			}
		</div>
	</section>
}

// taskRow is shared with the Plan-4b artifact pages.
templ taskRow(runID string, t state.TaskView) {
	<div class={ "work-item", TaskKind(t) }>
		<span class="name">{ t.Key }</span>
		if t.Engine != "" {
			<span class="mono muted">{ t.Engine }</span>
		}
		<span class={ "state", TaskKind(t) }>{ TaskStateText(TaskKind(t)) }</span>
		<span class="mono">{ FormatDuration(TaskElapsed(t)) }</span>
		if t.LogPath != "" {
			<a href={ templ.SafeURL(fmt.Sprintf("/logs/%s/%s", url.PathEscape(runID), url.PathEscape(t.Key))) }>view log</a>
		}
	</div>
}
```

- [ ] **Step 2: Write the text helpers**

Create `internal/hud/views/text.go`:

```go
package views

import (
	"fmt"

	"github.com/corruptmemory/ringer/internal/state"
)

// OutcomeText is the run's one-line result (mirrors ringside's outcome
// string, dashboard/ringside.html:885-889).
func OutcomeText(rs state.RunState) string {
	pass := PassCount(rs)
	if RunState(rs) == "live" {
		return fmt.Sprintf("%d passed so far", pass)
	}
	if fail := FailCount(rs); fail > 0 {
		return fmt.Sprintf("%d passed · %d failed", pass, fail)
	}
	return fmt.Sprintf("all %d passed", pass)
}

// TaskStateText is the human label for a task bucket (ringside
// taskStateText, dashboard/ringside.html:708-712).
func TaskStateText(kind string) string {
	switch kind {
	case "pass":
		return "finished & checked"
	case "working":
		return "working"
	case "retry":
		return "sent back — redoing"
	case "fail":
		return "failed"
	default:
		return "waiting"
	}
}
```

- [ ] **Step 3: Write the failing test**

Create `internal/hud/runs_test.go`:

```go
package hud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/state"
)

func TestHudRunsRendersFromGoState(t *testing.T) {
	dir := t.TempDir()
	if err := state.WriteRunState(dir, state.RunState{
		RunID: "demo-1", RunName: "demo", Identity: "jim", Done: true,
		StartedAt: "2026-07-09T00:00:00Z", UpdatedAt: "2026-07-09T00:00:05Z",
		Tasks: []state.TaskView{
			{Key: "alpha", Engine: "mock", Status: "passed", StartedAt: "2026-07-09T00:00:00Z", EndedAt: "2026-07-09T00:00:03Z", LogPath: "/x/alpha.log"},
			{Key: "bravo", Engine: "mock", Status: "failed", StartedAt: "2026-07-09T00:00:00Z", EndedAt: "2026-07-09T00:00:05Z"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	srv := New(dir, nil).Handler()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hud/runs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"demo",                       // run name
		"alpha", "bravo",             // task keys
		`class="run fail"`,           // finished-with-failure bucket (derived, no adapter)
		"finished &amp; checked",     // passed task label (templ HTML-escapes the &)
		"failed",                     // failed task label
		"/logs/demo-1/alpha",         // log link for the task that has a log path
		"1 passed · 1 failed",        // OutcomeText
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("runs fragment missing %q\n---\n%s", want, body)
		}
	}
}

func TestHudRunsEmpty(t *testing.T) {
	srv := New(t.TempDir(), nil).Handler()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hud/runs", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No runs yet") {
		t.Fatalf("empty runs: code=%d body=%q", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `./build.sh --test`
Expected: FAIL — `handleRuns` returns 501.

- [ ] **Step 5: Implement the handler + scan**

Delete the `handleRuns` line from `internal/hud/stubs.go`, and append the real handler + `scanRunStates` to `internal/hud/runs.go` (which already holds `handleRoot` from Task 4 — do not redefine it; add these imports to the file's block):

```go
// add to internal/hud/runs.go (alongside the existing handleRoot):
import (
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/corruptmemory/ringer/internal/state"
)

// hudRunsLimit caps how many recent runs the panel shows (upstream: 12).
const hudRunsLimit = 12

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	s.renderComponent(w, r, views.RunsPanel(s.scanRunStates()))
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
		info, err := e.Info()
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
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
	for i, st := range out {
		res[i] = st.rs
	}
	return res
}
```

(`scanRunStates` uses the `jsonUnmarshal` helper already defined in `server.go` at Task 4.)

- [ ] **Step 6: Run test to verify it passes**

Run: `./build.sh --test`
Expected: PASS — the finished run renders `class="run fail"`, both task keys, the derived labels, and the log link — all straight from `state.RunState`, no adapter.

- [ ] **Step 7: Commit**

```bash
git add internal/hud
git commit -m "feat(hud): /hud/runs live fragment — run-card/task-row templ from Go run-state (no adapter)"
```

---

### Task 7: `/hud/library` (reconcile + render) + `/hud/models` stub

**Files:**
- Create: `internal/hud/views/library.templ`, `internal/hud/views/models.templ`
- Modify: `internal/hud/library.go` (new file, real `handleLibrary`), `internal/hud/models.go` (new file, real `handleModels`); remove the two stubs from `runs.go`
- Test: `internal/hud/library_test.go`

**Interfaces:**
- Consumes: `artifact.ReadLibrary`, `artifact.ReconcileDeadRuns` (Task 2); the run states (for the library panel's live entries).
- Produces: `views.LibraryPanel(lib artifact.Library) templ.Component`, `views.ModelsPanel() templ.Component`. `GET /hud/library` reconciles dead runs then renders; `GET /hud/models` renders the Plan-5 stub.

**Why:** the library panel lists each run_name's artifact state (live/died/pass/fail) and links to its live page + versions, rendered from `artifact.Library`. Reconcile-on-poll (upstream parity) flips dead live entries before rendering. `/hud/models` is a Plan-5 stub so the page's third panel renders cleanly.

- [ ] **Step 1: Write the templ**

Create `internal/hud/views/library.templ`:

```templ
package views

import (
	"fmt"
	"net/url"

	"github.com/corruptmemory/ringer/internal/artifact"
)

templ LibraryPanel(lib artifact.Library) {
	<div class="artifact-history">
		if len(lib.Artifacts) == 0 {
			<div class="artifact-error">No artifacts yet.</div>
		}
		for name, entry := range lib.Artifacts {
			<div class={ "artifact-row", entry.State }>
				<span class="artifact-state-dot" aria-hidden="true"></span>
				<a href={ templ.SafeURL("/artifacts/live/" + url.PathEscape(name) + ".html") } class="artifact-name">{ name }</a>
				<span class="mono muted">{ entry.State }</span>
				if entry.Identity != "" {
					<span class="mono muted">{ entry.Identity }</span>
				}
				<span class="mono muted">{ fmt.Sprintf("%d versions", len(entry.Versions)) }</span>
			</div>
		}
	</div>
}
```

Create `internal/hud/views/models.templ`:

```templ
package views

templ ModelsPanel() {
	<div class="artifact-status">Model analytics land in Plan 5.</div>
}
```

- [ ] **Step 2: Write the failing test**

Create `internal/hud/library_test.go`:

```go
package hud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/artifact"
)

func TestHudLibraryReconcilesAndRenders(t *testing.T) {
	dir := t.TempDir()
	// A live entry whose run is NOT registered → must render as died.
	if err := artifact.WriteLibrary(dir, artifact.Library{Artifacts: map[string]artifact.Entry{
		"demo": {State: "live", CurrentRunID: "demo-1", Identity: "jim"},
	}}); err != nil {
		t.Fatal(err)
	}
	srv := New(dir, nil).Handler()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hud/library", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "demo") || !strings.Contains(body, `artifact-row died`) {
		t.Fatalf("library did not reconcile-then-render died: %s", body)
	}
	// And it persisted the flip (reconcile side effect).
	if artifact.ReadLibrary(dir).Artifacts["demo"].State != "died" {
		t.Fatal("reconcile flip not persisted")
	}
}

func TestHudModelsStub(t *testing.T) {
	srv := New(t.TempDir(), nil).Handler()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hud/models", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Plan 5") {
		t.Fatalf("models stub: code=%d body=%q", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `./build.sh --test`
Expected: FAIL — stubs return 501.

- [ ] **Step 4: Implement**

Delete the `handleLibrary` and `handleModels` lines from `internal/hud/stubs.go`. Create `internal/hud/library.go`:

```go
package hud

import (
	"net/http"
	"time"

	"github.com/corruptmemory/ringer/internal/artifact"
	"github.com/corruptmemory/ringer/internal/hud/views"
)

func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	// Reconcile dead runs on every poll (upstream parity); non-fatal.
	if _, err := artifact.ReconcileDeadRuns(s.stateDir, time.Now().UTC().Format(time.RFC3339)); err != nil {
		s.lg.Warnf("hud: reconcile library: %v", err)
	}
	s.renderComponent(w, r, views.LibraryPanel(artifact.ReadLibrary(s.stateDir)))
}
```

Create `internal/hud/models.go`:

```go
package hud

import (
	"net/http"

	"github.com/corruptmemory/ringer/internal/hud/views"
)

// handleModels renders the Plan-5 stub; the model-log analytics read the
// SQLite eval store and land in Plan 5.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	s.renderComponent(w, r, views.ModelsPanel())
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `./build.sh --test`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/hud
git commit -m "feat(hud): /hud/library reconcile+render + /hud/models Plan-5 stub"
```

---

### Task 8: File-serving routes — `/artifacts/*`, `/logs/*`, `/api/open-folder`

**Files:**
- Create: `internal/hud/files.go` (replaces the three stubs)
- Test: `internal/hud/files_test.go`

**Interfaces:**
- Consumes: `artifact.ArtifactsDir`, `artifact.SanitizeName` (Task 2); `state.RunState` (for the log path).
- Produces: `GET /artifacts/<path>` (serves files under the artifact tree, traversal-guarded), `GET /logs/<run_id>/<task_key>` (last 64 KB byte-tail, `text/plain`), `GET /api/open-folder?run=<run_id>` (opens the deliverables dir: `xdg-open` on Linux, `open` on macOS).

**Why:** these serve files/dirs, not rendered pages — unchanged by the templ decision. `/artifacts` serves the zero-LLM pages/deliverables Plan-4b writes (and any Python-era tree today). The traversal guard resolves under the artifact root and rejects escapes. `/logs` byte-tails the worker log (last 64 KB, decoded lossily — may start mid-rune, as upstream). `/api/open-folder` is the spec-mandated per-OS fix.

- [ ] **Step 1: Write the failing test**

Create `internal/hud/files_test.go`:

```go
package hud

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/artifact"
	"github.com/corruptmemory/ringer/internal/state"
)

func TestArtifactsServeAndGuard(t *testing.T) {
	dir := t.TempDir()
	art := artifact.ArtifactsDir(dir)
	_ = os.MkdirAll(filepath.Join(art, "live"), 0o755)
	_ = os.WriteFile(filepath.Join(art, "live", "demo.html"), []byte("<h1>hi</h1>"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("nope"), 0o644) // outside the tree
	srv := New(dir, nil).Handler()

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/artifacts/live/demo.html", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("html serve: code=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, bad := range []string{"/artifacts/..%2fsecret.txt", "/artifacts/live/nope.html"} {
		r := httptest.NewRecorder()
		srv.ServeHTTP(r, httptest.NewRequest(http.MethodGet, bad, nil))
		if r.Code != http.StatusNotFound {
			t.Fatalf("%s: code=%d, want 404", bad, r.Code)
		}
	}
}

func TestLogsTail(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "a.worker.log")
	_ = os.WriteFile(logPath, []byte(strings.Repeat("H", 6*1024)+strings.Repeat("T", 64*1024)), 0o644)
	_ = state.WriteRunState(dir, state.RunState{RunID: "run-1", Tasks: []state.TaskView{{Key: "a", LogPath: logPath}}})
	srv := New(dir, nil).Handler()

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/logs/run-1/a", nil))
	if rec.Code != http.StatusOK || len(rec.Body.String()) != 64*1024 || strings.Contains(rec.Body.String(), "H") {
		t.Fatalf("tail wrong: code=%d len=%d", rec.Code, len(rec.Body.String()))
	}
	for _, bad := range []string{"/logs/run-1/nope", "/logs/..%2f..%2fetc/a"} {
		r := httptest.NewRecorder()
		srv.ServeHTTP(r, httptest.NewRequest(http.MethodGet, bad, nil))
		if r.Code != http.StatusNotFound {
			t.Fatalf("%s: code=%d, want 404", bad, r.Code)
		}
	}
}

func TestOpenFolderGuardsTraversal(t *testing.T) {
	srv := New(t.TempDir(), nil).Handler()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/open-folder?run=..%2f..%2f..%2fetc", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("escaping open-folder: code=%d, want 404", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./build.sh --test`
Expected: FAIL — the three handlers return 501.

- [ ] **Step 3: Implement**

Delete the `handleArtifacts`/`handleLogs`/`handleOpenFolder` lines from `internal/hud/stubs.go` — it is now empty, so `git rm internal/hud/stubs.go`. Create `internal/hud/files.go`:

```go
package hud

import (
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/corruptmemory/ringer/internal/artifact"
	"github.com/corruptmemory/ringer/internal/state"
	"github.com/go-chi/chi/v5"
)

const workerLogTailBytes = 64 * 1024

// --- /artifacts/<path> ---

func (s *Server) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	full, ok := s.resolveArtifactPath(chi.URLParam(r, "*"))
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

func (s *Server) resolveArtifactPath(rel string) (string, bool) {
	root, err := filepath.Abs(artifact.ArtifactsDir(s.stateDir))
	if err != nil {
		return "", false
	}
	candidate, err := filepath.Abs(filepath.Join(root, filepath.Clean("/"+rel)))
	if err != nil {
		return "", false
	}
	if candidate == root || !strings.HasPrefix(candidate, root+string(filepath.Separator)) {
		return "", false
	}
	return candidate, true
}

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

// --- /logs/<run_id>/<task_key> ---

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	runID, taskKey, ok := strings.Cut(chi.URLParam(r, "*"), "/")
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

func (s *Server) taskLogPath(runID, taskKey string) (string, bool) {
	runsRoot, err := filepath.Abs(filepath.Join(s.stateDir, "runs"))
	if err != nil {
		return "", false
	}
	candidate, err := filepath.Abs(filepath.Join(runsRoot, runID+".json"))
	if err != nil || filepath.Dir(candidate) != runsRoot {
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
	start := int64(0)
	if info.Size() > int64(max) {
		start = info.Size() - int64(max)
	}
	if _, err := f.Seek(start, 0); err != nil {
		return nil, err
	}
	buf := make([]byte, info.Size()-start)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// --- /api/open-folder ---

func (s *Server) handleOpenFolder(w http.ResponseWriter, r *http.Request) {
	root, err := filepath.Abs(artifact.ArtifactsDir(s.stateDir))
	if err != nil {
		http.Error(w, "bad state dir", http.StatusInternalServerError)
		return
	}
	target := filepath.Join(root, "deliverables")
	if runID := r.URL.Query().Get("run"); runID != "" {
		target = filepath.Join(target, artifact.SanitizeName(runID))
	}
	abs, err := filepath.Abs(target)
	if err != nil || (abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator))) {
		http.NotFound(w, r)
		return
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		abs = root
	}
	if err := openInFileManager(abs); err != nil {
		s.lg.Warnf("hud: open-folder %s: %v", abs, err)
		http.Error(w, "could not open folder", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// openInFileManager opens a directory: xdg-open on Linux, open on macOS
// (spec §8 fix — upstream was macOS-only). Detached; not waited on.
func openInFileManager(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "linux":
		return exec.Command("xdg-open", path).Start()
	default:
		return errUnsupportedOpen
	}
}

var errUnsupportedOpen = &openError{"open-folder is only supported on Linux (xdg-open) and macOS (open)"}

type openError struct{ msg string }

func (e *openError) Error() string { return e.msg }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `./build.sh --test`
Expected: PASS. (The open-folder traversal test asserts only the 404 guard — it never reaches the opener; the happy path shells to `xdg-open`, which in headless CI errors into a logged 500, not exercised by the guard test.)

- [ ] **Step 5: Commit**

```bash
git add internal/hud
git commit -m "feat(hud): file-serving routes — /artifacts (guarded), /logs 64KB tail, /api/open-folder per-OS"
```

---

### Task 9: ensure-HUD-running — probe `/healthz`, spawn detached, open browser once

**Files:**
- Create: `cmd/ringer/ensurehud.go`
- Modify: `cmd/ringer/run.go`, `cmd/ringer/demo.go`
- Test: `cmd/ringer/ensurehud_test.go`

**Interfaces:**
- Consumes: `hud.DefaultPort`.
- Produces: `ensureHUDRunning(stateDir string, port int, lg logging.Logger, openBrowser bool)`; `hudIsAlive(port int) bool`. Called by `run`/`demo` unless `--no-dashboard`.

**Why:** `run` makes the HUD available — probe `:8700/healthz` (0.4s); if nothing answers, spawn a **detached** `ringer hud --no-open --port N` (new session, output → `<stateDir>/hud.log`, stdin closed), poll up to ~3s for it, then open the browser **only if it wasn't already alive**. The "once per session" behavior is emergent from the long-lived detached process. Best-effort throughout — a run must never fail because the dashboard didn't start.

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

func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, p, _ := net.SplitHostPort(addr)
	var n int
	fmt.Sscanf(p, "%d", &n)
	return n
}

func TestHudProbe(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	if !hudIsAlive(portOf(t, ts.Listener.Addr().String())) {
		t.Fatal("probe should detect the live server")
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	free := portOf(t, ln.Addr().String())
	ln.Close()
	if hudIsAlive(free) {
		t.Fatal("probe should be false on a closed port")
	}
}

func TestEnsureHudRunningBestEffort(t *testing.T) {
	// openBrowser=false; a free port with no server. Must return promptly,
	// not panic, not error even if the spawned hud never comes up.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := portOf(t, ln.Addr().String())
	ln.Close()
	ensureHUDRunning(t.TempDir(), port, logging.Default(), false)
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

// hudIsAlive probes 127.0.0.1:<port>/healthz with a short timeout; true only
// on a 200.
func hudIsAlive(port int) bool {
	client := &http.Client{Timeout: 400 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ensureHUDRunning makes the Ringside HUD available: if nothing answers the
// probe, spawn a detached `ringer hud`, poll up to ~3s for it, then open the
// browser exactly once — only when it was not already alive. Best-effort: a
// spawn/browser failure is logged, never fatal.
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

func spawnDetachedHUD(stateDir string, port int) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(stateDir, "hud.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close() // the child holds its own fd after Start
	cmd := exec.Command(self, "hud", "--no-open", "--port", fmt.Sprintf("%d", port))
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach: new session
	return cmd.Start()
}

func openInBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	default:
		return fmt.Errorf("no browser opener for %s", runtime.GOOS)
	}
}
```

- [ ] **Step 4: Wire into run and demo**

In `cmd/ringer/run.go`: thread a `noDashboard bool` into `runManifestFile` (signature becomes `runManifestFile(ctx context.Context, manifestPath string, maxParallelOverride int, identityFlag string, dryRun, noDashboard bool) error`); `runCmd.Execute` and `demoCmd.Execute` pass `c.NoDashboard`. After the logger is built and before the run (skip when `dryRun`), add:

```go
	if !dryRun && !noDashboard {
		ensureHUDRunning(cfg.StateDirPath(), hud.DefaultPort, lg, true)
	}
```

Import `internal/hud` in `run.go`. Update the `NoDashboard` flag descriptions in both `run.go` and `demo.go` from `"accepted; always headless in Plan 2 (no HUD yet)"` to `"do not ensure the Ringside HUD is running / open a browser"`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS.

- [ ] **Step 6: Manual smoke (optional, local)**

```bash
./build.sh
./ringer hud &                                   # binds :8700
curl -s 127.0.0.1:8700/healthz; echo             # 200 (empty body)
curl -s 127.0.0.1:8700/ | grep -c 'hx-get="/hud/runs"'   # 1
kill %1
./ringer demo                                    # ensure-running spawns the HUD + opens a browser once
```

- [ ] **Step 7: Commit**

```bash
git add cmd/ringer
git commit -m "feat(cmd): ensure-HUD-running (probe /healthz -> spawn detached -> browser once); wire run/demo"
```

---

## Out of scope → Plan 4b and Plan 5

**Plan 4b — Artifact rendering (write side):** templ zero-LLM pages (status/report/index/file-wrapper — upstream `render_status_html`/`render_final_report_html`/`render_artifact_index_html`/`render_file_wrapper_html`), **reusing this plan's `views.runCard`/`views.taskRow`**; `library.json` *writing* + version-entry append/prune (`ARTIFACT_LIBRARY_MAX_VERSIONS = 20`); deliverables harvest on PASS (declared `expect_files` + fallback glob, `DELIVERABLE_MAX_BYTES = 20 MiB`, `FALLBACK_HARVEST_MAX_FILES = 8`), copied into `artifacts/deliverables/<run_id>/<task_key>/`; wired into the runner's flush tick. Golden tests pin the persisted pages (byte-equivalent-in-contract). The library panel here (Task 7) becomes meaningful once 4b writes entries; the artifact-preview iframe UX from the old Ringside (picker/version/preview) can also land with 4b, since it previews 4b's output.

**Plan 5 — analytics + cutover:** `/hud/models` fills from the SQLite eval store (scoreboard tiers, catalog enrichment, identity); `models`/`catalog` CLI; `db import`; the cutover deletions (`ringer.py`, `hud/`, `dashboard/dashboard.html`, `dashboard/ringside.html`; README/SKILL sweep); and the `allow_full_access` config gating still unenforced since Plan 2.

## Plan-3 follow-ups (opportunistic, non-blocking)

From the Plan-3 status banner — isolation-package cleanups unrelated to the HUD; list for the final whole-branch review, do not gate Plan 4:
- Redundant host-toolchain double-mount in `internal/isolate/jail.go`.
- `<workdir>/.jail` and `<workdir>/.scratch` base dirs left as empty litter after per-key cleanup.




