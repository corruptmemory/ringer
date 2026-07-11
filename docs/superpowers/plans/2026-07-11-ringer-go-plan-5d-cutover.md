# Ringer Go Plan 5d — Hard Cutover Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the Python/Rust → Go cutover: delete `ringer.py`, the Tauri HUD, the Python periphery (dashboard HTML, backfill script, nudge hook, Python tests, mock-worker), and the Tauri release workflow; enforce the `allow_full_access` gate the config has promised but never enforced; and sweep README + SKILL.md so they document the Go binary. After this, the repo is Go-only.

**Architecture:** A before/after metrics scorecard (captured first, while both codebases coexist), then pure subtraction plus one small additive security gate. The Go binary is already functionally independent of every deletion target — the survey (below) confirmed the only Go references to deleted files are *provenance comments* (`// ports ringer.py:4501`), not imports or runtime dependencies, so deletion cannot break the build. The one behavioral addition is a fail-closed `allow_full_access` gate in the run path. README/SKILL edits are mechanical (`./ringer.py X` → `ringer X`; drop Python/Rust requirements).

**Tech Stack:** Go 1.26, module `github.com/corruptmemory/ringer`, `CGO_ENABLED=0`. Build/test ONLY via `./build.sh` and `./build.sh --test [--race]`. Git deletions via `git rm -r`.

---

## STATUS — EXECUTED & REVIEWED (2026-07-11, branch `go-5d`, commits `7c2d18d..df01476`)

Executed via superpowers subagent-driven-development. `./build.sh --test --race` GREEN all 21 packages. **Opus whole-branch review = READY TO MERGE** (0 Critical, 0 Important; 1 defer-able Minor: a "Python parity" rationale comment in `config.sample.toml`). Per-task reviews all Approved. The repo is now **Go-only**: opus confirmed no tracked `.rs`/`.swift`/`Cargo.*`/`tauri` files remain, the only `.py` are the kept `templates/*/checks/*.py` product scripts, CI is Go-only, and spec §11 is complete.

