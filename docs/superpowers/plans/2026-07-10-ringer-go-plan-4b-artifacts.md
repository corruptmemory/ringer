# Ringer Go — Plan 4b: Artifact Rendering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port the Python-era artifact system — the self-refreshing per-run results pages, the frozen final reports, the all-runs index, `library.json`, deliverable harvesting, and text file-wrappers — to Go/templ, rendering straight from `state.RunState` and sharing the dashboard's row primitive + derivation helpers, so a Go `ringer run`/`demo` produces the same on-disk artifact tree the Python tool did.

**Architecture:** A leaf `internal/artifact` package owns *persistence* (library CRUD + prune, deliverable harvest, dead-run reconcile — no rendering, no I/O beyond files). The templ artifact **pages** live in `internal/hud/views` (package `views`) alongside the dashboard's `taskRow`, reusing the same derivation helpers (`render.go`/`text.go`) and CSS vocabulary. A new `internal/hud/artifactwriter` package orchestrates *rendering → disk* (render pages, then record the pages' paths into `library.json`); it implements a tiny `ArtifactWriter` interface the runner calls once per 1 Hz flush (`Live`) and once at completion (`Finish`). The runner itself stays render-agnostic: it calls `artifact.HarvestOnPass` per passing task and the injected `ArtifactWriter` at flush/finish. `cmd/ringer` constructs the concrete writer (when artifacts are enabled) and injects it into `run`/`demo`.

**Tech Stack:** Go 1.24+, `github.com/a-h/templ` (already a dep), chi HUD (already built, Plan 4), `go:embed` for the lifted CSS. No new third-party deps. Always build/test via `./build.sh` (it runs `go tool templ generate` before vet/build) and `./build.sh --test`.

## Global Constraints

Every task's requirements implicitly include this section. Exact values, copied from the spec (§8/§9) and `ringer.py`:

- **Frozen on-disk artifact tree** (all under `<state_dir>/artifacts/`, `<state_dir>` defaults to `~/.ringer`):
  - `<run_id>.html` — per-run page: status while live, final report once done. Written every flush.
  - `live/<sanitize(run_name)>.html` — same content as `<run_id>.html`, rendered relative to `live/`. Written every flush.
  - `<run_id>-report.html` — final report, written once at completion.
  - `versions/<sanitize(run_name)>/<sanitize(run_id)>.html` — frozen archival copy of the final report, once at completion.
  - `index.html` — all-runs table, written every flush.
  - `library.json` — the artifact library (schema below).
  - `deliverables/<sanitize(run_id)>/<sanitize(task_key)>/<name>` — harvested deliverable files (raw copies).
  - `view/<sanitize(run_id)>/<sanitize(task_key)>--<sanitize(source_name)>.html` — text/log file-wrapper pages.
- **`sanitize_artifact_name`** = replace every run of chars outside `[A-Za-z0-9._-]` with `-`, strip leading/trailing `.`/`-`. Already implemented as `artifact.SanitizeName` (`internal/artifact/paths.go`). **Known carry-over:** Go's empty-name fallback is `"unnamed"`; Python's was `"artifact"`. This only fires on a fully-empty sanitized name (never in practice); library.json is Go-authoritative (§9.4) so keep `"unnamed"` — do **not** churn Plan-4 code for a never-hit fallback.
- **Constants (verbatim, `ringer.py:63-79`):** `ARTIFACT_WRAPPER_TAIL_BYTES = 256*1024` (262144), `ARTIFACT_LIBRARY_MAX_VERSIONS = 20`, `DELIVERABLE_MAX_BYTES = 20*1024*1024` (20971520), `WORKER_LOG_TAIL_BYTES = 64*1024` (already `hud.workerLogTailBytes`), `TASK_REPORT_FILENAMES = ("report.md","report.html")`, `FALLBACK_HARVEST_MAX_FILES = 8`.
- **`TEXT_DELIVERABLE_SUFFIXES`** = `{.md .txt .log}`. **`IMAGE_DELIVERABLE_SUFFIXES`** = `{.avif .gif .jpeg .jpg .png .svg .webp}`. **`FALLBACK_HARVEST_SUFFIXES`** = `(TEXT − {.log}) ∪ IMAGE ∪ {.html .htm .json .csv .pdf .mp4 .webm .mov}` = the 17 distinct suffixes `{.md .txt .avif .gif .jpeg .jpg .png .svg .webp .html .htm .json .csv .pdf .mp4 .webm .mov}`.
- **CSP meta tag, verbatim (`ringer.py:82-85`), inlined into every artifact HTML doc** (no external stylesheet can load — the CSS is embedded per page): `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:">`.
- **`library.json` schema (frozen, JSON keys exact):** top-level `{"artifacts": { "<raw run_name>": <entry> }}` (map key is the **raw, un-sanitized** run_name). Entry: `live_path` (str), `state` (str ∈ `live|died|pass|fail`), `identity` (str), `current_run_id` (str), `updated_at` (str RFC3339), `versions` (array, **newest-first**, capped 20). Version: `run_id`, `path`, `report_path` (str|null — null when it equals `path`), `finished_at`, `outcome` (∈ `pass|fail|died`), `tasks_pass` (int), `tasks_fail` (int), `deliverables` (array). Deliverable: `task_key`, `name`, `path` (absolute copied-file path), `bytes` (int). These Go types already exist in `internal/artifact/library.go`.
- **`ArtifactConfig` defaults (`ringer.py:157-172`):** `enabled` default **true**; `out` default `<artifacts>/{run_id}.html`; `report_out` default `<artifacts>/{run_id}-report.html`; `index_out` default `<artifacts>/index.html`. `{run_id}`/`{run_name}` substituted **raw** (unsanitized) in `out`/`report_out`. `live`/`versions` paths are computed (sanitized), not config-templated.
- **Package layering (never reverse):** `state` (leaf) ← `artifact` (imports state) ← `views` (imports state+artifact) ← `artifactwriter` (imports state+artifact+views). `runner` imports `state`+`artifact` and defines its own `ArtifactWriter` interface (methods take `state.RunState`) — `runner` must **not** import `views`/`artifactwriter`/`hud`. `cmd/ringer` imports `runner`+`artifactwriter`+`config` and wires them.
- **CSS-class discipline:** every CSS class any `.templ` emits must have a matching selector in the embedded `internal/hud/static/artifact.css`. (Plan 4 shipped a bug where invented classes rendered unstyled — do not repeat it.)
- **Error-logging discipline:** artifact failures are **non-fatal** — a run must never fail because an artifact page or library write failed. Log them via the injected logger at `Warn` (never silent, never fatal), mirroring how `WriteRunState` failures are logged in `runner.go:138-140`. Positive tests install a recording logger and assert zero unexpected `.ERROR`s.
- **Tooling:** never call `go build`/`go test`/`templ generate` directly — use `./build.sh` and `./build.sh --test`. Format with `gofmt`. Tests are table-driven / sequential with `t.TempDir()` for isolation.
- **Fidelity:** **full parity** — reproduce the Python results pages faithfully (progress bar, plain-language briefing, per-task deliverable lists with image thumbnails, "read what it found"/log links, verification proof drawer, index table, 256 KiB text file-wrappers). Where exact byte-level HTML/text is the deliverable, port from the cited `ringer.py` function and lock the Go output with a golden file — the golden is authoritative for the Go renderer.

---

## File Structure

**New files:**
- `internal/artifact/deliverables.go` — `HarvestOnPass` + harvest constants/suffix sets. (persistence, no rendering)
- `internal/artifact/deliverables_test.go`
- `internal/artifact/library_write.go` — `OutcomeFromState`, `UpdateLibraryLive`, `AppendLibraryVersion`, `PruneVersions`. (persistence)
- `internal/artifact/library_write_test.go`
- `internal/hud/static/artifact.css` — the lifted `ARTIFACT_BASE_CSS` (verbatim), `go:embed`ed.
- `internal/hud/views/artifact_chrome.templ` — `artifactCorner`, `progressBar`, `briefing` (run-level page chrome).
- `internal/hud/views/artifact_work.templ` — `workSection`, `workGroup`, `workItem`, `taskLinks` (the work list + deliverables).
- `internal/hud/views/artifact_pages.templ` — `StatusPage`, `FinalReportPage`, `IndexPage`, `FileWrapperPage` (top-level page shells).
- `internal/hud/views/artifact_render.go` — pure helpers new to artifacts: briefing/legend/label/kind/href/data-URI logic + `IndexRow` type + `ArtifactPageData` structs.
- `internal/hud/views/artifact_render_test.go`, `internal/hud/views/artifact_pages_test.go` (goldens), plus golden fixtures under `internal/hud/views/testdata/`.
- `internal/hud/artifactwriter/writer.go` — `Writer` struct, `New`, `Live`, `Finish`, wrapper generation. Implements the runner's `ArtifactWriter`.
- `internal/hud/artifactwriter/writer_test.go`

**Modified files:**
- `internal/hud/views/render.go` — `TaskElapsed` gains a `nowISO` arg (timer fix).
- `internal/hud/views/runs.templ` — `taskRow`/`runCard` thread `rs.UpdatedAt` (timer fix).
- `internal/hud/views/render_test.go` — update `TaskElapsed` callers; add running-task case.
- `internal/state/state.go` — add `Deliverable` type; add `Deliverables`/`CheckTail`/`DeliverableNotes` to `TaskView`; add `ReadAllRunStates`.
- `internal/artifact/library.go` — change `Deliverable` to a type alias of `state.Deliverable` (keep JSON identical).
- `internal/config/config.go` — `ArtifactConfig.Enabled` → `*bool` (nil = default true); add `ResolveArtifact()` returning resolved paths + enabled.
- `internal/runner/runner.go` — define `ArtifactWriter` interface + `Options.Artifact`; call `Live` each flush, `Finish` at end (nil-safe); thread harvest + check-tail + deliverables into the terminal task result.
- `internal/runner/actor.go` — extend `opSetResult` to carry `deliverables`/`checkTail`/`notes`.
- `cmd/ringer/run.go` — construct + inject the writer when artifacts enabled.
- `config.sample.toml` — document the `[artifact]` table (if present in repo).

---

## Task 1: Working-task ticking timer fix

