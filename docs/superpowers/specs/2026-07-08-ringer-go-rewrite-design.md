# Ringer Go Rewrite — Design

**Date:** 2026-07-08
**Status:** Approved (design review with Jim, this date)
**Approach:** Hard cutover — one Go binary replaces all Python and Rust tooling in this repo; idiomatic internal re-architecture behind frozen external contracts ("Approach B").

## 1. Context and goals

Ringer today is a single 8300-line Python 3.11+ file (`ringer.py`, stdlib-only, optional `psycopg`), a Tauri/Rust desktop HUD prototype (`hud/`), and Python/shell periphery (mock worker engine, Claude Code nudge hook, backfill script, 21 stdlib-unittest files). The product is sound; the toolchain is the problem: Python is a deployment landmine and the Rust/Tauri lane is a lagging prototype the repo's own README steers users away from.

**Goal:** one self-contained static Go binary, `ringer`, with subcommands covering everything the repo currently leans on Python for, plus a `hud` subcommand replacing the Rust lane with the already-canonical web dashboard. Single binary is the gold standard; every deviation was interrogated and eliminated (see §13, §14).

**Non-goal:** redesigning the product. External contracts — manifest schema, config schema, on-disk state, HTTP API, engine spawn contract, SKILL.md operational contract — are frozen (§10). "Swap the toolchain" must not become "rewrite the product."

## 2. Settled decisions and rationale

| Decision | Rationale (evidence) |
|---|---|
| Hard cutover, no strangler period | Personal fork, no external install base; dual-maintenance buys nothing |
| Delete Tauri `hud/`; `ringer hud` = web server on :8700 | Tauri shell reads the same state dir the web HUD serves; README concedes web is ahead; native window chrome explicitly not wanted |
| Consolidate on `ringside.html`; delete per-run dashboard server + `dashboard.html` | Per-run page is stale and polls endpoints only :8700 implements; its other consumer (Tauri live iframe) is deleted |
| SQLite **only** (no JSONL source of truth, no Postgres) | ACID beats append-log repair conventions; upstream JSONL was a Python-stdlib artifact; deleting the derived-read-model subsystem (offset sync, `db rebuild/sync`, staleness bugs). Postgres bought only cross-machine aggregation, which upstream's own philosophy disavows; `db export/import` covers it |
| Driver: `modernc.org/sqlite` (pure Go) | Multi-source research 2026-07-08 (Perplexity deep research, Codex web research, direct reads of cznic tracker): multi-process support is a stated changelog feature (v1.1.0-alpha.2, 2019); zero corruption reports; flagship issue #232 is single-process pool contention, maintainer deems BUSY legitimate. Verdict: safe-with-caveats; caveats are config-level (§7). Fallback documented: `ncruces/go-sqlite3` (mptest-validated) behind the store seam |
| Linux worker sandbox: vendor flywheel's `jail` package, native engine integration | Rootless userns+mount-ns+chroot; `jail.Command()` returns `*exec.Cmd` (fits spawn path unchanged); default-deny reads is a security upgrade over Seatbelt (credentials invisible to rented-compute workers); no bwrap dependency. flywheel's copy is strictly ahead of lightweight-containers' (TmpfsMount, SetDropUser, /dev/pts) |
| Keep `engines/opencode-sandboxed.sh` | macOS Seatbelt lane, referenced by config path only; not Python/Rust |
| Stack | chi, BurntSushi/toml, go-flags, templ (artifact pages), `go:embed` (ringside.html), build.sh as sole entry point, table-driven tests |

## 3. CLI surface

Module `github.com/corruptmemory/ringer`, Go 1.26, `CGO_ENABLED=0`, go-flags subcommands.