- **Rewrite metrics (Task 1, `docs/go-rewrite-metrics.md`):** production "money" code 11,354 → 7,977 (**−29.7%**: Python 6,488 + Rust/Tauri 852 + dashboard HTML 4,014 → Go 7,369 + templ 608); tests 4,360 → 6,400 (**+46.8%**); test:production ratio 0.38 → 0.80 (**~2.1×**); Go coverage mean **76.7%** (21 pkgs); `ringer.py`'s 8,300-line monolith → 22 focused packages (largest 590). Numbers cloc-measured before deletion (both codebases coexisting) and independently reproduced in review.
- **`allow_full_access` gate (Task 2):** `checkFullAccessGate` in `cmd/ringer/run.go` — fail-closed, refuses a `full_access` task unless the operator opted in; placed before the dry-run branch so it refuses on both paths. The config had documented this gate since Plan 2 but never enforced it.
- **Deletion (Task 3):** `ringer.py` + `hud/` (Tauri) + `dashboard/` + `scripts/` + `hooks/` + `tests/` + `engines/mock_worker.py` + `.github/workflows/release.yml` = 90 files / 24,219 lines. Kept: `engines/opencode-sandboxed.sh`, `templates/**` (incl. `.py` check scripts), `registry/`, `docs/`, `ci.yml`, all Go provenance comments.
- **README sweep (Task 4):** 14 `ringer.py`→`ringer`, dropped Python/Rust requirements + the Tauri paragraph, added two migration notes (`$RINGER_HOME`/`state_dir`; legacy-hook cleanup). BEYOND-BRIEF (reviewer-verified accurate vs source): also corrected stale JSONL/Postgres eval-store prose to the SQLite reality.
- **PROVENANCE.md (Task 4.5, added mid-execution per Jim's direction):** credits original author **Jonathan Edwards** and copyright holder **Nate Jones Media LLC**, carries the PolyForm Shield Required Notice verbatim, frames this repo as a Go rewrite preserving their product design; the two stale pre-rewrite working notes were removed (full text in git history, credit distilled into PROVENANCE.md). Every attribution claim was corroborated against `git log` + `LICENSE.md` in review.
- **SKILL.md sweep (Task 5):** retargeted every invocation to the `ringer` binary + re-`cp`'d to `internal/agent/SKILL.md` (drift-lock `cmp`-identical, `TestEmbeddedSkillMatchesCanonical` green).
- **Straggler sweep (`df01476`):** the plan's own final-verification grep caught two user-facing docs Task 4/5 scope missed — `docs/MODEL-NOTES.md` (embedded judgment notes) + `templates/README.md` — swept to the Go binary (no test asserts on MODEL-NOTES content, so the embed change was safe).

---

## Global Constraints

- **This is one PR, one coherent cutover** (spec §11). After it lands the repo has no Python/Rust implementation, only Go + language-agnostic product content.
- **DELETE (git rm -r):**
  - `ringer.py` (the 298 KB Python implementation)
  - `hud/` (entire Rust/Tauri desktop prototype, 62 files)
  - `dashboard/` (`dashboard.html`, `ringside.html` — Python-era SPAs; the Go HUD is templ-rendered)
  - `scripts/` (`backfill_model_log.py`; `db import` replaced it)
  - `hooks/` (`ringer_nudge.py`; `ringer nudge-hook` replaced it in 5c)
  - `tests/` (all 22 files are Python stdlib-unittest against the deleted implementation; the 21 Go packages carry the Go test suite)
  - `engines/mock_worker.py` (the `ringer mock-worker` subcommand replaced it; `[engines.mock]` points at the ringer binary)
  - `.github/workflows/release.yml` (the Python/Tauri release workflow)
- **KEEP — do NOT delete or edit (except where a task says so):**
  - `engines/opencode-sandboxed.sh` (spec §11 "Keep"; the macOS Seatbelt lane, referenced by config path and by an `internal/isolate/select.go` error message)
  - `templates/**` — including the ~28 `templates/*/checks/*.py` scripts. **These are product content: check commands that manifests invoke via `sh -c`, language-agnostic, NOT the Python implementation.** Deleting them would break the templates.
  - `registry/`, `docs/`, `.github/workflows/ci.yml` (the Go CI), `config.sample.toml`
  - **ALL Go code, including provenance comments** that cite `ringer.py:NNNN` / `engines/mock_worker.py` / `scripts/backfill_model_log.py`. They document port fidelity and point into git history; scrubbing dozens of them is churn with no benefit and loses provenance. Leave them.
- **`./build.sh --test --race` must stay green after every task.** The Go binary does not depend on any deletion target (survey-verified), so this holds by construction.
- **SKILL.md drift-lock coupling (critical):** `internal/agent/SKILL.md` is a committed copy of `.claude/skills/ringer/SKILL.md`, locked by `TestEmbeddedSkillMatchesCanonical`. **Any edit to the canonical `.claude/skills/ringer/SKILL.md` REQUIRES re-running `cp .claude/skills/ringer/SKILL.md internal/agent/SKILL.md` in the same task**, or that test fails.
- **Doc voice:** README and SKILL.md describe the Go binary. `./ringer.py <x>` → `ringer <x>`; `python3 …` invocations of ringer → `ringer …`. Drop the Python 3.11+ and Rust-toolchain requirements.

## Survey findings (why this is safe — from the 2026-07-11 cutover survey)

- No `*.go` file (test or non-test) has a functional dependency on a deletion target; every match is a provenance comment or the `commandInvokesRinger` test string `"python3 ringer.py run"` (a test input, correct, stays).
- `build.sh` has no Python/Rust/dashboard steps (only `internal/hud/static/vendor` = the Go hud). `ci.yml` runs no Python. `release.yml` is the Tauri release workflow.
- README `ringer.py` references: 14. Plus a Tauri paragraph (line ~212), "needs Python 3.11+" (line ~32), and a `## Requirements` block (lines ~302-306) listing Python 3.11+ / Rust toolchain.
- `allow_full_access`: task-level `FullAccess` routes a task around the jail (`selectIsolator`, `runner.go:262`), but nothing reads `cfg.AllowFullAccess` to *refuse* a full_access task when the gate is off — the config doc string ("a task with full_access=true still fails unless this is true") is currently unenforced.

---

## Task 1: Rewrite metrics document (before/after scorecard)

Capture a `cloc`-based before/after comparison of the retiring Python/Rust/dashboard implementation vs the new Go implementation — production ("money") vs test code, coverage, and structural consolidation — into a committed doc. **This MUST run before Task 3's deletion**, while both codebases still coexist in the working tree.

**Files:**
- Create: `docs/go-rewrite-metrics.md`

**Tooling:** `cloc` 2.08 (installed at `/usr/bin/cloc`; strips comments/blanks, detects languages). Go coverage via a one-off `go test -cover ./...` — a read-only measurement for this doc, NOT the dev build/test loop (the `./build.sh`-only rule governs the build workflow; note this exception in the doc's methodology section).

**Scope decisions (frozen — categorization is the crux):**
- **BEFORE / implementation being retired:** Python production = `ringer.py`, `engines/mock_worker.py`, `hooks/ringer_nudge.py`, `scripts/backfill_model_log.py`; Python tests = `tests/*.py`; Rust/Tauri prototype = `hud/` excluding `Cargo.lock`; dashboard SPAs = `dashboard/*.html`.
- **AFTER / Go implementation:** Go production = `*.go` excluding `*_test.go` AND `*_templ.go`; Go tests = `*_test.go`; templ source = `*.templ`.
- **Excluded from BOTH** (shared/kept product content, not implementation): `templates/**` (incl. its `.py` check scripts), `registry/`, `docs/`.
- **Excluded as generated/vendored:** `*_templ.go` (generated from `.templ`), `internal/hud/static/vendor/**` (vendored htmx/idiomorph), `Cargo.lock`.

**Reference numbers** (from the 2026-07-11 pre-deletion measurement — the implementer re-runs the commands in Step 1 and must reproduce these within a few lines; flag any material drift rather than silently reporting different figures):

| Group | files | code (cloc) |
|---|---|---|
| BEFORE Python production | 4 | 6,488 |
| BEFORE Python tests | 21 | 4,360 |
| BEFORE Rust/Tauri hud (ex `Cargo.lock`) | 11 | 852 (Rust 623 / JS 107 / JSON 64 / TOML 21 / other 37) |
| BEFORE dashboard HTML | 2 | 4,014 |
| AFTER Go production (ex `*_test.go` & generated `*_templ.go`) | 78 | 7,369 |
| AFTER Go tests | 71 | 6,400 |
| AFTER templ source (HTML-forced) | 8 | 608 |
| (excluded) generated `*_templ.go` | — | 2,735 |

Derived headline figures: production "money" 11,354 → 7,977 (**−29.7%**); tests 4,360 → 6,400 (**+46.8%**); test:production ratio 0.38 → 0.80 (**~2.1×**); total (money+test) 15,714 → 14,377 (**−8.5%**). Go coverage mean **76.7%** across 21 packages (range 55.7%–96.0%). Consolidation: `ringer.py` was 8,300 lines in one file → **22 Go packages**, largest file 590 lines; 4 primary languages (Python/Rust/JS/HTML) → 1 (Go) + templ; Python runtime + Rust toolchain + Tauri → **one static binary**.

- [ ] **Step 1: Run the measurement command set and capture outputs**

```bash
# BEFORE
cloc --quiet ringer.py engines/mock_worker.py hooks/ringer_nudge.py scripts/backfill_model_log.py
cloc --quiet tests/
cloc --quiet --not-match-f='Cargo\.lock' hud/
cloc --quiet dashboard/
# AFTER
cloc --quiet $(git ls-files '*.go' | grep -vE '_(test|templ)\.go$')
cloc --quiet $(git ls-files '*_test.go')
cloc --quiet --force-lang=HTML,templ $(git ls-files '*.templ')
# structure + coverage
git ls-files '*.go' | grep -vE '_test\.go$' | xargs -n1 dirname | sort -u | wc -l   # Go package count
go test -cover ./...                                                                 # per-package coverage
```

- [ ] **Step 2: Write `docs/go-rewrite-metrics.md`** with these sections:
  1. **Title + one-paragraph intro** — what's being compared and why (the Python/Rust→Go cutover), and the honest caveat that LOC is a rough proxy.
  2. **Production ("money") code — before vs after** — a table (Python / Rust-Tauri / dashboard vs Go / templ), totals, and the −29.7% headline.
  3. **Test code — before vs after** — table, +46.8%, and the test:production ratio 0.38 → 0.80.
  4. **Coverage** — the Go `go test -cover` per-package numbers + mean 76.7%; note the legacy Python suite's runtime coverage % was not measured (retired code), so its test:code ratio is the investment proxy.
  5. **Structural consolidation** — 4 languages → 1 (+templ); 8,300-line monolith → 22 packages (largest 590); Python runtime + Rust toolchain + Tauri → single static binary.
  6. **Methodology / reproducibility** — the exact commands from Step 1, the categorization (what counts as production vs test, what's excluded and why: generated `*_templ.go`, vendored JS, `templates/**` shared content), and the `go test -cover` note.
- [ ] **Step 3: Verify the doc's numbers reproduce** the Step 1 command outputs (spot-check totals against the reference table; the derived percentages must follow from the reported figures).
- [ ] **Step 4: Commit**

```bash
git add docs/go-rewrite-metrics.md
git commit -m "docs: add before/after rewrite metrics (Python/Rust -> Go cloc scorecard)"
```

---

## Task 2: Enforce the `allow_full_access` gate

Add the fail-closed gate the config has always promised: a task requesting `full_access` is refused unless the operator set `allow_full_access = true`. Extracted as a pure helper for unit testing, called in the run path before any task starts (covering both dry-run and real runs, so the refusal is fail-fast).

**Files:**
- Modify: `cmd/ringer/run.go` (add helper + one call site)
- Test: `cmd/ringer/run_test.go` (add a table test)

**Interfaces:**
- Consumes: `manifest.Manifest` (`.Tasks[].FullAccess`, `.Tasks[].Key`), `config.AppConfig.AllowFullAccess`.
- Produces: `checkFullAccessGate(m *manifest.Manifest, allowFullAccess bool) error`.

- [ ] **Step 1: Write the failing test** in `cmd/ringer/run_test.go` (mirrors the existing `TestSelectIsolator` construction idiom)

```go
func TestCheckFullAccessGate(t *testing.T) {
	full := []manifest.Task{{Key: "a", Engine: "codex", FullAccess: true}}
	safe := []manifest.Task{{Key: "b", Engine: "codex"}}
	cases := []struct {
		name    string
		tasks   []manifest.Task
		allow   bool
		wantErr bool
	}{
		{"full-access refused when gate off", full, false, true},
		{"full-access allowed when gate on", full, true, false},
		{"no full-access, gate off", safe, false, false},
		{"no full-access, gate on", safe, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &manifest.Manifest{Workdir: t.TempDir(), Tasks: tc.tasks}
			err := checkFullAccessGate(m, tc.allow)
			if (err != nil) != tc.wantErr {
				t.Fatalf("checkFullAccessGate = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
```

- [ ] **Step 2: Run it to confirm it fails** — Run: `./build.sh --test` — Expected: FAIL (`checkFullAccessGate` undefined).

- [ ] **Step 3: Add the helper** to `cmd/ringer/run.go` (place it near `selectIsolator`)

```go
// checkFullAccessGate refuses a run when any task requests full_access but the
// operator has not opted in via [config] allow_full_access. Fail-closed: the
// isolation layer already routes full_access tasks around the jail, so this
// makes the config doc's promised gate ("a task with full_access=true still
// fails unless this is true") real (spec §6). Applied before the dry-run
// branch so a forbidden manifest is refused whether you dry-run or run.
func checkFullAccessGate(m *manifest.Manifest, allowFullAccess bool) error {
	if allowFullAccess {
		return nil
	}
	for _, t := range m.Tasks {
		if t.FullAccess {
			return fmt.Errorf("task %q requests full_access but allow_full_access is not enabled in config; refusing (set allow_full_access = true to opt in)", t.Key)
		}
	}
	return nil
}
```

- [ ] **Step 4: Wire the call site** in `runManifestFile`, immediately after the `engine.Preflight(engines, used)` block and before the `if dryRun {` branch (currently run.go:124-126):

```go
	if err := engine.Preflight(engines, used); err != nil {
		return err
	}

	if err := checkFullAccessGate(m, cfg.AllowFullAccess); err != nil {
		return err
	}

	if dryRun {
```

- [ ] **Step 5: Run tests** — Run: `./build.sh --test` — Expected: PASS (the new test + full suite green).

- [ ] **Step 6: Commit**

```bash
git add cmd/ringer/run.go cmd/ringer/run_test.go
git commit -m "run: enforce allow_full_access gate (refuse full_access tasks unless opted in)"
```

---

## Task 3: Delete the Python/Rust implementation and periphery

Pure subtraction. Remove every deletion-list path; keep `engines/opencode-sandboxed.sh`. The Go build is unaffected (survey-verified).

**Files:**
- Delete: `ringer.py`, `hud/`, `dashboard/`, `scripts/`, `hooks/`, `tests/`, `engines/mock_worker.py`, `.github/workflows/release.yml`

- [ ] **Step 1: Remove the deletion targets**

```bash
git rm -r ringer.py hud dashboard scripts hooks tests engines/mock_worker.py .github/workflows/release.yml
```

- [ ] **Step 2: Confirm the survivors in `engines/` and `.github/`**

```bash
ls engines/            # expect ONLY: opencode-sandboxed.sh
ls .github/workflows/  # expect ONLY: ci.yml
```
Expected: `engines/opencode-sandboxed.sh` present; `hud/`, `dashboard/`, `scripts/`, `hooks/`, `tests/` gone.

- [ ] **Step 3: Verify the Go build + full suite still pass** — Run: `./build.sh --test --race` — Expected: PASS, all 21 packages (nothing depended on the deleted files).

- [ ] **Step 4: Smoke the binary end-to-end** (proves the deletions didn't sever a runtime path)

```bash
./build.sh
./ringer --help | grep -E 'run|demo|hud|models|catalog|nudge-hook|install-agent'   # subcommands intact
./ringer demo --dry-run                                                            # mock engine (Go), no ringer.py needed
```
Expected: help lists the Go subcommands; `demo --dry-run` prints a plan without error.

- [ ] **Step 5: Commit**

```bash
git commit -m "cutover: delete ringer.py, Tauri hud/, dashboard/, scripts/, hooks/, Python tests/, mock_worker.py, release.yml"
```

---

## Task 4: Sweep the README to describe the Go binary

Mechanical doc edit. The README currently documents the Python tool; make it document the single Go binary, and fold in the two 5c carry-forward migration notes.

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Read the whole README** so every edit is in context: `README.md`.

- [ ] **Step 2: Replace every ringer.py invocation** — `./ringer.py <args>` → `ringer <args>` and any `python3 ringer.py <args>` → `ringer <args>` (14 occurrences). Do NOT touch the `engines/opencode-sandboxed.sh` mentions (that file stays) or the `scripts/backfill_model_log.py` history references if any remain in prose about provenance — but the user-facing command examples must be the `ringer` binary.

- [ ] **Step 3: Fix the requirements/build prose:**
  - Line ~32 ("Ringer runs on macOS and Linux (Windows via WSL) and needs Python 3.11+."): drop the Python requirement — it's a single static binary now. Suggested: "Ringer runs on macOS and Linux (Windows via WSL) and ships as a single static binary — no runtime dependencies."
  - Line ~212 (the "A native desktop build (Tauri, under `hud/`) …" paragraph): delete it — the Tauri prototype is gone.
  - The `## Requirements` block (lines ~302-306, "Python 3.11+ …", "Rust toolchain …"): replace with the Go story — to *run*, nothing (static binary); to *build from source*, Go 1.26 and `./build.sh`. Keep the macOS/Linux/WSL platform line.

- [ ] **Step 4: Add the two migration notes** near the install-agent / OpenCode sections (a short "Migrating from the Python ringer" note):
  - If you keep a non-default `state_dir` in config, set `$RINGER_HOME` to the same path, or the nudge hook's live-run suppression won't see your active runs (harmless — you'll just get an occasional advisory nudge during a live run).
  - `ringer uninstall-agent` removes hooks it recognizes by the `nudge-hook` marker; it does not remove a legacy `python3 …/ringer_nudge.py` hook left by the old Python `install-agent`. Run the old `uninstall-agent` first, or delete that hook from `settings.json` by hand.

- [ ] **Step 5: Verify no stale references remain**

```bash
grep -nE 'ringer\.py' README.md            # expect: no matches
grep -niE 'python 3\.11|rust toolchain|tauri' README.md   # expect: no matches
```
Expected: both empty.

- [ ] **Step 6: Commit**

```bash
git add README.md
git commit -m "docs: sweep README to the Go binary (ringer.py -> ringer; drop Python/Rust reqs; migration notes)"
```

---

## Task 5: Sweep SKILL.md to the Go binary + re-sync the drift-locked copy

The operational skill (`.claude/skills/ringer/SKILL.md`) still tells the agent to invoke `ringer.py`. Sweep it to the Go binary, then re-copy it to the embedded copy so `TestEmbeddedSkillMatchesCanonical` re-passes.

**Files:**
- Modify: `.claude/skills/ringer/SKILL.md`
- Regenerate: `internal/agent/SKILL.md` (via `cp`)

- [ ] **Step 1: Read the whole skill** for context: `.claude/skills/ringer/SKILL.md`.

- [ ] **Step 2: Sweep Python-era references to the Go binary:**
  - `./ringer.py <args>` / `python3 ringer.py <args>` → `ringer <args>` everywhere.
  - Any reference to `hooks/ringer_nudge.py`, `engines/mock_worker.py`, `scripts/backfill_model_log.py`, `dashboard/ringside.html`, or the Tauri `hud/` as *the tool to run* → the Go equivalent (`ringer nudge-hook`, `ringer mock-worker`, `ringer db import`, the built-in Ringside HUD). Do not invent new operational claims — only retarget the invocation surface the skill already documents.
  - Keep `engines/opencode-sandboxed.sh` references (it stays).

- [ ] **Step 3: Re-sync the embedded copy** (REQUIRED — the drift-lock test compares them):

```bash
cp .claude/skills/ringer/SKILL.md internal/agent/SKILL.md
```

- [ ] **Step 4: Verify the sweep + drift-lock**

```bash
grep -nE 'ringer\.py' .claude/skills/ringer/SKILL.md   # expect: no matches
./build.sh --test                                       # TestEmbeddedSkillMatchesCanonical must pass
```
Expected: no `ringer.py` in the skill; full suite green (embed matches canonical again).

- [ ] **Step 5: Commit**

```bash
git add .claude/skills/ringer/SKILL.md internal/agent/SKILL.md
git commit -m "docs: sweep SKILL.md to the Go binary + re-sync embedded copy (drift-lock)"
```

---

## Final verification (whole-plan)

- [ ] `./build.sh --test --race` — green across all 21 packages.
- [ ] **No dangling functional reference to any deleted file** (provenance comments in `.go` are the only allowed survivors):

```bash
# Deleted basenames referenced anywhere OUTSIDE Go provenance comments / the nudge test string:
grep -rnE 'ringer\.py|mock_worker\.py|ringer_nudge\.py|backfill_model_log\.py|dashboard\.html|ringside\.html' \
  --include='*.md' --include='*.toml' --include='*.sh' --include='*.yml' --include='*.yaml' . \
  | grep -v 'docs/superpowers/'          # plan/spec docs legitimately discuss the cutover
# Expect: no user-facing README/SKILL/config/CI references to the deleted files.
```

- [ ] `git status` clean; the branch is one coherent cutover (metrics doc + gate + deletions/sweeps).
- [ ] Binary smoke: `./ringer demo --dry-run` runs; `HOME=$(mktemp -d) ./ringer install-agent` writes the swept skill; a full_access manifest with `allow_full_access` unset is refused by `ringer run` / `ringer run --dry-run`.

## Self-Review (author checklist)

- **Spec §11 coverage:** before/after metrics scorecard (Task 1, prepended per Jim's request); delete ringer.py/hud/dashboard/scripts/hooks/tests/mock_worker.py/release.yml (Task 3); README + SKILL sweep (Tasks 4-5); `config.sample.toml` Python-era keys were already removed in 5b (gen-config regeneration) — nothing to do here; `allow_full_access` gating (Task 2); `.claude/skills/ringer` embed re-synced (Task 5). Covered.
- **Keep-list honored:** `engines/opencode-sandboxed.sh`, `templates/**` (incl. `.py` checks), `registry/`, `docs/`, `ci.yml`, Go provenance comments — none deleted.
- **Drift-lock:** Task 4 re-copies the embedded SKILL.md in the same task that edits the canonical one.
- **Type consistency:** `checkFullAccessGate(*manifest.Manifest, bool) error` defined and called in Task 2; test mirrors `TestSelectIsolator`.
- **No placeholders:** every code/command step is concrete.