The dashboard's per-task timer reads `0s` while a task is running because `TaskElapsed` needs both `StartedAt` and `EndedAt`, and a running task has `EndedAt == ""`. Make a running task's elapsed compute `StartedAt → nowISO` (the snapshot's `UpdatedAt`), so it ticks with each 1 Hz poll. This also makes the artifact status page (Task 9, which renders the same `taskRow`) show live per-task timers.

**Files:**
- Modify: `internal/hud/views/render.go:46-48`
- Modify: `internal/hud/views/runs.templ:27-52`
- Test: `internal/hud/views/render_test.go`

**Interfaces:**
- Produces: `func TaskElapsed(t state.TaskView, nowISO string) float64` (signature change — one arg added). `taskRow(runID, nowISO string, t state.TaskView)`.

- [ ] **Step 1: Update the failing test first.** In `internal/hud/views/render_test.go`, find the existing `TaskElapsed` test(s) and (a) update every call to pass a `nowISO` second arg, and (b) add this case. It fails to compile until Step 2 (compile failure *is* the red state).

```go
func TestTaskElapsedRunningTaskTicks(t *testing.T) {
	// A running task (no ended_at) must measure started_at -> nowISO, not 0.
	running := state.TaskView{Status: "running", StartedAt: "2026-07-10T10:00:00Z", EndedAt: ""}
	if got := TaskElapsed(running, "2026-07-10T10:00:13Z"); got != 13 {
		t.Errorf("running TaskElapsed = %v, want 13", got)
	}
	// A finished task ignores nowISO and uses ended_at.
	done := state.TaskView{Status: "passed", StartedAt: "2026-07-10T10:00:00Z", EndedAt: "2026-07-10T10:00:05Z"}
	if got := TaskElapsed(done, "2026-07-10T10:00:13Z"); got != 5 {
		t.Errorf("finished TaskElapsed = %v, want 5", got)
	}
	// Unparseable/empty start still yields 0.
	if got := TaskElapsed(state.TaskView{}, "2026-07-10T10:00:13Z"); got != 0 {
		t.Errorf("empty TaskElapsed = %v, want 0", got)
	}
}
```

- [ ] **Step 2: Run it, expect a compile failure**

Run: `./build.sh --test 2>&1 | head -30`
Expected: build/test fails — `TaskElapsed` called with 2 args but defined with 1 (and the templ still calls the 1-arg form).

- [ ] **Step 3: Change `TaskElapsed` in `render.go`**

```go
// TaskElapsed is a task's elapsed seconds: ended-started once finished, else
// nowISO-started while still running (so a working task's timer ticks with
// each 1 Hz snapshot instead of reading 0). nowISO is the snapshot's
// UpdatedAt. 0 if start is unparseable or the interval is non-positive.
func TaskElapsed(t state.TaskView, nowISO string) float64 {
	end := t.EndedAt
	if end == "" {
		end = nowISO
	}
	return elapsed(t.StartedAt, end)
}
```

- [ ] **Step 4: Thread `nowISO` through `runs.templ`.** In `runCard`, change the task loop to pass the run's `UpdatedAt`; in `taskRow`, add the `nowISO` param and use it:

```templ
templ runCard(rs state.RunState) {
	<section class={ "run", RunState(rs) }>
		<header class="corner">
			<span class={ "live-dot", templ.KV("live", RunState(rs) == "live") } aria-hidden="true"></span>
			<span class="eyebrow">{ rs.RunName }</span>
			<span class="identity">{ rs.Identity }</span>
			<span class="clock mono run-stats">{ OutcomeText(rs) } · { FormatDuration(RunElapsed(rs)) }</span>
		</header>
		<div class="workers">
			for _, t := range rs.Tasks {
				@taskRow(rs.RunID, rs.UpdatedAt, t)
			}
		</div>
	</section>
}
```

In `taskRow`, change the signature to `templ taskRow(runID, nowISO string, t state.TaskView)` and the time span to `<span class="time mono">{ FormatDuration(TaskElapsed(t, nowISO)) }</span>`. Leave everything else in `taskRow` unchanged.

- [ ] **Step 5: Regenerate templ + run the full suite**

Run: `./build.sh --test 2>&1 | tail -30`
Expected: PASS — `go tool templ generate` regenerates `runs_templ.go`, and all `views` + `hud` tests pass. (If the HUD httptest suite pinned the old 1-arg behavior anywhere, update those assertions to the ticking value.)

- [ ] **Step 6: Commit**

```bash
git add internal/hud/views/render.go internal/hud/views/runs.templ internal/hud/views/runs_templ.go internal/hud/views/render_test.go
git commit -m "fix(hud): running-task timer ticks (TaskElapsed uses snapshot UpdatedAt)"
```

---

## Task 2: State data model — Deliverable type + TaskView fields + ReadAllRunStates

Full parity needs the run-state snapshot to carry, per task: the harvested **deliverables**, a **check-output tail** (for the proof drawer), and any **deliverable notes** (oversized/cap messages). Add these to `state` (the leaf both `artifact` and `views` import), and add a helper to read every run-state file (the index page + reconcile scanning need it).

**Files:**
- Modify: `internal/state/state.go:11-33` (TaskView + new Deliverable type), and add `ReadAllRunStates`.
- Modify: `internal/artifact/library.go:33-38` (make `Deliverable` an alias).
- Test: `internal/state/state_test.go`

**Interfaces:**
- Produces: `state.Deliverable{TaskKey, Name, Path string; Bytes int64}` (JSON `task_key`/`name`/`path`/`bytes`); `TaskView.Deliverables []Deliverable` (`deliverables`), `TaskView.CheckTail string` (`check_tail`), `TaskView.DeliverableNotes []string` (`deliverable_notes`); `func ReadAllRunStates(stateDir string) ([]RunState, error)` (reads `<stateDir>/runs/*.json`, skips malformed, newest-`UpdatedAt`-first).
- Consumes (in `artifact`): `type Deliverable = state.Deliverable`.

- [ ] **Step 1: Write the failing test** in `internal/state/state_test.go`:

```go
func TestTaskViewDeliverablesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := RunState{
		RunID: "r1", RunName: "run", UpdatedAt: "2026-07-10T10:00:00Z",
		Tasks: []TaskView{{
			Key: "alpha", Status: "passed",
			Deliverables:     []Deliverable{{TaskKey: "alpha", Name: "out.md", Path: "/a/out.md", Bytes: 12}},
			CheckTail:        "ok\n",
			DeliverableNotes: []string{"big.bin skipped"},
		}},
	}
	if err := WriteRunState(dir, in); err != nil {
		t.Fatal(err)
	}
	got, err := ReadAllRunStates(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Tasks) != 1 {
		t.Fatalf("got %d runs", len(got))
	}
	d := got[0].Tasks[0].Deliverables
	if len(d) != 1 || d[0].Name != "out.md" || d[0].Bytes != 12 {
		t.Errorf("deliverables round-trip wrong: %+v", d)
	}
	if got[0].Tasks[0].CheckTail != "ok\n" {
		t.Errorf("check_tail lost: %q", got[0].Tasks[0].CheckTail)
	}
}

func TestReadAllRunStatesSkipsMalformedNewestFirst(t *testing.T) {
	dir := t.TempDir()
	_ = WriteRunState(dir, RunState{RunID: "old", UpdatedAt: "2026-07-10T09:00:00Z"})
	_ = WriteRunState(dir, RunState{RunID: "new", UpdatedAt: "2026-07-10T11:00:00Z"})
	if err := os.WriteFile(filepath.Join(dir, "runs", "junk.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadAllRunStates(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].RunID != "new" {
		t.Fatalf("want [new, old] skipping junk, got %+v", got)
	}
}
```

- [ ] **Step 2: Run it, expect failure**

Run: `./build.sh --test 2>&1 | grep -A3 -iE "Deliverable|ReadAllRunStates|undefined" | head`
Expected: FAIL — `Deliverable`, `TaskView.Deliverables`, `ReadAllRunStates` undefined.

- [ ] **Step 3: Add the types + fields in `internal/state/state.go`**

```go
// Deliverable is one harvested task output file, recorded on the run-state
// snapshot and copied into the artifact library version. JSON keys are frozen
// (Python parity): task_key/name/path/bytes. path is the absolute copied-file
// path under <state_dir>/artifacts/deliverables/.
type Deliverable struct {
	TaskKey string `json:"task_key"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Bytes   int64  `json:"bytes"`
}
```

Add to `TaskView` (after `EndedAt`):

```go
	Deliverables     []Deliverable `json:"deliverables,omitempty"`      // harvested on PASS
	CheckTail        string        `json:"check_tail,omitempty"`         // tail of the final verify output (proof drawer)
	DeliverableNotes []string      `json:"deliverable_notes,omitempty"`  // oversized/cap messages