| Subcommand | Contract |
|---|---|
| `run <manifest>` | As upstream: `--max-parallel --identity --no-dashboard --no-artifact --dry-run`, lint warnings after load, auto-HUD, catalog auto-refresh. `--browser` REMOVED (per-run dashboard deleted) |
| `lint <manifest>` | As upstream; exit 1 on findings |
| `demo` | 3-task toy manifest in /tmp, mirrors `run` flags |
| `hud` | Serve Ringside on 127.0.0.1:8700 (`--port --no-open`) |
| `models` | Scoreboard from SQLite: `--task-type --model --engine --since --explore --html --open --json` |
| `catalog` | OpenRouter snapshot: `--refresh --source --file --free --changes --json`; 24h throttle; background refresh on `run` |
| `db export` | Eval rows → JSONL (backup / grep / cross-machine) |
| `db import` | JSONL → SQLite; carries the legacy-backfill mapping semantics (`run_id:task_key` > `run_id` > `name:prefix`); replaces `scripts/backfill_model_log.py` |
| `db integrity` / `db checkpoint` | `PRAGMA integrity_check` / `wal_checkpoint(TRUNCATE)` |
| `mock-worker <spec>` | Byte-compatible port of `engines/mock_worker.py` (grammar: `MOCK_FILE:`/`MOCK_END`/`MOCK_FAIL`; path-escape guards; exits 0/1/2). `[engines.mock]` points at the ringer binary itself |
| `nudge-hook pre-bash\|post-edit` | Port of `hooks/ringer_nudge.py`: stdin hook JSON → `hookSpecificOutput.additionalContext`; same PROVIDER/HARNESS regexes; edit-spiral counter in `~/.ringer/nudge-state/`; always exit 0 |
| `install-agent` / `uninstall-agent` | Same idempotent settings.json merge + backup; hooks register `ringer nudge-hook …` (binary path, not python3); copies SKILL.md |
| `gen-config` | Reflection-based sample-config generator (house standard) |

Global: `--config PATH`. Identity resolution preserved: `--identity` > `FLEET_IDENTITY`/`RINGER_IDENTITY` > `.fleet-agent` walk-up > `identity_default` > hostname.

Removed config keys (`[eval] backend/jsonl_path`, `[eval.postgres]`) and any unrecognized key **fail loudly at load** with a migration hint (BurntSushi `md.Undecoded()`). No silent config rot.

## 4. Package architecture

```
cmd/ringer/          go-flags wiring only
internal/config      TOML load (strict), identity resolution
internal/manifest    parse + validate (frozen JSON schema)
internal/lint        heuristics ported near-verbatim (fidelity over elegance)
internal/engine      EngineConfig, args_template expansion, preflight
internal/jail        vendored from github.com/corruptmemory/flywheel jail/ (provenance header; deps: stdlib + x/sys)
internal/runner      orchestration core + run-state actor
internal/verify      check execution (sh -c, 60s cap), expect_files floor
internal/state       run-state JSON writer, active-runs registry (frozen formats)
internal/store       SQLite via modernc; schema, eval writes, scoreboard queries; driver seam for ncruces fallback
internal/catalog     OpenRouter fetch/normalize/diff/throttle + events
internal/scoreboard  tiers (proven/probation/untested), explore, MODEL-NOTES.md + registry/model-identity.toml parsing
internal/artifact    templ-rendered status/report/index/library pages (frozen output paths + library.json schema)
internal/hud         chi server :8700, embedded ringside.html, JSON API
internal/agent       install/uninstall-agent
```

Dependency direction: `cmd` → everything; `runner` → engine/jail/verify/state/store/artifact; `hud` → state/store/artifact (read-side). No package reaches back into `runner`.

`build.sh`: `templ generate` → `go vet` → `CGO_ENABLED=0 go build` → `--test [--race]`. No npm, no air.

## 5. Runner core — actor-owned state

Upstream shape: asyncio task loop + three threads (state flusher, HTTP, catalog) sharing `TaskRuntime` objects under one `RLock`. Go shape: **actor pattern** (house standard).

