# Go Rewrite Metrics: Before/After Scorecard

This document captures a `cloc`-based before/after comparison of the retiring
Python/Rust/dashboard implementation of Ringer against the new Go
implementation, measured at the pre-deletion commit where both codebases
still coexist in the working tree (this measurement must run before the old
code is deleted — see [Plan 5d, Task 3](superpowers/plans/2026-07-11-ringer-go-plan-5d-cutover.md)).
"Before" is the Python orchestrator (`ringer.py` + its companion scripts),
its `pytest` suite, the Rust/Tauri HUD prototype (`hud/`), and the two
static dashboard SPAs (`dashboard/*.html`). "After" is the Go rewrite:
production `.go` sources, `_test.go` files, and `.templ` template sources.

**Honest caveat:** lines of code is a rough proxy, not a quality metric. A
smaller number isn't automatically "better," and a bigger test suite isn't
automatically "more correct." What LOC and file-count deltas *do* tell us
reliably here: how much surface area changed hands in the rewrite, how the
test:production investment ratio shifted, and how much the implementation
consolidated (languages, toolchains, deployment artifacts). Treat every
number below as directional evidence, not proof.

## 1. Production ("money") code — before vs after

"Money" code is the code that ships and does the work — orchestration,
HUD/dashboard rendering, worker engines — excluding tests and excluding
generated/vendored code (see Methodology, section 6).

| Component | Language(s) | Files | Code (cloc) |
|---|---|---:|---:|
| **Before** | | | |
| `ringer.py`, `engines/mock_worker.py`, `hooks/ringer_nudge.py`, `scripts/backfill_model_log.py` | Python | 4 | 6,488 |
| `hud/` (ex `Cargo.lock`) | Rust 623 / JS 107 / JSON 64 / TOML 21 / other 37 | 11 | 852 |
| `dashboard/*.html` | HTML | 2 | 4,014 |
| **Before total** | | **17** | **11,354** |
| **After** | | | |
| `*.go` (ex `_test.go`, ex generated `_templ.go`) | Go | 78 | 7,369 |
| `*.templ` | templ (HTML-forced) | 8 | 608 |
| **After total** | | **86** | **7,977** |

**Production code shrank 11,354 → 7,977 lines: −29.7%.**

## 2. Test code — before vs after

| Component | Language | Files | Code (cloc) |
|---|---|---:|---:|
| `tests/*.py` (before) | Python | 21 | 4,360 |
| `*_test.go` (after) | Go | 71 | 6,400 |

**Test code grew 4,360 → 6,400 lines: +46.8%.**

Combined with the production shrink, the test:production ratio nearly
doubled:

| | Production | Tests | Test : production ratio |
|---|---:|---:|---:|
| Before (Python/Rust/dashboard) | 11,354 | 4,360 | 0.38 |
| After (Go/templ) | 7,977 | 6,400 | 0.80 |

That's roughly a **2.1×** increase in relative test investment per line of
shipped code.

**Total code (production + tests):** 15,714 (before) → 14,377 (after), a
**−8.5%** reduction — smaller overall footprint even after test code grew
substantially as a share of the total.

## 3. Coverage

The legacy Python suite's *runtime* coverage percentage was never measured
(the code is retired, not instrumented retroactively — there is no
authoritative before-figure to compare against). What we do have for the
before-side is the test:production LOC ratio above (0.38), which stands in
as a rough investment proxy. The Go rewrite has a real, tool-measured
number, from a one-off `go test -cover ./...` run (this is a read-only
metrics measurement, not the project's dev build/test loop — see
Methodology):

| Package | Coverage |
|---|---:|
| `cmd/ringer` | 59.3% |
| `internal/agent` | 80.9% |
| `internal/artifact` | 82.8% |
| `internal/catalog` | 62.3% |
| `internal/config` | 79.5% |
| `internal/engine` | 93.0% |
| `internal/hud` | 65.0% |
| `internal/hud/artifactwriter` | 75.6% |
| `internal/hud/views` | 56.4% |
| `internal/isolate` | 69.9% |
| `internal/jail` | 55.7% |
| `internal/lint` | 79.3% |
| `internal/logging` | 93.1% |
| `internal/manifest` | 80.0% |
| `internal/mockworker` | 86.6% |
| `internal/nudge` | 80.0% |
| `internal/runner` | 88.2% |
| `internal/scoreboard` | 84.1% |
| `internal/state` | 79.1% |
| `internal/store` | 64.2% |
| `internal/verify` | 96.0% |