```

- [ ] **Step 4: Add `ReadAllRunStates`** to `internal/state/state.go` (uses `sort` — add to imports):

```go
// ReadAllRunStates reads every <stateDir>/runs/*.json run-state file, skipping
// any that are missing/malformed, sorted newest-UpdatedAt-first. Used by the
// artifact index page and any all-runs scan.
func ReadAllRunStates(stateDir string) ([]RunState, error) {
	entries, err := os.ReadDir(filepath.Join(stateDir, "runs"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []RunState
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(stateDir, "runs", e.Name()))
		if err != nil {
			continue
		}
		var rs RunState
		if err := json.Unmarshal(data, &rs); err != nil {
			continue
		}
		out = append(out, rs)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}
```

Add `"sort"` and `"strings"` to the `state` imports.

- [ ] **Step 5: Alias `artifact.Deliverable` to `state.Deliverable`.** In `internal/artifact/library.go`, delete the `Deliverable struct {...}` block and replace with:

```go
// Deliverable is defined in state (the leaf both artifact and the run-state
// snapshot share); aliased here so the frozen library.json schema and the
// existing artifact.Version.Deliverables field are unchanged.
type Deliverable = state.Deliverable
```

Add `"github.com/corruptmemory/ringer/internal/state"` to the `artifact` imports if not already present (it is, via `reconcile.go`, but `library.go` needs its own import).

- [ ] **Step 6: Run tests + commit**

Run: `./build.sh --test 2>&1 | tail -20`
Expected: PASS (state + artifact + hud all green; the alias keeps `library_test.go` compiling).

```bash
git add internal/state/state.go internal/state/state_test.go internal/artifact/library.go
git commit -m "feat(state): Deliverable type + per-task deliverables/check-tail/notes + ReadAllRunStates"
```

---

## Task 3: Deliverable harvesting on PASS

Port `_harvest_deliverables_on_pass` (`ringer.py:6923-6983`). For a passing task, copy its output files into `<state_dir>/artifacts/deliverables/<sanitize(run_id)>/<sanitize(task_key)>/` and return the `state.Deliverable` records + human-readable notes for anything skipped.

**Files:**
- Create: `internal/artifact/deliverables.go`
- Test: `internal/artifact/deliverables_test.go`

**Interfaces:**
- Consumes: `artifact.DeliverablesDir` (exists), `artifact.SanitizeName` (exists), `state.Deliverable`.
- Produces: `func HarvestOnPass(stateDir, runID, taskKey, taskDir string, expectFiles []string, worktrees bool) (deliverables []state.Deliverable, notes []string, err error)`.

**Selection rules (exact, from the Python):**
1. If `expectFiles` is non-empty: harvest those, **in declaration order**. A relative path resolves inside `taskDir`; an absolute or `~`-prefixed path resolves as-is. A missing / non-regular-file entry is **silently skipped** (on PASS every declared file already exists and is non-empty, guaranteed by verify).
2. Else if **not** `worktrees` (fallback glob): scan the **top level** of `taskDir` (non-recursive), regular files only, exclude dotfiles (name starts with `.`), keep files whose lowercased suffix is in `FALLBACK_HARVEST_SUFFIXES`, **sort alphabetically by basename**, then cap to the **first `FALLBACK_HARVEST_MAX_FILES` (8)**. If more than 8 matched, append a note. Harvest the survivors.
3. Else (`worktrees` and no `expectFiles`): harvest nothing.
4. For each candidate: if source size `> DELIVERABLE_MAX_BYTES`, **skip entirely** (no copy, no record) and append a note `"<name> was not copied because it is larger than 20 MB."`. Otherwise copy to `<deliverablesDir>/<name>` preserving mtime (equivalent to `shutil.copy2`), and record `{TaskKey: taskKey, Name: name, Path: <abs dest>, Bytes: <copied size>}`. `TASK_REPORT_FILENAMES` get **no** special treatment here.

- [ ] **Step 1: Write the failing tests** in `internal/artifact/deliverables_test.go`:

```go
package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHarvestExpectFilesInOrder(t *testing.T) {
	sd := t.TempDir()
	td := t.TempDir()
	writeFile(t, filepath.Join(td, "b.txt"), 3)
	writeFile(t, filepath.Join(td, "a.txt"), 4)
	got, notes, err := HarvestOnPass(sd, "run-1", "alpha", td, []string{"b.txt", "a.txt"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Errorf("unexpected notes: %v", notes)
	}
	if len(got) != 2 || got[0].Name != "b.txt" || got[1].Name != "a.txt" {
		t.Fatalf("want [b.txt a.txt] in declaration order, got %+v", got)
	}
	if got[0].TaskKey != "alpha" || got[0].Bytes != 3 {
		t.Errorf("record wrong: %+v", got[0])
	}
	// Copied under deliverables/<run>/<task>/<name>.
	want := filepath.Join(DeliverablesDir(sd, "run-1", "alpha"), "b.txt")
	if got[0].Path != want {
		t.Errorf("path = %q, want %q", got[0].Path, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("deliverable not copied: %v", err)
	}
}

func TestHarvestMissingDeclaredFileSkippedSilently(t *testing.T) {
	sd, td := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(td, "present.txt"), 2)
	got, notes, err := HarvestOnPass(sd, "r", "t", td, []string{"present.txt", "gone.txt"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "present.txt" || len(notes) != 0 {
		t.Fatalf("missing file must be skipped silently: got=%+v notes=%v", got, notes)
	}
}

func TestHarvestFallbackGlobSortedCappedAt8(t *testing.T) {
	sd, td := t.TempDir(), t.TempDir()
	for _, n := range []string{"09.md", "01.md", "02.md", "03.md", "04.md", "05.md", "06.md", "07.md", "08.md"} {
		writeFile(t, filepath.Join(td, n), 1)
	}
	writeFile(t, filepath.Join(td, ".hidden.md"), 1) // dotfile excluded
	writeFile(t, filepath.Join(td, "note.log"), 1)   // .log excluded from fallback
	writeFile(t, filepath.Join(td, "data.bin"), 1)   // unlisted suffix excluded
	got, notes, err := HarvestOnPass(sd, "r", "t", td, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 {
		t.Fatalf("want cap 8, got %d", len(got))
	}
	if got[0].Name != "01.md" || got[7].Name != "08.md" {
		t.Errorf("want first-8-alphabetical, got first=%s last=%s", got[0].Name, got[7].Name)
	}
	if len(notes) == 0 {
		t.Errorf("expected a cap note")
	}
}

func TestHarvestWorktreesNoExpectFilesHarvestsNothing(t *testing.T) {
	sd, td := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(td, "x.md"), 1)
	got, _, err := HarvestOnPass(sd, "r", "t", td, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("worktrees + no expect_files must harvest nothing, got %+v", got)
	}
}

func TestHarvestOversizedSkippedWithNote(t *testing.T) {
	sd, td := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(td, "big.md"), int(DeliverableMaxBytes)+1)
	writeFile(t, filepath.Join(td, "ok.md"), 5)
	got, notes, err := HarvestOnPass(sd, "r", "t", td, []string{"big.md", "ok.md"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "ok.md" {
		t.Fatalf("oversized must be skipped, got %+v", got)
	}
	if len(notes) != 1 {
		t.Errorf("want 1 oversize note, got %v", notes)
	}
}
```

- [ ] **Step 2: Run tests, expect failure**

Run: `./build.sh --test 2>&1 | grep -iE "HarvestOnPass|DeliverableMaxBytes|undefined" | head`
Expected: FAIL — undefined `HarvestOnPass`, `DeliverableMaxBytes`.

- [ ] **Step 3: Implement `internal/artifact/deliverables.go`**

```go
package artifact

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/corruptmemory/ringer/internal/state"
)

const (
	DeliverableMaxBytes    = 20 * 1024 * 1024 // 20 MiB; oversized deliverables are skipped
	FallbackHarvestMaxFiles = 8
)

// fallbackHarvestSuffixes is (TEXT_DELIVERABLE − {.log}) ∪ IMAGE ∪ extras
// (ringer.py:74-78): file types worth rescuing from a task dir that declared
// no expect_files. .log excluded — the worker log is linked separately.
var fallbackHarvestSuffixes = map[string]bool{
	".md": true, ".txt": true,
	".avif": true, ".gif": true, ".jpeg": true, ".jpg": true, ".png": true, ".svg": true, ".webp": true,
	".html": true, ".htm": true, ".json": true, ".csv": true, ".pdf": true, ".mp4": true, ".webm": true, ".mov": true,
}

// HarvestOnPass copies a passing task's deliverable files into the artifacts
// tree and returns the records + human-readable notes for anything skipped.
// Ported from _harvest_deliverables_on_pass (ringer.py:6923-6983). Never
// returns a hard error for a per-file copy failure that leaves the run
// unaffected; err is non-nil only for an unexpected filesystem failure while
// preparing the destination.
func HarvestOnPass(stateDir, runID, taskKey, taskDir string, expectFiles []string, worktrees bool) ([]state.Deliverable, []string, error) {
	names := expectFiles
	var notes []string
	if len(names) == 0 {
		if worktrees {
			return nil, nil, nil // worktree taskdir is a whole repo; nothing to guess
		}
		globbed, capNote := fallbackCandidates(taskDir)
		names = globbed
		if capNote != "" {
			notes = append(notes, capNote)
		}
	}
	if len(names) == 0 {
		return nil, notes, nil
	}
	targetDir := DeliverablesDir(stateDir, runID, taskKey)
	var out []state.Deliverable
	for _, rel := range names {
		src := expectFilePath(taskDir, rel)
		info, err := os.Stat(src)
		if err != nil || !info.Mode().IsRegular() {
			continue // missing / non-file: silently skipped (verify already guaranteed presence on PASS)
		}
		if info.Size() > DeliverableMaxBytes {
			notes = append(notes, fmt.Sprintf("%s was not copied because it is larger than 20 MB.", filepath.Base(src)))
			continue
		}
		dst := filepath.Join(targetDir, filepath.Base(src))
		if err := copyFilePreservingMtime(src, dst); err != nil {
			return nil, notes, fmt.Errorf("harvest %s: %w", rel, err)
		}
		di, _ := os.Stat(dst)
		var size int64
		if di != nil {
			size = di.Size()
		}
		out = append(out, state.Deliverable{TaskKey: taskKey, Name: filepath.Base(src), Path: dst, Bytes: size})
	}
	return out, notes, nil
}

// expectFilePath resolves a declared expect_files entry: absolute or ~-prefixed
// as-is, otherwise relative to taskDir (ringer.py:6747-6749).
func expectFilePath(taskDir, p string) string {
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(taskDir, p)
}

// fallbackCandidates globs the top level of taskDir for harvestable files,
// alphabetical, capped at FallbackHarvestMaxFiles. Returns the kept names and
// a note if the cap truncated the list.
func fallbackCandidates(taskDir string) ([]string, string) {
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return nil, ""
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if !fallbackHarvestSuffixes[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		if info, err := e.Info(); err != nil || !info.Mode().IsRegular() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if len(names) > FallbackHarvestMaxFiles {
		note := fmt.Sprintf("Showing the first %d of %d files produced.", FallbackHarvestMaxFiles, len(names))
		return names[:FallbackHarvestMaxFiles], note
	}
	return names, ""
}

func copyFilePreservingMtime(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if info, err := os.Stat(src); err == nil {
		_ = os.Chtimes(dst, info.ModTime(), info.ModTime())
	}
	return nil
}
```

- [ ] **Step 4: Run tests, expect PASS**

Run: `./build.sh --test 2>&1 | tail -20`
Expected: PASS — all five harvest tests green.

- [ ] **Step 5: Commit**

```bash
git add internal/artifact/deliverables.go internal/artifact/deliverables_test.go
git commit -m "feat(artifact): HarvestOnPass — port deliverable harvesting"
```

---

## Task 4: Library write path — live update, version append, prune

Port `_library_entry` / `update_artifact_library_live` / `append_artifact_library_version` / `prune_artifact_versions` / `artifact_outcome_from_state` (`ringer.py:1934-2054`). These build on the existing `ReadLibrary`/`WriteLibrary` (Plan 4).

**Files:**
- Create: `internal/artifact/library_write.go`
- Test: `internal/artifact/library_write_test.go`

**Interfaces:**
- Consumes: `ReadLibrary`, `WriteLibrary`, `LibraryPath`, `ArtifactsDir`, `state.RunState`.
- Produces:
  - `func OutcomeFromState(rs state.RunState) string` → `live|died|pass|fail` (there is no `died` from a live snapshot; `died` comes only from reconcile — this returns `live` while `!Done`, else `fail` if any fail, else `pass`).
  - `func UpdateLibraryLive(stateDir, runName, runID, identity, livePath, state string, nowISO string) error` — read-modify-write the entry, **preserving existing versions**.
  - `func AppendLibraryVersion(stateDir string, e VersionRecord, nowISO string) error` — prepend a version (dedup same `run_id`), flip the entry `state` to the version outcome, cap to 20, prune the tail. `VersionRecord` bundles the fields below.
  - `type VersionRecord struct { RunName, RunID, Identity, LivePath, VersionPath string; ReportPath *string; Outcome string; TasksPass, TasksFail int; Deliverables []state.Deliverable }`

- [ ] **Step 1: Write the failing tests** in `internal/artifact/library_write_test.go`:

```go
package artifact

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/state"
)

func TestUpdateLibraryLivePreservesVersions(t *testing.T) {
	sd := t.TempDir()
	// Seed an entry with one existing version.
	seed := Library{Artifacts: map[string]Entry{"run": {
		State: "pass", Identity: "id", CurrentRunID: "old",
		Versions: []Version{{RunID: "old", Path: "/p/old.html", Outcome: "pass"}},
	}}}
	if err := WriteLibrary(sd, seed); err != nil {
		t.Fatal(err)
	}
	if err := UpdateLibraryLive(sd, "run", "new", "id", "/a/live.html", "live", "2026-07-10T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	e := ReadLibrary(sd).Artifacts["run"]
	if e.State != "live" || e.CurrentRunID != "new" || e.LivePath != "/a/live.html" {
		t.Errorf("entry not updated: %+v", e)
	}
	if len(e.Versions) != 1 || e.Versions[0].RunID != "old" {
		t.Errorf("existing versions must be preserved: %+v", e.Versions)
	}
}

func TestAppendVersionPrependsDedupsFlipsState(t *testing.T) {
	sd := t.TempDir()
	_ = UpdateLibraryLive(sd, "run", "r2", "id", "/a/live.html", "live", "t0")
	rep := "/a/r2-report.html"
	rec := VersionRecord{
		RunName: "run", RunID: "r2", Identity: "id", LivePath: "/a/live.html",
		VersionPath: "/a/versions/run/r2.html", ReportPath: &rep,
		Outcome: "pass", TasksPass: 3, TasksFail: 0,
		Deliverables: []state.Deliverable{{TaskKey: "a", Name: "o.md", Path: "/d/o.md", Bytes: 4}},
	}
	if err := AppendLibraryVersion(sd, rec, "t1"); err != nil {
		t.Fatal(err)
	}
	e := ReadLibrary(sd).Artifacts["run"]
	if e.State != "pass" {
		t.Errorf("entry state must flip to outcome, got %q", e.State)
	}
	if len(e.Versions) != 1 || e.Versions[0].RunID != "r2" || e.Versions[0].TasksPass != 3 {
		t.Fatalf("version not recorded: %+v", e.Versions)
	}
	if len(e.Versions[0].Deliverables) != 1 {
		t.Errorf("deliverables not carried: %+v", e.Versions[0])
	}
	// Re-appending same run_id replaces (still one, still front).
	_ = AppendLibraryVersion(sd, rec, "t2")
	if e2 := ReadLibrary(sd).Artifacts["run"]; len(e2.Versions) != 1 {
		t.Errorf("same run_id must dedup, got %d versions", len(e2.Versions))
	}
}

func TestAppendVersionCapsAt20AndPrunesFiles(t *testing.T) {
	sd := t.TempDir()
	art := ArtifactsDir(sd)
	mk := func(rel string) string { // create a real file under artifacts/ so prune can delete it
		p := filepath.Join(art, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
		return p
	}
	for i := 0; i < 21; i++ {
		id := "r" + string(rune('a'+i))
		vpath := mk("versions/run/" + id + ".html")
		rpath := mk(id + "-report.html")
		rp := rpath
		_ = AppendLibraryVersion(sd, VersionRecord{
			RunName: "run", RunID: id, Identity: "id", LivePath: "/a/live.html",
			VersionPath: vpath, ReportPath: &rp, Outcome: "pass",
		}, "t")
	}
	e := ReadLibrary(sd).Artifacts["run"]
	if len(e.Versions) != 20 {
		t.Fatalf("want 20 kept, got %d", len(e.Versions))
	}
	// The oldest (r"a") version's files must be gone from disk.
	if _, err := os.Stat(filepath.Join(art, "versions/run/ra.html")); !os.IsNotExist(err) {
		t.Errorf("pruned version html should be deleted")
	}
	if _, err := os.Stat(filepath.Join(art, "ra-report.html")); !os.IsNotExist(err) {
		t.Errorf("pruned report html should be deleted")
	}
}

func TestOutcomeFromState(t *testing.T) {
	live := state.RunState{Done: false, Tasks: []state.TaskView{{Status: "running"}}}
	if OutcomeFromState(live) != "live" {
		t.Errorf("running run should be live")
	}
	pass := state.RunState{Done: true, Tasks: []state.TaskView{{Status: "passed"}}}
	if OutcomeFromState(pass) != "pass" {
		t.Errorf("all-pass should be pass")
	}
	fail := state.RunState{Done: true, Tasks: []state.TaskView{{Status: "passed"}, {Status: "failed"}}}
	if OutcomeFromState(fail) != "fail" {
		t.Errorf("any-fail should be fail")
	}
}
```

- [ ] **Step 2: Run tests, expect failure**

Run: `./build.sh --test 2>&1 | grep -iE "UpdateLibraryLive|AppendLibraryVersion|OutcomeFromState|VersionRecord|undefined" | head`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Implement `internal/artifact/library_write.go`**

```go
package artifact

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/corruptmemory/ringer/internal/state"
)

const ArtifactLibraryMaxVersions = 20

// OutcomeFromState maps a run snapshot to its library outcome (ringer.py:1934-
// 1941, minus the "died" branch which only reconcile produces): live while
// running, else fail if any task failed/timed out, else pass.
func OutcomeFromState(rs state.RunState) string {
	if !rs.Done {
		return "live"
	}
	for _, t := range rs.Tasks {
		if t.Status == "failed" || t.Status == "timeout" {
			return "fail"
		}
	}
	return "pass"
}

// UpdateLibraryLive read-modify-writes the entry for run_name, setting the
// live fields and preserving any existing versions (ringer.py:1967-1989). The
// artifacts map is keyed by the RAW run_name.
func UpdateLibraryLive(stateDir, runName, runID, identity, livePath, entryState, nowISO string) error {
	lib := ReadLibrary(stateDir)
	prev := lib.Artifacts[runName] // zero Entry if absent; Versions nil is fine
	lib.Artifacts[runName] = Entry{
		LivePath:     livePath,
		State:        entryState,
		Identity:     identity,
		CurrentRunID: runID,
		UpdatedAt:    nowISO,
		Versions:     prev.Versions,
	}
	return WriteLibrary(stateDir, lib)
}

// VersionRecord bundles the fields AppendLibraryVersion needs to build a
// frozen version + flip the entry to its final outcome.
type VersionRecord struct {
	RunName, RunID, Identity, LivePath, VersionPath string
	ReportPath                                      *string
	Outcome                                         string
	TasksPass, TasksFail                            int
	Deliverables                                    []state.Deliverable
}

// AppendLibraryVersion prepends a new version (de-duping any prior version with
// the same run_id), rewrites the entry with state=outcome, caps to 20 kept, and
// prunes the pruned tail's files off disk (ringer.py:1992-2054).
func AppendLibraryVersion(stateDir string, r VersionRecord, nowISO string) error {
	lib := ReadLibrary(stateDir)
	prev := lib.Artifacts[r.RunName]
	newVersion := Version{
		RunID:        r.RunID,
		Path:         r.VersionPath,
		ReportPath:   r.ReportPath,
		FinishedAt:   nowISO,
		Outcome:      r.Outcome,
		TasksPass:    r.TasksPass,
		TasksFail:    r.TasksFail,
		Deliverables: r.Deliverables,
	}
	versions := []Version{newVersion}
	for _, v := range prev.Versions {
		if v.RunID != r.RunID {
			versions = append(versions, v)
		}
	}
	kept, pruned := versions, []Version(nil)
	if len(versions) > ArtifactLibraryMaxVersions {
		kept = versions[:ArtifactLibraryMaxVersions]
		pruned = versions[ArtifactLibraryMaxVersions:]
	}
	lib.Artifacts[r.RunName] = Entry{
		LivePath:     r.LivePath,
		State:        r.Outcome,
		Identity:     r.Identity,
		CurrentRunID: r.RunID,
		UpdatedAt:    nowISO,
		Versions:     kept,
	}
	if err := WriteLibrary(stateDir, lib); err != nil {
		return err
	}
	pruneVersions(stateDir, pruned)
	return nil
}

// pruneVersions deletes each pruned version's page + report file, but only when
// the path resolves strictly INSIDE the artifacts root (ringer.py:2039-2054).
// Best-effort: errors are swallowed (the JSON is already correct). Deliverables
// are never pruned.
func pruneVersions(stateDir string, pruned []Version) {
	root, err := filepath.EvalSymlinks(ArtifactsDir(stateDir))
	if err != nil {
		return
	}
	del := func(p string) {
		if p == "" {
			return
		}
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			return
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return // not strictly inside the artifacts root
		}
		if info, err := os.Stat(resolved); err == nil && info.Mode().IsRegular() {
			_ = os.Remove(resolved)
			_ = os.Remove(filepath.Dir(resolved)) // rmdir if now empty (best-effort)
		}
	}
	for _, v := range pruned {
		del(v.Path)
		if v.ReportPath != nil {
			del(*v.ReportPath)
		}
	}
}
```

- [ ] **Step 4: Run tests, expect PASS**

Run: `./build.sh --test 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/artifact/library_write.go internal/artifact/library_write_test.go
git commit -m "feat(artifact): library live-update, version append/dedup/cap, prune"
```

---

## Task 5: Runner harvest wiring

Wire `HarvestOnPass` into `runTask` (before worktree cleanup, exactly as Python does at `ringer.py:6902-6908`) and thread the harvested deliverables + a check-output tail + notes onto the task's terminal `TaskView` via the actor.

**Files:**
- Modify: `internal/runner/actor.go` (extend `opSetResult`)
- Modify: `internal/runner/runner.go:344-385` (harvest + richer setResult)
- Test: `internal/runner/runner_test.go` (extend the mock E2E)

**Interfaces:**
- Produces: `actor.setResult(key, status string, tokens int64, verified, logPath, ts string, deliverables []state.Deliverable, checkTail string, notes []string)` (extended signature); snapshots now carry per-task deliverables/check-tail/notes.

- [ ] **Step 1: Extend the actor.** In `internal/runner/actor.go`, add fields to `actorCmd`:

```go
	deliverables []state.Deliverable // opSetResult only
	checkTail    string              // opSetResult only
	notes        []string            // opSetResult only
```

In `run()`'s `opSetResult` case, after the existing assignments, add:

```go
				tv.Deliverables = c.deliverables
				tv.CheckTail = c.checkTail
				tv.DeliverableNotes = c.notes
```

Change `setResult` to:

```go
func (a *actor) setResult(key, status string, tokens int64, verified, logPath, ts string, deliverables []state.Deliverable, checkTail string, notes []string) {
	a.cmds <- actorCmd{op: opSetResult, key: key, status: status, tokens: tokens, verified: verified, logPath: logPath, ts: ts, deliverables: deliverables, checkTail: checkTail, notes: notes}
}
```

- [ ] **Step 2: Update the early-exit `setResult` calls in `runTask`.** In `internal/runner/runner.go`, the three early-failure `a.setResult(... )` calls (around lines 239, 249, 257) gain three trailing `nil, "", nil` args:

```go
		a.setResult(task.Key, "failed", -1, task.Verified, "", time.Now().UTC().Format(time.RFC3339), nil, "", nil)
```

- [ ] **Step 3: Harvest on PASS + capture the check tail.** In `runTask`, replace the tail of the function (the `if verdict == "PASS" { cleanupWorktreeOnPass(...) }` block through the final `setResult`, currently lines 379-384) with:

```go
	var deliverables []state.Deliverable
	var notes []string
	if verdict == "PASS" {
		var herr error
		deliverables, notes, herr = artifact.HarvestOnPass(
			opts.StateDir, runID, task.Key, taskDir, task.ExpectFiles, opts.Manifest.Worktrees)
		if herr != nil {
			lg.Warnf("task %s: harvest deliverables: %v", task.Key, herr)
		}
		cleanupWorktreeOnPass(opts.Manifest, lg, task.Key, taskDir, logsDir)
	}

	checkTail := capTail(lastCheckOutput, failureContextMax)
	a.setResult(task.Key, verdictToStatus(verdict), tokens, task.Verified, logPath,
		time.Now().UTC().Format(time.RFC3339), deliverables, checkTail, notes)
	lg.Infof("task %s: %s (%d attempt(s), tokens=%d)", task.Key, verdict, attempts, tokens)
```

Add `"github.com/corruptmemory/ringer/internal/artifact"` to `runner.go` imports. To have `lastCheckOutput` available: in the attempt loop, capture the verify output each attempt — after `vres := verify.Verify(...)` (line 344) add `lastCheckOutput = vres.Output`, and declare `var lastCheckOutput string` alongside `attempts := 0` (line 278). (Harvest must run **before** `cleanupWorktreeOnPass`, which removes the worktree — declared deliverables would vanish otherwise.)

- [ ] **Step 4: Extend the E2E test.** In `internal/runner/runner_test.go`, in the existing mock pass-task test, after asserting the run passed, assert the run-state snapshot carries the deliverable. The mock `alpha` task writes `alpha.txt` and declares `expect_files: ["alpha.txt"]`, so:

```go
	// alpha declared expect_files -> its deliverable is harvested + recorded.
	data, _ := os.ReadFile(filepath.Join(stateDir, "runs", res.RunID+".json"))
	var rs state.RunState
	if err := json.Unmarshal(data, &rs); err != nil {
		t.Fatal(err)
	}
	var alpha state.TaskView
	for _, tv := range rs.Tasks {
		if tv.Key == "alpha" {
			alpha = tv
		}
	}
	if len(alpha.Deliverables) != 1 || alpha.Deliverables[0].Name != "alpha.txt" {
		t.Errorf("alpha deliverable not harvested into state: %+v", alpha.Deliverables)
	}
	if _, err := os.Stat(alpha.Deliverables[0].Path); err != nil {
		t.Errorf("harvested file missing on disk: %v", err)
	}
```

(Add `"encoding/json"` to the test imports if not present. If the existing mock manifest's `alpha` lacks `expect_files`, add `ExpectFiles: []string{"alpha.txt"}` to it — mirroring the demo manifest.)

- [ ] **Step 5: Run the suite, expect PASS**

Run: `./build.sh --test 2>&1 | tail -25`
Expected: PASS — runner, actor, state, artifact all green; the mock run now harvests.

- [ ] **Step 6: Commit**

```bash
git add internal/runner/actor.go internal/runner/runner.go internal/runner/runner_test.go
git commit -m "feat(runner): harvest deliverables on PASS + carry check-tail/notes on TaskView"
```

---

## Task 6: Lift ARTIFACT_BASE_CSS into an embedded stylesheet

The artifact pages inline their CSS (the CSP forbids external stylesheets). Lift `ARTIFACT_BASE_CSS` (`ringer.py:2198-2719`) **verbatim** into a committed file and `go:embed` it — exactly as Plan 4 lifted `ringside.css`. Also expose the CSP meta string as a Go constant.

**Files:**
- Create: `internal/hud/static/artifact.css` (verbatim lift)
- Create: `internal/hud/views/artifact_css.go` (embed + CSP constant)
- Test: `internal/hud/views/artifact_css_test.go`

**Interfaces:**
- Produces: `views.ArtifactCSS string` (the embedded stylesheet body), `views.CSPMeta string` (the CSP `<meta>` tag, verbatim from Global Constraints).

- [ ] **Step 1: Lift the CSS.** Copy the exact contents of `ringer.py:2198-2719` (the triple-quoted `ARTIFACT_BASE_CSS` string body, without the Python quotes) into `internal/hud/static/artifact.css`. Do not edit selectors or values — it is a verbatim lift. (Note the known upstream quirk: `.chip` backgrounds reference `var(--running)`/`status_color` which isn't defined in the token block; leave it as-is for parity — the index page sets chip color inline anyway, Task 10.)

- [ ] **Step 2: Embed it.** Create `internal/hud/views/artifact_css.go`:

```go
package views

import _ "embed"

//go:embed ../static/artifact.css
var ArtifactCSS string

// CSPMeta is the Content-Security-Policy meta tag every artifact HTML document
// carries (ringer.py:82-85): no external anything, inline styles, data: images.
const CSPMeta = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:">`
```

(The `go:embed` path is relative to the Go file; `internal/hud/static/artifact.css` sits one dir up from `views/`. If `go:embed` cannot traverse `..`, instead place a copy at `internal/hud/views/artifact.css` and embed `artifact.css` directly — decide by what compiles; Plan 4's `ringside.css` is embedded from `internal/hud/static.go` in package `hud`, so the `..` traversal is the untested path. Prefer a `views/`-local embed if `..` fails.)

- [ ] **Step 3: Sanity test** `internal/hud/views/artifact_css_test.go`:

```go
package views

import "testing"

func TestArtifactCSSEmbedded(t *testing.T) {
	if len(ArtifactCSS) < 1000 {
		t.Fatalf("artifact.css looks empty (%d bytes)", len(ArtifactCSS))
	}
	for _, sel := range []string{".page", ".corner", ".live-dot", ".work-group", ".glyph", ".rounds"} {
		if !contains(ArtifactCSS, sel) {
			t.Errorf("artifact.css missing selector %q", sel)
		}
	}
}

func contains(hay, needle string) bool { return len(hay) >= len(needle) && (indexOf(hay, needle) >= 0) }
func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Build + test**

Run: `./build.sh --test 2>&1 | tail -15`
Expected: PASS. If the embed failed to compile, apply the `views/`-local fallback from Step 2.

- [ ] **Step 5: Commit**

```bash
git add internal/hud/static/artifact.css internal/hud/views/artifact_css.go internal/hud/views/artifact_css_test.go
git commit -m "feat(hud): lift ARTIFACT_BASE_CSS into embedded artifact.css + CSP constant"
```

---

## Task 7: Artifact render helpers + run-level chrome components

Port the pure derivation helpers new to artifacts and the run-level page chrome: the artifact **corner header** (packs run_name+identity in one `.eyebrow`, `.clock` shows elapsed), the **progress bar** (`.rounds` of per-task chips + `.legend`), and the **briefing** heading (live vs final prose). Reuse the existing `TaskKind`/`PassCount`/`FailCount`/`RunElapsed`/`FormatDuration` helpers.

**Files:**
- Create: `internal/hud/views/artifact_render.go`
- Create: `internal/hud/views/artifact_chrome.templ`
- Test: `internal/hud/views/artifact_render_test.go`

**Port from:** `render_corner_header` (`ringer.py:3429-3450`), `render_progress_bar` (`3158-3188`), `live_briefing_html` (`3013-3040`), `final_briefing_html` (`3041-3070`), `task_state_bucket`/`task_state_word` (`3128-3157`). Use `TaskKind`/`TaskStateText` (already in `render.go`/`text.go`) for the buckets/words — they already match the Python `pass|fail|retry|working|waiting` vocabulary.

**Interfaces:**
- Produces:
  - `func BriefingLive(rs state.RunState) string` / `func BriefingFinal(rs state.RunState) string` — the plain-language heading text (port the exact sentences from the cited Python; the golden locks them).
  - `templ artifactCorner(rs state.RunState, live bool)` — `header.corner` with `live-dot` (class `is-live` when live, else the final bucket), `eyebrow` = `Ringer · <b>{run_name}</b> · {identity}`, `clock.mono` = `{elapsed} elapsed` (live) / `{elapsed} total` (final).
  - `templ progressBar(rs state.RunState)` — `div.rounds` with one `<span class={ TaskKind(t) }>` per task, then `p.legend`.
  - `templ briefingHeading(rs state.RunState, live bool)` — `h1.briefing` (id `right-now-heading` live / `what-happened-heading` final) rendering `BriefingLive`/`BriefingFinal`.

- [ ] **Step 1: Write the failing helper test** `internal/hud/views/artifact_render_test.go`:

```go
package views

import (
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/state"
)

func TestBriefingLiveAndFinal(t *testing.T) {
	live := state.RunState{RunName: "run", StartedAt: "2026-07-10T10:00:00Z", UpdatedAt: "2026-07-10T10:00:20Z",
		Tasks: []state.TaskView{{Status: "running"}, {Status: "passed"}}}
	if b := BriefingLive(live); !strings.Contains(b, "2") { // "working on N tasks"
		t.Errorf("live briefing missing task count: %q", b)
	}
	final := state.RunState{RunName: "run", Done: true, StartedAt: "2026-07-10T10:00:00Z", UpdatedAt: "2026-07-10T10:01:04Z",
		Tasks: []state.TaskView{{Status: "passed"}, {Status: "passed"}, {Status: "passed"}}}
	if b := BriefingFinal(final); !strings.Contains(b, "3") {
		t.Errorf("final briefing missing task count: %q", b)
	}
}
```

- [ ] **Step 2: Run it, expect failure** — `BriefingLive`/`BriefingFinal` undefined.

Run: `./build.sh --test 2>&1 | grep -iE "Briefing|undefined" | head`

- [ ] **Step 3: Implement `internal/hud/views/artifact_render.go`.** Port the exact sentence templates from `live_briefing_html` (`ringer.py:3013-3040`) and `final_briefing_html` (`3041-3070`). Structure:

```go
package views

import (
	"fmt"

	"github.com/corruptmemory/ringer/internal/state"
)

// BriefingLive is the status page's plain-language heading (port of
// live_briefing_html, ringer.py:3013-3040): e.g. "Ringer is working on N
// tasks — M finished, K to go, started X ago." Reproduce the exact wording
// from the cited function; the golden test in Task 8/9 locks the bytes.
func BriefingLive(rs state.RunState) string {
	n := len(rs.Tasks)
	pass := PassCount(rs)
	// ... port exact phrasing incl. the "started <elapsed> ago" clause ...
	return fmt.Sprintf("Ringer is working on %d tasks — %d finished so far.", n, pass) // REPLACE with exact port
}

// BriefingFinal is the final report heading (port of final_briefing_html,
// ringer.py:3041-3070): e.g. "Ringer finished N tasks in <elapsed>. All N
// finished and checked." / "... M finished and checked, K failed after retry."
func BriefingFinal(rs state.RunState) string {
	n := len(rs.Tasks)
	pass, fail := PassCount(rs), FailCount(rs)
	elapsed := FormatDuration(RunElapsed(rs))
	if fail == 0 {
		return fmt.Sprintf("Ringer finished %d tasks in %s. All %d finished and checked.", n, elapsed, n)
	}
	return fmt.Sprintf("Ringer finished %d tasks in %s. %d finished and checked, %d failed after retry.", n, elapsed, pass, fail)
}
```

> The two `// REPLACE`/exact-wording notes mean: open the cited Python function and reproduce its user-facing sentences verbatim (including the "started … ago" phrasing that `live_briefing_html` builds). The golden file committed in Task 8 is the authority; if the Python wording and the golden ever disagree, the golden wins for the Go renderer.

- [ ] **Step 4: Implement `internal/hud/views/artifact_chrome.templ`.** Emit the exact structure below (classes must all exist in `artifact.css`):

```templ
package views

import "github.com/corruptmemory/ringer/internal/state"

templ artifactCorner(rs state.RunState, live bool) {
	<header class="corner">
		if live {
			<span class="live-dot is-live" aria-hidden="true"></span>
		} else {
			<span class={ "live-dot", RunState(rs) } aria-hidden="true"></span>
		}
		<span class="eyebrow">Ringer · <b>{ rs.RunName }</b> · { rs.Identity }</span>
		if live {
			<span class="clock mono">{ FormatDuration(RunElapsed(rs)) } elapsed</span>
		} else {
			<span class="clock mono">{ FormatDuration(RunElapsed(rs)) } total</span>
		}
	</header>
}

templ progressBar(rs state.RunState) {
	<div class="rounds">
		for _, t := range rs.Tasks {
			<span class={ TaskKind(t) }></span>
		}
	</div>
	<p class="legend">pass · fail · working · waiting</p>
}

templ briefingHeading(rs state.RunState, live bool) {
	if live {
		<h1 id="right-now-heading" class="briefing">{ BriefingLive(rs) }</h1>
	} else {
		<h1 id="what-happened-heading" class="briefing">{ BriefingFinal(rs) }</h1>
	}
}
```

(Match the `.legend` text to the Python `render_progress_bar` legend line at `ringer.py:3180-3187`; the golden locks it.)

- [ ] **Step 5: Build + run helper tests, expect PASS**

Run: `./build.sh --test 2>&1 | tail -15`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/hud/views/artifact_render.go internal/hud/views/artifact_chrome.templ internal/hud/views/artifact_chrome_templ.go internal/hud/views/artifact_render_test.go
git commit -m "feat(hud): artifact page chrome — corner header, progress bar, briefing"
```

---

## Task 8: Work section — work-group, work-item, deliverable classification, task links

Port the per-task work list: `render_work_section`/`render_work_group`/`render_work_item`/`render_task_links` (`ringer.py:3189-3428, 3532-3591`) and the deliverable classification + href helpers (`work_label_and_kind` `3398-3406`, `is_text/image_deliverable` `3410-3415`, `image_data_uri` `3418-3427`, `deliverable_title` `2738-2743`). This is where the artifact pages diverge from the dashboard `taskRow`: each task is a `.work-group` (worker row **plus** a `.work-group-body` with the deliverable list, verification proof drawer, and task links).

**Files:**
- Modify: `internal/hud/views/artifact_render.go` (classification + href + data-URI helpers)
- Create: `internal/hud/views/artifact_work.templ`
- Test: `internal/hud/views/artifact_render_test.go` (classification cases)

**Interfaces:**
- Produces:
  - `func DeliverableKind(name string) string` → `web page|image|document|download` (`.html`/`.htm`→web page; image suffix→image; text suffix→document; else download).
  - `func DeliverableTitle(name string) string` → "Work log" / "What this worker produced" / capitalized stem (port `deliverable_title`).
  - `func IsTextDeliverable(name string) bool`, `func IsImageDeliverable(name string) bool`.
  - `func ImageDataURI(path string) string` → `data:<mime>;base64,<...>` for a small image, or `""` on error/oversize (port `image_data_uri`, incl. its size guard).
  - `func DeliverableHref(d state.Deliverable, stateDir string) string` → for a text deliverable, the wrapper page path (`view/…--….html`) relative to the artifacts dir; else the raw `deliverables/…` relative path. (Relative to the artifacts dir so it resolves both over HTTP `/artifacts/` and `file://`.)
  - `func WrapperRelPath(runID, taskKey, sourceName string) string` → `view/<sanitize(runID)>/<sanitize(taskKey)>--<sanitize(sourceName)>.html`.
  - `templ workSection(rs state.RunState, stateDir string, primary bool)`, `templ workGroup(rs state.RunState, t state.TaskView, stateDir string)`, `templ workItem(d state.Deliverable, stateDir string)`, `templ taskLinks(t state.TaskView, stateDir string)`.

- [ ] **Step 1: Write failing classification tests** (append to `artifact_render_test.go`):

```go
func TestDeliverableKind(t *testing.T) {
	cases := map[string]string{
		"chart.png": "image", "photo.SVG": "image",
		"notes.md": "document", "log.txt": "document",
		"page.html": "web page", "p.HTM": "web page",
		"data.json": "download", "archive.zip": "download",
	}
	for name, want := range cases {
		if got := DeliverableKind(name); got != want {
			t.Errorf("DeliverableKind(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestWrapperRelPathSanitized(t *testing.T) {
	got := WrapperRelPath("run 1", "task/a", "my report.md")
	want := "view/run-1/task-a--my-report.md.html"
	if got != want {
		t.Errorf("WrapperRelPath = %q, want %q", got, want)
	}
}

func TestDeliverableHrefTextWrapsImagesRaw(t *testing.T) {
	sd := "/x"
	txt := state.Deliverable{TaskKey: "a", Name: "notes.md", Path: "/x/artifacts/deliverables/r/a/notes.md"}
	if h := DeliverableHref(txt, sd); h != "view/r/a--notes.md.html" { // relative to artifacts dir; run/task sanitized upstream
		t.Errorf("text deliverable should link to wrapper, got %q", h)
	}
	img := state.Deliverable{TaskKey: "a", Name: "c.png", Path: "/x/artifacts/deliverables/r/a/c.png"}
	if h := DeliverableHref(img, sd); h != "deliverables/r/a/c.png" {
		t.Errorf("image should link raw, got %q", h)
	}
}
```

(Note: `DeliverableHref` needs the run_id + task_key for the wrapper name — carry them on the `state.Deliverable` (it has `TaskKey`) and pass run_id via the enclosing render; adjust the helper signature to `DeliverableHref(d state.Deliverable, runID, stateDir string)` if the test shows run_id is needed. Make the test match the final signature.)

- [ ] **Step 2: Run, expect failure.**

Run: `./build.sh --test 2>&1 | grep -iE "DeliverableKind|WrapperRelPath|DeliverableHref|undefined" | head`

- [ ] **Step 3: Implement the classification/href helpers** in `artifact_render.go`. Port the suffix sets + logic:

```go
var textDeliverableSuffixes = map[string]bool{".md": true, ".txt": true, ".log": true}
var imageDeliverableSuffixes = map[string]bool{".avif": true, ".gif": true, ".jpeg": true, ".jpg": true, ".png": true, ".svg": true, ".webp": true}

func IsTextDeliverable(name string) bool  { return textDeliverableSuffixes[strings.ToLower(filepath.Ext(name))] }
func IsImageDeliverable(name string) bool { return imageDeliverableSuffixes[strings.ToLower(filepath.Ext(name))] }

// DeliverableKind labels a deliverable for the results page (ringer.py:3398-3406).
func DeliverableKind(name string) string {
	switch ext := strings.ToLower(filepath.Ext(name)); {
	case ext == ".html" || ext == ".htm":
		return "web page"
	case imageDeliverableSuffixes[ext]:
		return "image"
	case textDeliverableSuffixes[ext]:
		return "document"
	default:
		return "download"
	}
}

// DeliverableTitle (ringer.py:2738-2743).
func DeliverableTitle(name string) string { /* worker.log->"Work log"; report.md/html->"What this worker produced"; else Title(stem) */ }

// WrapperRelPath is view/<sanitize(runID)>/<sanitize(taskKey)>--<sanitize(name)>.html.
func WrapperRelPath(runID, taskKey, sourceName string) string {
	return filepath.ToSlash(filepath.Join("view", artifact.SanitizeName(runID),
		artifact.SanitizeName(taskKey)+"--"+artifact.SanitizeName(sourceName)+".html"))
}

// DeliverableHref returns the artifacts-dir-relative link for a deliverable:
// text -> its wrapper page; anything else -> the raw copied file.
func DeliverableHref(d state.Deliverable, runID, stateDir string) string {
	if IsTextDeliverable(d.Name) {
		return WrapperRelPath(runID, d.TaskKey, d.Name)
	}
	// raw file: make d.Path relative to the artifacts dir.
	rel, err := filepath.Rel(artifact.ArtifactsDir(stateDir), d.Path)
	if err != nil {
		return d.Path
	}
	return filepath.ToSlash(rel)
}

// ImageDataURI reads a small image and returns a data: URI, or "" on error /
// over the inline size guard (port image_data_uri, ringer.py:3418-3427 — keep
// its byte cap).
func ImageDataURI(path string) string { /* read, cap, base64, mime by ext */ }
```

Add imports (`encoding/base64`, `os`, `path/filepath`, `strings`, the `artifact` package). Port `DeliverableTitle`/`ImageDataURI` bodies from the cited functions.

- [ ] **Step 4: Implement `internal/hud/views/artifact_work.templ`.** Structure (from `ringer.py:3189-3428`; classes must exist in `artifact.css`):

```templ
package views

import (
	"fmt"
	"net/url"

	"github.com/corruptmemory/ringer/internal/state"
)

templ workSection(rs state.RunState, stateDir string, primary bool) {
	<section class={ "work", templ.KV("is-primary", primary) }>
		<h2 id="the-work-heading">The work</h2>
		<div class="work-list">
			for _, t := range rs.Tasks {
				@workGroup(rs, t, stateDir)
			}
		</div>
	</section>
}

templ workGroup(rs state.RunState, t state.TaskView, stateDir string) {
	<div class="work-group">
		<div class="worker">
			<span class={ "glyph", TaskKind(t) } aria-hidden="true"></span>
			<span class="name" title={ t.Key }>{ t.Key }</span>
			<span class={ "state", TaskKind(t) }>{ TaskStateText(TaskKind(t)) }</span>
			<span class="time mono">{ FormatDuration(TaskElapsed(t, rs.UpdatedAt)) }</span>
		</div>
		<div class="work-group-body">
			if len(t.Deliverables) == 0 {
				<p class="empty-note">No files captured.</p>
			} else {
				<div class="work-list">
					for _, d := range t.Deliverables {
						@workItem(rs.RunID, d, stateDir)
					}
				</div>
			}
			for _, n := range t.DeliverableNotes {
				<p class="omitted-note">{ n }</p>
			}
			if t.Verified != "" {
				<p class="verified">{ t.Verified }</p>
			}
			if t.CheckTail != "" {
				<details class="proof"><summary>proof</summary><pre>{ t.CheckTail }</pre></details>
			}
			<span class="links">@taskLinks(rs.RunID, t, stateDir)</span>
		</div>
	</div>
}

templ workItem(runID string, d state.Deliverable, stateDir string) {
	<div class="work-item">
		if IsImageDeliverable(d.Name) && ImageDataURI(d.Path) != "" {
			<a class="work-thumb-link" href={ templ.SafeURL(DeliverableHref(d, runID, stateDir)) }>
				<img class="work-thumb" src={ ImageDataURI(d.Path) } alt={ d.Name }/>
			</a>
		}
		<div class="work-main">
			<a class="work-link" href={ templ.SafeURL(DeliverableHref(d, runID, stateDir)) }>{ fmt.Sprintf("%s — %s", d.Name, DeliverableKind(d.Name)) }</a>
			<div class="work-kind">{ DeliverableKind(d.Name) }</div>
		</div>
	</div>
}

templ taskLinks(runID string, t state.TaskView, stateDir string) {
	if t.LogPath != "" {
		<a href={ templ.SafeURL(WrapperRelPath(runID, t.Key, "worker.log")) }>view the work log</a>
	}
}
```

> Match the `state`/proof/links wording and any `.activity` line for working/retry tasks to `render_work_group` (`ringer.py:3296-3428`); the golden in Task 9 locks the exact bytes. `taskLinks` should also emit the "Read what it found" report link when a `report.md`/`report.html` deliverable exists (port `render_task_links`, `ringer.py:3532-3591`).

- [ ] **Step 5: Build + run classification tests, expect PASS**

Run: `./build.sh --test 2>&1 | tail -15`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/hud/views/artifact_render.go internal/hud/views/artifact_work.templ internal/hud/views/artifact_work_templ.go internal/hud/views/artifact_render_test.go
git commit -m "feat(hud): artifact work list — work-group/item, deliverable classification, links"
```

---

## Task 9: Status page + final report page

Assemble the two per-run page shells from the chrome (Task 7) + work section (Task 8), and lock them with golden files. The status page self-refreshes (`<meta refresh 2>`); the final report is frozen (no refresh, `.work.is-primary`).

**Files:**
- Create: `internal/hud/views/artifact_pages.templ` (`StatusPage`, `FinalReportPage`)
- Test: `internal/hud/views/artifact_pages_test.go` + `internal/hud/views/testdata/status_page.golden.html`, `final_report.golden.html`

**Port from:** `render_status_html` (`ringer.py:3451-3488`) and `render_final_report_html` (`3489-3531`).

**Interfaces:**
- Produces: `templ StatusPage(rs state.RunState, stateDir string)`, `templ FinalReportPage(rs state.RunState, stateDir string)`.

- [ ] **Step 1: Implement the page shells** in `artifact_pages.templ`:

```templ
package views

import "github.com/corruptmemory/ringer/internal/state"

templ pageHead(title string, refreshSeconds int) {
	<head>
		<meta charset="utf-8"/>
		@templ.Raw(CSPMeta)
		if refreshSeconds > 0 {
			<meta http-equiv="refresh" content={ intToString(refreshSeconds) }/>
		}
		<title>{ title }</title>
		<style>@templ.Raw(ArtifactCSS)</style>
	</head>
}

templ StatusPage(rs state.RunState, stateDir string) {
	<!doctype html>
	<html lang="en">
		@pageHead("ringer · "+rs.RunName, 2)
		<body>
			<div class="page">
				@artifactCorner(rs, true)
				@briefingHeading(rs, true)
				@progressBar(rs)
				@workSection(rs, stateDir, false)
				<footer>Updated by ringer · this page refreshes while the work runs.</footer>
			</div>
		</body>
	</html>
}

templ FinalReportPage(rs state.RunState, stateDir string) {
	<!doctype html>
	<html lang="en">
		@pageHead("ringer report · "+rs.RunName, 0)
		<body>
			<div class="page">
				@artifactCorner(rs, false)
				@briefingHeading(rs, false)
				@progressBar(rs)
				@workSection(rs, stateDir, true)
				<footer>Finished by ringer.</footer>
			</div>
		</body>
	</html>
}
```

Add a small `intToString` helper to `artifact_render.go` (`strconv.Itoa`) if templ can't inline it. Match footer/title text to the Python (`ringer.py:3466-3487` / `3505-3530`); the golden locks it.

- [ ] **Step 2: Write the golden test** `internal/hud/views/artifact_pages_test.go`:

```go
package views

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/state"
)

var update = flag.Bool("update", false, "update golden files")

func renderToString(t *testing.T, c interface{ Render(context.Context, ...any) }) string { /* use templ.Component.Render into a strings.Builder */ }

func fixedRun() state.RunState {
	return state.RunState{
		RunID: "run-123", RunName: "demo", Identity: "godlike-artix",
		StartedAt: "2026-07-10T10:00:00Z", UpdatedAt: "2026-07-10T10:01:04Z", Done: true,
		Tasks: []state.TaskView{
			{Key: "alpha", Status: "passed", Engine: "mock", StartedAt: "2026-07-10T10:00:00Z", EndedAt: "2026-07-10T10:00:05Z",
				Verified: "alpha.txt exists", CheckTail: "ok\n",
				Deliverables: []state.Deliverable{{TaskKey: "alpha", Name: "alpha.txt", Path: "/s/artifacts/deliverables/run-123/alpha/alpha.txt", Bytes: 11}}},
			{Key: "bravo", Status: "failed", Engine: "mock", StartedAt: "2026-07-10T10:00:00Z", EndedAt: "2026-07-10T10:00:07Z"},
		},
	}
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		_ = os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with -update first): %v", name, err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch; run `./build.sh --test` after `-update` to refresh", name)
	}
}