**Process topology (why one process, why exec only at the leaf):** `ringer run` is the single long-lived orchestrator. Everything sitting *on top of* the engines — scheduler, run-state actor, output-collector actor, per-task workers, kill/reap — is goroutines inside that one process. `exec` appears only at genuine external-program boundaries: the engine binaries (opencode/codex/claude), the user's verify command (`sh -c`), and — under Plan-3 isolation — the `unshare` namespace-entry trampoline whose bash `exec chroot`s *into* the engine (it replaces itself; it is not a supervisor). There is no `ringer`-execs-`ringer` orchestration layer and no external process manager (tmux etc.). This is not a change from upstream: `ringer.py` was already one asyncio process spawning engines directly (`create_subprocess_exec`, `active_processes` dict, process-group reaping) — the port keeps that topology and upgrades the substrate (goroutines + typed-channel actors replace the event loop + `RLock`). The §5 spawn invariants below are the portable OS-level part of child supervision, not a Pythonism.

- `runActor` goroutine owns all mutable run state (task runtimes, attempt counters, active process-group registry). Typed command channel: `taskStarted`, `attemptDone`, `logActivity`, `snapshot{reply chan<- RunSnapshot}`, `killAll`.
- One goroutine per task, gated by a semaphore channel (`max_parallel`). Per-task: prepare dir (or `git worktree add`) → attempt loop (max 2; retry re-spawns with the check's failure output appended to the spec) → harvest deliverables (worktrees mode: `git worktree remove` on PASS — footgun semantics preserved and documented).
- **Spawn invariants (frozen):** argv from `args_template` (placeholders substituted per token: `{taskdir} {spec} {model}` string-replaced; `{access_args} {engine_args} {sandbox_args} {full_access_args}` list-spliced); cwd = taskdir; stdin `/dev/null`; `Setpgid`; stdout+stderr merged, teed (`io.MultiWriter`) to worker log + our stdout + a per-task sink into the run's **output-collector actor** (a run-scoped actor owning bounded, chunk-granular per-task tails — no fixed-size ring buffer; recent output for token scraping and the live HUD); token count scraped from that tail via engine `token_regex`.
- Timeout: `SIGTERM` to process group → 5s grace → `SIGKILL`. `killAll` on cancellation.
- StateWriter goroutine: 1s tick → snapshot from actor → atomic write (temp + rename) of `~/.ringer/runs/<run_id>.json`, schema frozen (died-run detection and Ringside depend on it).
- Eval row per attempt → `store` (short INSERT transaction).

## 6. Engine layer and jail

`EngineConfig` mirrors the TOML contract: `bin`, `args_template`, `sandbox_args`, `full_access_args`, `token_regex`, `model_default`. New optional keys:

```toml
[engines.opencode]
bin = "opencode"              # direct binary on Linux; no wrapper script
args_template = ["run", ...]
isolation = "jail"            # none (default) | jail
jail_state_dirs = ["~/.config/opencode", "~/.local/share/opencode"]  # rw binds
jail_ro_binds = ["~/.opencode"]   # ro binds — engine installs living outside the host-toolchain mounts (Plan 3 addendum 2026-07-09)
```

- `isolation = "jail"` (Linux): per-task `UnshareJail` — read-only host toolchain (`HostMounts`: /usr /etc /bin /lib…), taskdir rw, tmpfs scratch wired as `TMPDIR`/`XDG_CACHE_HOME`, `jail_state_dirs` rw. Default-deny reads: `~/.ssh`, `~/.claude.json`, etc. do not exist inside the namespace. Network open (no netns) — matches the Seatbelt wrapper's threat model: confine an honest-but-sloppy CLI, not a malicious one.
- `jail.Command()` returns `*exec.Cmd` → the §5 tee/timeout/kill path is identical jailed or not.
- `full_access: true` (gated by config `allow_full_access`) = no jail (unchanged semantics).
- macOS: `isolation = "jail"` fails preflight with a clear message; Seatbelt wrapper remains the macOS lane. Worktrees mode: parent repo path must be bind-mounted ro into the jail (worktree gitdir pointer) — spike, §14.
- Preflight: engine bins with install hints (as upstream) + `CheckUnsharePreflight()` iff any task jails.

## 7. Store (SQLite)

- One DB: `~/.ringer/ringer.db` (path derives from `state_dir`). Tables: `attempts` (columns = frozen eval-row keys: run_id, run_name, task_key, engine, model, task_type, verdict, retry, duration_s, tokens, check output, identity, timestamps), `catalog_models`, `catalog_events`, `identity`, `schema_version`.
- **Pragma discipline** (from 2026-07-08 driver research): `journal_mode=WAL`; `busy_timeout=5000` set via PRAGMA — never `_txlock=immediate` (cznic #192: busy_timeout ignored with it, unfixed at v1.53.0); `synchronous=NORMAL`; `SetMaxOpenConns(1)` per process; explicit `wal_checkpoint(TRUNCATE)` at run end (cznic #179: WAL growth). Pin `modernc.org/libc` to the version in modernc's own go.mod. Local filesystem only.
- Writers: runner (one short INSERT per attempt). Readers: `models`/explore, HUD `/api/models`. Workload envelope: ≤5 concurrent writer processes, ~10 writes/min — far inside SQLite's envelope.
- **Multi-process smoke test is a permanent CI test** (N processes hammering one DB; assert no lost rows, bounded BUSY). If it ever indicts modernc, swap the store seam to `ncruces/go-sqlite3` (CGo-free, validated with SQLite's `mptest`).
- Scope boundary: SQLite replaces the *eval log* only. Run-state files, `active-runs.json`, artifacts tree stay filesystem (single-writer live snapshots + served files; a died run leaving its last state file is load-bearing).

## 8. HUD and artifacts

- `ringer hud`: chi on 127.0.0.1:8700. Frozen endpoints: `/` (ringside), `/api/runs`, `/api/models`, `/api/library`, `/api/open-folder` (fix: `xdg-open` on Linux, `open` on macOS), `/artifacts/<path>`, `/logs/<run_id>/<task_key>` (64KB tail). Single fixed port; fail if taken (as upstream).
- `ringside.html`: models tab **baked into the committed asset** (upstream injected at serve time for Tauri-sharing reasons that no longer exist); embedded `go:embed`. `dashboard.html` + `hud/` tree deleted.
- `run` keeps ensure-HUD-running: probe :8700 → spawn detached `ringer hud` → open browser once per session.
- Artifacts: templ components render the zero-LLM pages byte-equivalent-in-contract (paths `~/.ringer/artifacts/live/<run_name>.html`, versioned `<run_id>.html`, `<run_id>-report.html`, `index.html`, `library.json` schema frozen — Ringside consumes these). Dead-run reconciliation + deliverables copy ported.

## 9. Frozen contracts (conformance checklist)

1. Manifest JSON schema (all task + run-level fields incl. `worktrees`, `full_access`, `engine_args`, `verified`, `task_type`).
2. Config TOML schema minus removed keys, plus `isolation`/`jail_state_dirs`.
3. Engine spawn contract: args_template DSL, cwd, stdin closed, merged raw output, token_regex, full-access gating. `engines/opencode-sandboxed.sh` keeps working unchanged (macOS).
4. On-disk: `~/.ringer/runs/<id>.json` schema, `active-runs.json`, artifacts tree + `library.json`, `nudge-state/`, worker log locations (taskdir or `workdir/logs/` in worktrees mode). **Adjudicated 2026-07-09 (Plan 2 final review):** `active-runs.json` keeps Python parity (5-field entries incl. `workdir`; prune-on-write) because both eras write the shared file. `runs/<id>.json` is **Go-authoritative** (the Task 5 Go schema, `done:bool` etc.) — it is written only by Go runs; the Python HUD is out-of-scope for Go-written state dirs, and Plan 4's Go HUD reads the Go schema.
5. HTTP API consumed by `ringside.html` (§8 list).
6. CLI surface per SKILL.md: `lint` before `run`, `demo`, `--dry-run`, `--identity`, `models [--task-type|--explore|--open]`, `catalog --changes`, `hud`, `install-agent`.
7. `registry/model-identity.toml` format.
8. The four hard-won invariants, verbatim: stdin closed; sandbox mode explicit; verification executes the artifact; logs carry raw worker output only.
9. Mock-worker spec grammar + exit codes; nudge-hook stdin/stdout protocol + exit-0-always.

## 10. Testing

- **Unit (table-driven):** lint heuristics (seeded from upstream's cases), manifest parsing, args_template expansion, mock grammar, identity resolution, catalog diffing, scoreboard tiers, artifact goldens.
- **E2E (black-box):** `ringer run` with mock engine — pass task + fail-then-retry task (locks the whole loop; mirrors upstream's only black-box test); `httptest` over the HUD API; jail integration test (auto-skip without userns); multi-process store smoke.
- **CI:** one GitHub Actions workflow: `./build.sh --test` on Linux + macOS. Tauri `release.yml` deleted.
- Error-logging discipline: errors route through the logger; tests install a recording logger — positive tests assert zero `.ERROR`s suite-wide.

## 11. Cutover (one branch, one PR)

Delete: `ringer.py`, `hud/`, `tests/*.py`, `hooks/`, `scripts/`, `engines/mock_worker.py`, `dashboard/dashboard.html`, `.github/workflows/release.yml`.
Keep: `engines/opencode-sandboxed.sh`, `templates/`, `registry/`, `docs/`, `dashboard/ringside.html` (moves into `internal/hud/` for embedding).
Update: README (requirements: drop Python; `./ringer.py …` → `ringer …` everywhere), SKILL.md (same sweep), `config.sample.toml` (isolation keys; `[eval]` simplified), `.claude/skills/ringer` symlink target unchanged.
Migrate: one-time `ringer db import --jsonl ~/.ringer/runs.jsonl` seeds eval history. Run-state files and artifacts remain readable as-is.

## 12. Removed (deliberate cuts)

Postgres/Supabase eval backend; JSONL as source of truth; derived-read-model machinery (`db rebuild/sync`); per-run dashboard server + `dashboard.html` + `--browser`; Tauri desktop app; standalone backfill script (absorbed by `db import`); serve-time models-tab injection (baked in); macOS-only `open-folder` behavior (now per-OS).

## 13. Spikes and risks (front of the implementation plan)

1. **opencode as namespace-UID-0:** if opencode tolerates running as ns-root (maps to the invoking user's UID), files land user-owned and the UID-mapping/cleanup tax vanishes; else wire `SetDropUser` + chown/chmod discipline on taskdirs and namespace-aware cleanup.
2. **Worktrees × jail:** git worktree dirs reference the parent repo via gitdir pointer; jail mount table must bind the parent repo ro for in-jail git to function. Verify; document interaction with the worktree-deletion-on-PASS footgun.
3. **modernc multi-process smoke** (promoted to permanent CI, but first run is the go/no-go for the driver choice).
4. **Behavioral-drift corners:** lint heuristics and artifact HTML are ported near-verbatim with golden tests to pin them.
5. Host prereqs for jail: userns enabled (Arch default), util-linux present; `subuid/subgid` only if `SetDropUser` proves necessary. Preflight reports precisely.

## Appendix: SQLite driver research (2026-07-08)

Question: is `modernc.org/sqlite` safe for one DB file written by ≤5 concurrent processes (~10 writes/min) + dashboard reader on local Linux FS?
Lanes: Perplexity deep research; OpenAI Codex web research; direct browser reads of gitlab.com/cznic/sqlite (README + issue #232). (A fourth lane, Antigravity CLI, produced no usable output.)
Convergent verdict: **safe-with-caveats** — caveats config-level, encoded in §7.
Key evidence: CHANGELOG v1.1.0-alpha.2 (2019-12-26) explicitly adds multi-process concurrent access; WAL/`-shm` is translated C-SQLite VFS code, actively maintained (regression chain fixed v1.51.0, 2026-05-28; current v1.53.0/SQLite 3.53.2); zero multi-process corruption reports; #232 is a single-process 100-connection pool complaint the maintainer attributes to legitimate contention; #192 (`busy_timeout` × `_txlock=immediate`, unfixed) and #179 (WAL growth) drive the §7 rules. Fallback: `ncruces/go-sqlite3` v0.35.2 (2026-07-06), OFD locks + mmap shm, validated with SQLite's `mptest`.