**Mean across 21 tested packages: 76.7%** (range 55.7%–96.0%). The root
package (`github.com/corruptmemory/ringer`) has no test files and is
excluded from the mean — it's a thin `package main` entry shim, not
represented as its own coverage figure.

## 4. Structural consolidation

- **Languages:** 4 primary languages before (Python, Rust, JavaScript,
  HTML) → 1 (Go) plus templ's HTML-derived template language after.
- **File decomposition:** `ringer.py` was a single 8,300-line file (`wc -l`)
  → the Go rewrite spans **22 packages**, with the largest single file
  (`internal/hud/views/artifact_render.go`) at 590 lines — roughly 14x
  smaller than the old monolith's peak, and spread across a real package
  boundary structure instead of one file.
- **Runtime/toolchain footprint:** Python interpreter + pip dependencies,
  Rust toolchain + Cargo, and Tauri's native/webview bridge → **one static
  Go binary**, embedding all HTML/CSS/JS assets, with no external runtime
  dependency at deploy time.

## 5. Methodology / reproducibility

All figures above come from `cloc` 2.08 (`/usr/bin/cloc`, strips blank
lines and comments before counting) and `go test -cover`, run from the
repository root at the pre-deletion commit where both the retiring
Python/Rust/dashboard code and the new Go code coexist in the working tree.

**Exact commands:**

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

**Categorization (frozen for this measurement):**

- **Before / retiring:** Python production = `ringer.py`,
  `engines/mock_worker.py`, `hooks/ringer_nudge.py`,
  `scripts/backfill_model_log.py`; Python tests = `tests/*.py`; Rust/Tauri
  prototype = `hud/` excluding `Cargo.lock` (a generated lockfile, not
  authored code); dashboard SPAs = `dashboard/*.html`.
- **After / Go rewrite:** Go production = `*.go` excluding `*_test.go` and
  excluding generated `*_templ.go`; Go tests = `*_test.go`; templ source =
  `*.templ` (counted with `--force-lang=HTML,templ` since `.templ` isn't a
  `cloc`-native language).
- **Excluded from both sides** (shared/kept product content, not
  implementation being compared): `templates/**` (including its `.py`
  check scripts — these are product content/config, not orchestrator code),
  `registry/`, `docs/`.
- **Excluded as generated/vendored** (not authored, would inflate either
  side without reflecting engineering effort): generated `*_templ.go`
  (mechanically produced from `.templ` by `templ generate` — measuring it
  would double-count the `.templ` source), `internal/hud/static/vendor/**`
  (vendored third-party htmx/idiomorph JS), `Cargo.lock` (a generated
  lockfile).

**`go test -cover` note:** this project's standing rule is that dev
build/test iteration goes through `./build.sh` only. The `go test -cover
./...` invocation here is an exception, explicitly scoped to this one-off,
read-only coverage measurement for this document — it does not run
`templ generate` or produce build artifacts, and is not part of the
project's normal build/test loop.

**Reproducibility note on the excluded generated-code figure:** the Step 1
command set above does not include a command for measuring the generated
`*_templ.go` line count (it's explicitly out of scope for the production
and test tables), but for completeness this document's authoring process
also ran `cloc --quiet $(git ls-files '*_templ.go')` and got **2,541** lines
across 8 files. This diverges from an earlier figure of 2,735 attached to
the original planning notes for this task; the two other reproducible
figures at this same commit — production Go (7,369) and non-test Go total
(7,369 + 2,541 = 9,910, confirmed by running `cloc` on the undivided
non-test `*.go` file set) — are internally consistent with 2,541, so 2,541
is treated as the correct, reproducible figure for this commit. Because
this row is excluded from every headline computation in sections 1–4, the
discrepancy doesn't affect any reported percentage or ratio in this
document.