func TestStatusAndFinalGoldens(t *testing.T) {
	rs := fixedRun()
	status := renderComponentString(t, StatusPage(rs, "/s"))
	assertGolden(t, "status_page.golden.html", status)
	// contract sanity independent of golden bytes:
	for _, must := range []string{"refresh", "class=\"page\"", "alpha", "work-group", "glyph pass", "glyph fail"} {
		if !strings.Contains(status, must) {
			t.Errorf("status page missing %q", must)
		}
	}
	final := renderComponentString(t, FinalReportPage(rs, "/s"))
	assertGolden(t, "final_report.golden.html", final)
	if strings.Contains(final, "http-equiv=\"refresh\"") {
		t.Error("final report must not self-refresh")
	}
	if !strings.Contains(final, "is-primary") {
		t.Error("final report work section must be primary")
	}
}
```

Implement `renderComponentString(t, templ.Component) string` (render the component into a `strings.Builder` via `component.Render(context.Background(), &sb)`).

- [ ] **Step 3: Generate the goldens, then verify**

Run: `./build.sh 2>&1 | tail -3 && go test ./internal/hud/views/ -run TestStatusAndFinalGoldens -update 2>&1 | tail -3` *(the one place a direct `go test -update` is acceptable — it only writes fixtures; re-run `./build.sh --test` after to prove the committed goldens pass)*
Then: `./build.sh --test 2>&1 | tail -15`
Expected: PASS. Eyeball `testdata/status_page.golden.html` in a browser to confirm it looks like the Python status page before committing.

- [ ] **Step 4: Commit**

```bash
git add internal/hud/views/artifact_pages.templ internal/hud/views/artifact_pages_templ.go internal/hud/views/artifact_pages_test.go internal/hud/views/testdata/status_page.golden.html internal/hud/views/testdata/final_report.golden.html
git commit -m "feat(hud): artifact status + final report pages (+ goldens)"
```

---

## Task 10: All-runs index page

Port `render_artifact_index_html` (`ringer.py:3592-3660`): a self-refreshing (`<meta refresh 5>`) table over every run-state file (newest first), one row per run with a status chip, run name, identity, pass/fail, elapsed, and links to that run's live page + report. Links are `file://` URIs so the page works opened directly from disk.

**Files:**
- Modify: `internal/hud/views/artifact_pages.templ` (`IndexPage`)
- Modify: `internal/hud/views/artifact_render.go` (`IndexRow` builder + `StatusColor`)
- Test: `internal/hud/views/artifact_pages_test.go` (index golden)

**Interfaces:**
- Produces:
  - `type IndexRow struct { RunName, Identity, State, Elapsed, LiveHref, ReportHref string; Pass, Fail int }`
  - `func BuildIndexRows(runs []state.RunState, stateDir string) []IndexRow` — one row per run; `LiveHref`/`ReportHref` are `file://` URIs to `artifacts/<run_id>.html` and (if the run is done) `artifacts/<run_id>-report.html`.
  - `func StatusColor(state string) string` — the chip background (port `status_color`; map live/running→accent, pass→--pass, fail→--fail, waiting→--waiting).
  - `templ IndexPage(rows []IndexRow)`.

- [ ] **Step 1: Write the failing test** (append to `artifact_pages_test.go`):

```go
func TestIndexPageGolden(t *testing.T) {
	runs := []state.RunState{
		{RunID: "run-123", RunName: "demo", Identity: "id", Done: true, StartedAt: "2026-07-10T10:00:00Z", UpdatedAt: "2026-07-10T10:01:04Z",
			Tasks: []state.TaskView{{Status: "passed"}, {Status: "passed"}}},
		{RunID: "run-124", RunName: "live-run", Identity: "id", Done: false, StartedAt: "2026-07-10T10:02:00Z", UpdatedAt: "2026-07-10T10:02:20Z",
			Tasks: []state.TaskView{{Status: "running"}}},
	}
	rows := BuildIndexRows(runs, "/s")
	page := renderComponentString(t, IndexPage(rows))
	assertGolden(t, "index_page.golden.html", page)
	for _, must := range []string{"refresh", "demo", "live-run", "file://", "<table"} {
		if !strings.Contains(page, must) {
			t.Errorf("index page missing %q", must)
		}
	}
}
```

- [ ] **Step 2: Run, expect failure.** `BuildIndexRows`/`IndexPage`/`StatusColor` undefined.

- [ ] **Step 3: Implement `BuildIndexRows`/`StatusColor`** in `artifact_render.go` and `IndexPage` in `artifact_pages.templ`. `file://` href = `"file://" + filepath.Join(artifact.ArtifactsDir(stateDir), runID+".html")`. Row state comes from `OutcomeFromState`-equivalent on the run (or `RunState(rs)` bucket). Table columns + header text match `ringer.py:3620-3658`. Chip uses `style={ "background:"+StatusColor(row.State) }` (inline, as Python does).

```templ
templ IndexPage(rows []IndexRow) {
	<!doctype html>
	<html lang="en">
		@pageHead("ringer · all runs", 5)
		<body>
			<div class="wrap">
				<h1>ringer — all runs</h1>
				<p class="meta">{ fmt.Sprintf("%d runs", len(rows)) }</p>
				<table>
					<thead><tr><th>State</th><th>Run</th><th>Identity</th><th>Result</th><th>Elapsed</th><th>Artifacts</th></tr></thead>
					<tbody>
						for _, r := range rows {
							<tr>
								<td><span class="chip" style={ "background:" + StatusColor(r.State) }>{ r.State }</span></td>
								<td>{ r.RunName }</td>
								<td>{ r.Identity }</td>
								<td>{ fmt.Sprintf("%d pass / %d fail", r.Pass, r.Fail) }</td>
								<td class="mono">{ r.Elapsed }</td>
								<td class="run-links">
									<a href={ templ.SafeURL(r.LiveHref) }>live</a>
									if r.ReportHref != "" {
										{ " · " }
										<a href={ templ.SafeURL(r.ReportHref) }>report</a>
									}
								</td>
							</tr>
						}
					</tbody>
				</table>
			</div>
		</body>
	</html>
}
```

(`.wrap` is intentionally unstyled by `artifact.css` — parity with Python, which only styles `.page`. Keep it.)

- [ ] **Step 4: Generate golden + verify**

Run: `./build.sh 2>&1 | tail -3 && go test ./internal/hud/views/ -run TestIndexPageGolden -update 2>&1 | tail -3 && ./build.sh --test 2>&1 | tail -12`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/hud/views/artifact_render.go internal/hud/views/artifact_pages.templ internal/hud/views/artifact_pages_templ.go internal/hud/views/artifact_pages_test.go internal/hud/views/testdata/index_page.golden.html
git commit -m "feat(hud): all-runs artifact index page (+ golden)"
```

---

## Task 11: Text file-wrapper page

Port `render_file_wrapper_html`/`write_wrapper` (`ringer.py:2843-2926`): a standalone page wrapping a text deliverable or the worker log in `<pre>`, showing only the **last 256 KiB** with a truncation banner when the file is larger. Images are never wrapped (they link raw); this page is text-only.

**Files:**
- Modify: `internal/hud/views/artifact_pages.templ` (`FileWrapperPage`)
- Modify: `internal/hud/views/artifact_render.go` (`ReadTail`)
- Test: `internal/hud/views/artifact_pages_test.go` (wrapper golden + tail behavior)

**Interfaces:**
- Produces:
  - `const ArtifactWrapperTailBytes = 256 * 1024`
  - `func ReadTail(path string, max int) (content string, size int64, truncated bool)` — reads the last `max` bytes UTF-8 (invalid bytes replaced), returns full-file size + whether truncated.
  - `type WrapperData struct { RunName, TaskKey, Title, MetaLine, Content string }`
  - `templ FileWrapperPage(d WrapperData)`.

- [ ] **Step 1: Write the failing test** (append to `artifact_pages_test.go`):

```go
func TestReadTailTruncates(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.log")
	big := make([]byte, ArtifactWrapperTailBytes+100)
	for i := range big {
		big[i] = 'x'
	}
	copy(big[len(big)-3:], []byte("END"))
	_ = os.WriteFile(p, big, 0o644)
	content, size, truncated := ReadTail(p, ArtifactWrapperTailBytes)
	if !truncated || size != int64(len(big)) {
		t.Fatalf("truncated=%v size=%d", truncated, size)
	}
	if len(content) != ArtifactWrapperTailBytes || !strings.HasSuffix(content, "END") {
		t.Errorf("tail wrong: len=%d", len(content))
	}
}

func TestFileWrapperGolden(t *testing.T) {
	d := WrapperData{RunName: "demo", TaskKey: "alpha", Title: "Work log",
		MetaLine: "alpha produced this.", Content: "line one\nline two\n"}
	page := renderComponentString(t, FileWrapperPage(d))
	assertGolden(t, "file_wrapper.golden.html", page)
	for _, must := range []string{"<pre>", "line one", "class=\"page\""} {
		if !strings.Contains(page, must) {
			t.Errorf("wrapper missing %q", must)
		}
	}
}
```

- [ ] **Step 2: Run, expect failure.**

- [ ] **Step 3: Implement `ReadTail`** in `artifact_render.go` (seek to `size-max` when larger; decode with `strings.ToValidUTF8(..., "�")`) and `FileWrapperPage` in `artifact_pages.templ`:

```templ
templ FileWrapperPage(d WrapperData) {
	<!doctype html>
	<html lang="en">
		@pageHead("ringer · "+d.RunName+" · "+d.TaskKey, 0)
		<body>
			<div class="page">
				<header class="corner">
					<span class="live-dot waiting" aria-hidden="true"></span>
					<span class="eyebrow">Ringer · <b>{ d.RunName }</b> · { d.TaskKey }</span>
					<span class="clock mono">artifact</span>
				</header>
				<section class="timeline">
					<h1 class="briefing">{ d.Title }</h1>
					<p class="meta">@templ.Raw(d.MetaLine)</p>
					<pre>{ d.Content }</pre>
				</section>
			</div>
		</body>
	</html>
}
```

The truncation banner in `MetaLine` (built by the caller/Task 12): `"Showing the last <b>262,144</b> bytes of <b>{size}</b>."` with comma-grouped numbers — port from `ringer.py:2885-2890`. (Only `MetaLine` uses raw HTML for the `<b>` tags; `Content` is auto-escaped by templ.)

- [ ] **Step 4: Generate golden + verify + commit**

Run: `./build.sh 2>&1 | tail -3 && go test ./internal/hud/views/ -run TestFileWrapperGolden -update 2>&1 | tail -3 && ./build.sh --test 2>&1 | tail -12`
Expected: PASS.

```bash
git add internal/hud/views/artifact_render.go internal/hud/views/artifact_pages.templ internal/hud/views/artifact_pages_templ.go internal/hud/views/artifact_pages_test.go internal/hud/views/testdata/file_wrapper.golden.html
git commit -m "feat(hud): text file-wrapper page + 256KiB tail"
```

---

## Task 12: The artifact writer

The orchestrator that turns run-state snapshots into the on-disk artifact tree and keeps `library.json` current. It renders pages (Tasks 9-11), generates wrapper pages for text deliverables + the worker log (Task 11), and records page paths into the library (Task 4) with the 5-second live-throttle. It implements the runner's `ArtifactWriter` interface.

**Files:**
- Create: `internal/hud/artifactwriter/writer.go`
- Test: `internal/hud/artifactwriter/writer_test.go`

**Interfaces:**
- Consumes: `views.StatusPage/FinalReportPage/IndexPage/FileWrapperPage`, `views.ReadTail`, `views.WrapperRelPath`, `artifact.UpdateLibraryLive/AppendLibraryVersion/OutcomeFromState`, `state.ReadAllRunStates`, `artifact.SanitizeName`, `artifact.ArtifactsDir`.
- Produces:
  - `type Writer struct { ... }` with `func New(stateDir string, cfg Config, lg logging.Logger) *Writer` where `Config` carries the resolved `out`/`report_out`/`index_out` templates + paths.
  - `func (w *Writer) Live(rs state.RunState)` — render `<run_id>.html` (status) + `live/<run_name>.html` + `index.html`; `UpdateLibraryLive` throttled to once per 5 s per unchanged outcome. Non-fatal; logs at Warn.
  - `func (w *Writer) Finish(rs state.RunState)` — render final report into `<run_id>.html` + `live/…` + `<run_id>-report.html` + `versions/<run_name>/<run_id>.html`; regenerate all wrapper pages; `AppendLibraryVersion` (once) with the version/report paths + collected deliverables; final `index.html`.

- [ ] **Step 1: Write the test** `internal/hud/artifactwriter/writer_test.go`:

```go
package artifactwriter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/artifact"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/state"
)

func TestWriterLiveThenFinishProducesTree(t *testing.T) {
	sd := t.TempDir()
	lg, _ := logging.New(logging.Config{Level: 0})
	w := New(sd, DefaultConfig(sd), lg)

	live := state.RunState{RunID: "run-1", RunName: "demo", Identity: "id", Done: false,
		StartedAt: "2026-07-10T10:00:00Z", UpdatedAt: "2026-07-10T10:00:05Z",
		Tasks: []state.TaskView{{Key: "alpha", Status: "running", StartedAt: "2026-07-10T10:00:00Z"}}}
	w.Live(live)

	art := artifact.ArtifactsDir(sd)
	for _, rel := range []string{"run-1.html", "live/demo.html", "index.html"} {
		if _, err := os.Stat(filepath.Join(art, rel)); err != nil {
			t.Errorf("Live did not write %s: %v", rel, err)
		}
	}
	if e := artifact.ReadLibrary(sd).Artifacts["demo"]; e.State != "live" {
		t.Errorf("library entry not live: %+v", e)
	}

	done := live
	done.Done = true
	done.UpdatedAt = "2026-07-10T10:00:09Z"
	done.Tasks = []state.TaskView{{Key: "alpha", Status: "passed", StartedAt: "2026-07-10T10:00:00Z", EndedAt: "2026-07-10T10:00:09Z",
		Verified: "ok", CheckTail: "ok\n",
		Deliverables: []state.Deliverable{{TaskKey: "alpha", Name: "notes.md", Path: filepath.Join(art, "deliverables/run-1/alpha/notes.md"), Bytes: 5}}}}
	// stage the deliverable on disk so the wrapper can read it
	_ = os.MkdirAll(filepath.Dir(done.Tasks[0].Deliverables[0].Path), 0o755)
	_ = os.WriteFile(done.Tasks[0].Deliverables[0].Path, []byte("hello"), 0o644)
	w.Finish(done)

	for _, rel := range []string{"run-1-report.html", "versions/demo/run-1.html", "view/run-1/alpha--notes.md.html"} {
		if _, err := os.Stat(filepath.Join(art, rel)); err != nil {
			t.Errorf("Finish did not write %s: %v", rel, err)
		}
	}
	e := artifact.ReadLibrary(sd).Artifacts["demo"]
	if e.State != "pass" || len(e.Versions) != 1 || len(e.Versions[0].Deliverables) != 1 {
		t.Errorf("library version not recorded: %+v", e)
	}
}
```

- [ ] **Step 2: Run, expect failure.** `New`/`DefaultConfig`/`Live`/`Finish` undefined.

- [ ] **Step 3: Implement `internal/hud/artifactwriter/writer.go`.** Key points: render a `templ.Component` to a file via `component.Render(context.Background(), f)`; compute the frozen paths from `Config`; throttle `UpdateLibraryLive` with a stored `lastOutcome`/`lastWrite` (use a monotonic clock — but `time.Now` is fine here, this is not a workflow script); guard `Finish`'s `AppendLibraryVersion` with a `versionRecorded bool`; generate a wrapper page for each text deliverable (`views.WrapperRelPath`, `views.ReadTail`) and for the worker log. Collect `deliverables` for the version record by flattening `rs.Tasks[].Deliverables`.

```go
package artifactwriter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/corruptmemory/ringer/internal/artifact"
	"github.com/corruptmemory/ringer/internal/hud/views"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/state"
)

type Config struct {
	OutTemplate    string // <artifacts>/{run_id}.html
	ReportTemplate string // <artifacts>/{run_id}-report.html
	IndexPath      string // <artifacts>/index.html
}

func DefaultConfig(stateDir string) Config {
	art := artifact.ArtifactsDir(stateDir)
	return Config{
		OutTemplate:    filepath.Join(art, "{run_id}.html"),
		ReportTemplate: filepath.Join(art, "{run_id}-report.html"),
		IndexPath:      filepath.Join(art, "index.html"),
	}
}

type Writer struct {
	stateDir string
	cfg      Config
	lg       logging.Logger
	lastOutcome     string
	lastLibraryAt   time.Time
	versionRecorded bool
}

func New(stateDir string, cfg Config, lg logging.Logger) *Writer {
	return &Writer{stateDir: stateDir, cfg: cfg, lg: lg}
}

func nowISO() string { return time.Now().UTC().Format(time.RFC3339) }

func (w *Writer) outPath(rs state.RunState) string {
	return strings.NewReplacer("{run_id}", rs.RunID, "{run_name}", rs.RunName).Replace(w.cfg.OutTemplate)
}
func (w *Writer) reportPath(rs state.RunState) string {
	return strings.NewReplacer("{run_id}", rs.RunID, "{run_name}", rs.RunName).Replace(w.cfg.ReportTemplate)
}
func (w *Writer) livePath(rs state.RunState) string {
	return filepath.Join(artifact.ArtifactsDir(w.stateDir), "live", artifact.SanitizeName(rs.RunName)+".html")
}
func (w *Writer) versionPath(rs state.RunState) string {
	return filepath.Join(artifact.ArtifactsDir(w.stateDir), "versions", artifact.SanitizeName(rs.RunName), artifact.SanitizeName(rs.RunID)+".html")
}

func (w *Writer) renderFile(path string, c templ.Component) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		w.lg.Warnf("artifact: mkdir %s: %v", path, err)
		return
	}
	f, err := os.Create(path)
	if err != nil {
		w.lg.Warnf("artifact: create %s: %v", path, err)
		return
	}
	defer f.Close()
	if err := c.Render(context.Background(), f); err != nil {
		w.lg.Warnf("artifact: render %s: %v", path, err)
	}
}

func (w *Writer) Live(rs state.RunState) {
	w.renderFile(w.outPath(rs), views.StatusPage(rs, w.stateDir))
	w.renderFile(w.livePath(rs), views.StatusPage(rs, w.stateDir))
	w.writeIndex()
	w.updateLibraryLive(rs)
}

func (w *Writer) Finish(rs state.RunState) {
	report := views.FinalReportPage(rs, w.stateDir)
	w.renderFile(w.outPath(rs), report)
	w.renderFile(w.livePath(rs), report)
	w.renderFile(w.reportPath(rs), report)
	w.renderFile(w.versionPath(rs), report)
	w.writeWrappers(rs)
	w.appendVersion(rs)
	w.writeIndex()
}

func (w *Writer) writeIndex() {
	runs, err := state.ReadAllRunStates(w.stateDir)
	if err != nil {
		w.lg.Warnf("artifact: scan runs for index: %v", err)
		return
	}
	w.renderFile(w.cfg.IndexPath, views.IndexPage(views.BuildIndexRows(runs, w.stateDir)))
}

func (w *Writer) updateLibraryLive(rs state.RunState) {
	outcome := artifact.OutcomeFromState(rs)
	if outcome == w.lastOutcome && time.Since(w.lastLibraryAt) < 5*time.Second {
		return
	}
	w.lastOutcome, w.lastLibraryAt = outcome, time.Now()
	if err := artifact.UpdateLibraryLive(w.stateDir, rs.RunName, rs.RunID, rs.Identity, w.livePath(rs), outcome, nowISO()); err != nil {
		w.lg.Warnf("artifact: library live update: %v", err)
	}
}

func (w *Writer) appendVersion(rs state.RunState) {
	if w.versionRecorded {
		return
	}
	w.versionRecorded = true
	var dels []state.Deliverable
	pass, fail := 0, 0
	for _, t := range rs.Tasks {
		dels = append(dels, t.Deliverables...)
		switch t.Status {
		case "passed":
			pass++
		case "failed", "timeout":
			fail++
		}
	}
	rp := w.reportPath(rs)
	vp := w.versionPath(rs)
	var reportPtr *string
	if rp != vp {
		reportPtr = &rp
	}
	rec := artifact.VersionRecord{
		RunName: rs.RunName, RunID: rs.RunID, Identity: rs.Identity, LivePath: w.livePath(rs),
		VersionPath: vp, ReportPath: reportPtr, Outcome: artifact.OutcomeFromState(rs),
		TasksPass: pass, TasksFail: fail, Deliverables: dels,
	}
	if err := artifact.AppendLibraryVersion(w.stateDir, rec, nowISO()); err != nil {
		w.lg.Warnf("artifact: library version append: %v", err)
	}
}

// writeWrappers generates a text wrapper page for each text deliverable and for
// each task's worker log (ringer.py write_wrapper). Images/other are linked raw
// and get no wrapper.
func (w *Writer) writeWrappers(rs state.RunState) {
	art := artifact.ArtifactsDir(w.stateDir)
	for _, t := range rs.Tasks {
		for _, d := range t.Deliverables {
			if !views.IsTextDeliverable(d.Name) {
				continue
			}
			content, size, trunc := views.ReadTail(d.Path, views.ArtifactWrapperTailBytes)
			meta := d.Name
			if trunc {
				meta = views.TruncationBanner(size)
			}
			wp := filepath.Join(art, views.WrapperRelPath(rs.RunID, t.Key, d.Name))
			w.renderFile(wp, views.FileWrapperPage(views.WrapperData{
				RunName: rs.RunName, TaskKey: t.Key, Title: views.DeliverableTitle(d.Name), MetaLine: meta, Content: content}))
		}
		if t.LogPath != "" {
			content, size, trunc := views.ReadTail(t.LogPath, views.ArtifactWrapperTailBytes)
			meta := "worker log"
			if trunc {
				meta = views.TruncationBanner(size)
			}
			wp := filepath.Join(art, views.WrapperRelPath(rs.RunID, t.Key, "worker.log"))
			w.renderFile(wp, views.FileWrapperPage(views.WrapperData{
				RunName: rs.RunName, TaskKey: t.Key, Title: "Work log", MetaLine: meta, Content: content}))
		}
	}
}
```

Add `views.TruncationBanner(size int64) string` to `artifact_render.go` (the comma-grouped `<b>`-tagged banner from Task 11).

- [ ] **Step 4: Run, expect PASS**

Run: `./build.sh --test 2>&1 | tail -15`
Expected: PASS — the writer produces the full tree and the library entry/version.

- [ ] **Step 5: Commit**

```bash
git add internal/hud/artifactwriter/ internal/hud/views/artifact_render.go
git commit -m "feat(hud): artifactwriter — render pages to disk + keep library.json current"
```

---

## Task 13: Wire the writer into the runner + CLI

Give the runner a nil-safe `ArtifactWriter` hook (called at each flush + at finish), resolve `artifact.enabled` (default true), and construct + inject the writer from `cmd/ringer`. After this, a real `ringer run`/`demo` writes the whole artifact tree and the HUD's library panel comes alive.

**Files:**
- Modify: `internal/config/config.go` (`ArtifactConfig.Enabled` → `*bool`; `ResolveArtifactEnabled()`)
- Modify: `internal/runner/runner.go` (`ArtifactWriter` interface, `Options.Artifact`, flush/finish calls)
- Modify: `cmd/ringer/run.go` (construct + inject)
- Test: `internal/runner/runner_test.go` (writer-hook invoked), `internal/config/config_test.go` (enabled default)

**Interfaces:**
- Produces: `type ArtifactWriter interface { Live(state.RunState); Finish(state.RunState) }` (in package `runner`); `Options.Artifact ArtifactWriter`; `config.ArtifactEnabled(cfg) bool`.

- [ ] **Step 1: Config default.** Change `ArtifactConfig.Enabled` to `*bool` and add:

```go
// ArtifactEnabled resolves the artifact.enabled default: absent -> true
// (Python parity), explicit `enabled = false` -> false.
func (c *AppConfig) ArtifactEnabled() bool {
	return c.Artifact.Enabled == nil || *c.Artifact.Enabled
}
```

Add a `config_test.go` case: empty config → `ArtifactEnabled()` true; `[artifact]\nenabled = false` → false.

- [ ] **Step 2: Runner interface + calls.** In `internal/runner/runner.go`, add near `Options`:

```go
// ArtifactWriter renders the artifact tree from run-state snapshots. Injected
// by the CLI; nil in headless/test runs (artifacts simply not written).
type ArtifactWriter interface {
	Live(state.RunState)
	Finish(state.RunState)
}
```

Add `Artifact ArtifactWriter` to `Options`. In the flush closure `writeState` (after the `state.WriteRunState` call, lines 138-140), add:

```go
		if opts.Artifact != nil {
			opts.Artifact.Live(s)
		}
```

After the final `writeState(true)` (line 181), add:

```go
	if opts.Artifact != nil {
		opts.Artifact.Finish(a.snapshotStamped(startedAt)) // final snapshot with StartedAt/UpdatedAt/Done set
	}
```

Since `writeState` builds the stamped `s` locally, factor the stamping into a small helper or reuse: simplest is to have `writeState` return the stamped snapshot and capture it — change `writeState := func(done bool) state.RunState { ...; return s }` and call `finalSnap := writeState(true)` then `opts.Artifact.Finish(finalSnap)`. Update the ticker goroutine's `writeState(false)` call to ignore the return.

- [ ] **Step 3: CLI wiring.** In `cmd/ringer/run.go`, after the logger is built and before `runner.Run`, construct the writer when enabled:

```go
	var artWriter runner.ArtifactWriter
	if cfg.ArtifactEnabled() {
		artWriter = artifactwriter.New(cfg.StateDirPath(), artifactwriter.DefaultConfig(cfg.StateDirPath()), lg)
	}
```

and add `Artifact: artWriter,` to the `runner.Options{...}` literal. Add the `artifactwriter` import. (Respect any `cfg.Artifact.Out`/`ReportOut`/`IndexOut` overrides by mapping them into `artifactwriter.Config` — default when empty.)

- [ ] **Step 4: Runner hook test.** In `runner_test.go`, add a fake writer and assert both hooks fire:

```go
type fakeArtifact struct{ live, finish int }
func (f *fakeArtifact) Live(state.RunState)   { f.live++ }
func (f *fakeArtifact) Finish(state.RunState) { f.finish++ }

func TestRunnerCallsArtifactWriter(t *testing.T) {
	// ... set up the existing mock manifest + Options, add Artifact: fa ...
	fa := &fakeArtifact{}
	// opts.Artifact = fa
	// run it
	if fa.finish != 1 {
		t.Errorf("Finish should be called exactly once, got %d", fa.finish)
	}
	if fa.live < 1 {
		t.Errorf("Live should be called at least once (final flush), got %d", fa.live)
	}
}
```

- [ ] **Step 5: Build + full suite**

Run: `./build.sh --test 2>&1 | tail -20`
Expected: PASS across config, runner, artifact, views, artifactwriter, hud, cmd.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/runner/runner.go internal/runner/runner_test.go cmd/ringer/run.go
git commit -m "feat(runner,cli): inject artifact writer; artifact.enabled defaults true"
```

---

## Task 14: End-to-end verification + sample config + docs

Prove the whole path with a black-box test that runs a mock manifest through the real `run` path and asserts the produced artifact tree + `library.json`, and that the existing HUD `/hud/library` panel renders the entry. Add the `[artifact]` sample-config documentation.

**Files:**
- Create: `cmd/ringer/artifact_e2e_test.go`
- Modify: `config.sample.toml` (if present) — document `[artifact]`
- Test: the E2E itself

- [ ] **Step 1: Write the E2E** `cmd/ringer/artifact_e2e_test.go`: build a 2-task mock manifest (one pass with `expect_files`, one fail-then-retry-pass), run it via `runManifestFile` (or `runner.Run` with a real `artifactwriter`), pointing `state_dir` at `t.TempDir()`, with `--no-dashboard` so no HUD spawns. Then assert:

```go
	art := filepath.Join(stateDir, "artifacts")
	for _, rel := range []string{"library.json", "index.html"} {
		if _, err := os.Stat(filepath.Join(art, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
	lib := artifact.ReadLibrary(stateDir)
	e, ok := lib.Artifacts[runName]
	if !ok || e.State != "pass" || len(e.Versions) != 1 {
		t.Fatalf("library entry wrong: %+v", e)
	}
	// The version's report + version pages exist on disk.
	if _, err := os.Stat(e.Versions[0].Path); err != nil {
		t.Errorf("version page missing: %v", err)
	}
	// The pass task's deliverable was harvested + wrapped/linked.
	// (assert at least one deliverable recorded on the version)
	if len(e.Versions[0].Deliverables) == 0 {
		t.Error("no deliverables recorded")
	}
```

- [ ] **Step 2: HUD panel smoke.** In the same test (or `internal/hud`), construct `hud.New(stateDir, lg)`, `httptest` GET `/hud/library`, and assert the response HTML contains the run name + `pass` state (the panel reads the same `library.json` the writer wrote). This closes the loop: writer → library.json → HUD library panel.

- [ ] **Step 3: Sample config.** If `config.sample.toml` exists, add:

```toml
# [artifact] — self-refreshing HTML results pages under <state_dir>/artifacts/.
# enabled defaults to true; set false to run headless with no artifact pages.
[artifact]
enabled = true
# out       = "~/.ringer/artifacts/{run_id}.html"
# report_out = "~/.ringer/artifacts/{run_id}-report.html"
# index_out  = "~/.ringer/artifacts/index.html"
```

- [ ] **Step 4: Run the full suite + a manual smoke**

Run: `./build.sh --test 2>&1 | tail -20`
Then manual: `./ringer demo` and open `~/.ringer/artifacts/index.html` + the newest `<run_id>-report.html` in a browser — confirm the report shows the 3 tasks, deliverables, and reads like the Python page.
Expected: PASS + a good-looking report.

- [ ] **Step 5: Commit**

```bash
git add cmd/ringer/artifact_e2e_test.go config.sample.toml
git commit -m "test(artifact): E2E — run produces artifact tree + library; HUD panel renders it"
```

---

## Self-Review (completed by plan author)

**Spec coverage (§8 artifacts / §9 frozen contracts):**
- live/`<run_name>`.html, `<run_id>`.html, `<run_id>`-report.html, versions, index.html, library.json — Tasks 9/10/12. ✓
- `library.json` schema frozen (types already exist; write path Tasks 2/4). ✓
- Deliverables harvest + dead-run reconcile (reconcile already shipped Plan 4; harvest Task 3). ✓
- Run-card/task-row **shared with the dashboard** — **refined**: the artifact pages reuse the shared *derivation helpers* (`render.go`/`text.go`), the *worker-row vocabulary* + CSS classes, and `TaskElapsed`/`TaskKind`/`TaskStateText`, but have their **own** page scaffolding (`section.work`/`work-group`/`work-group-body` with deliverable list, proof drawer, task links) because the Python artifact pages genuinely diverge from the dashboard's `section.run`/`workers` structure. This is faithful to the spec's *intent* (unify on templ, share what shares) while matching the Python contract; flagged here for the reviewer. ✓ (with noted refinement)
- CSP + per-page inlined CSS (Task 6). ✓
- Text file-wrapper 256 KiB tail (Task 11). ✓

**Placeholder scan:** The intentional "port exact wording from ringer.py:NNNN, golden locks it" notes in Tasks 7/8/9/10/11 are **not** placeholders — they name the authoritative source function and the golden that pins the output. The `DeliverableTitle`/`ImageDataURI`/`BriefingLive` bodies marked for porting cite exact line ranges. No `TBD`/`handle edge cases`/`write tests for the above`.

**Type consistency:** `state.Deliverable` (Task 2) is used identically in `artifact` (alias, Task 2), harvest (Task 3), library version (Task 4), TaskView snapshot (Task 5), and the writer (Task 12). `TaskElapsed(t, nowISO)` signature change (Task 1) is consumed by `taskRow` (Task 1) and `workGroup` (Task 8) — both pass `rs.UpdatedAt`. `VersionRecord` (Task 4) is built only in the writer (Task 12). `ArtifactWriter` interface (Task 13) is implemented by `artifactwriter.Writer` (Task 12) — method set matches (`Live`/`Finish`, both take `state.RunState`).

**Known carry-overs surfaced (not warts):** `SanitizeName` empty-fallback `"unnamed"` vs Python `"artifact"` (never-hit; Go-authoritative); the upstream `--running`/`status_color` CSS quirk (index sets chip color inline anyway). Both documented in Global Constraints / Task 10.
