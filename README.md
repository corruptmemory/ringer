# Ringer

![Ringer — she reviews; the wall works](docs/hero.png)

**Parallel AI-agent swarms that prove their work. Your expensive model plans and reviews; cheap workers do the typing.**

Frontier models are finally good enough to trust with real implementation — but their tokens are priced like senior-engineer hours, and most of a build is not senior-engineer work. It's scaffolding, migrations, test suites, batch transforms. Mechanical labor.

So split the roles. Your best model writes the specs and reviews the results. A swarm of cheap workers — Codex, Grok, anything with a CLI — does the implementation in parallel. Your premium budget stops scaling with lines of code written and starts scaling with decisions made.

One problem: parallel agents lie. "Done" doesn't mean working. Ringer doesn't take the worker's word for anything — it **executes your check command** against the artifact. Pass or fail is decided by running the code, not by reading the agent's summary. Failures retry once with the failure context injected, and every attempt is logged so your setup gets measurably better over time.

And because a swarm you can't see is a swarm you don't trust: **Ringside**, a local web page every run opens automatically, showing every live swarm on your machine — who's running it, what each worker is doing, elapsed time, token burn — in real time, plus a versioned library of what past runs produced.

## How it works

```
manifest.json ──▶ ringer ──▶ N parallel workers (codex exec, each in its own dir)
                      │                │
                      │                ▼
                      │         executed checks ── fail ──▶ retry once w/ failure context
                      │                │
                      ▼                ▼
              ~/.ringer/runs/    eval log (SQLite)
                      │
                      ▼
              Ringside, in the browser (live, all swarms, all identities)
```

## Quickstart

Ringer runs on macOS and Linux (Windows via WSL) and ships as a single static binary — no runtime dependencies.

1. Install a worker CLI and sign in (Codex is the built-in default engine):

```bash
npm install -g @openai/codex   # or: brew install --cask codex
codex login                    # sign in with your ChatGPT plan
```

2. Get the repo:

```bash
git clone https://github.com/NateBJones-Projects/ringer && cd ringer
mkdir -p ~/.config/ringer && cp config.sample.toml ~/.config/ringer/config.toml   # optional — sane defaults without it
```

3. Teach your agent to route work through Ringer:

```bash
# optional but recommended: teach your agent to route work through ringer
ringer install-agent
```

4. Run the demo:

```bash
ringer demo                                           # 3 real workers, verified end to end
```

The demo spawns three Codex workers in parallel, verifies each artifact by executing it, and prints a verdict table — and Ringside, the live dashboard, opens in your browser on its own. If all three say PASS, that's the whole setup.

Run your own batch:

```bash
ringer run swarm.json --max-parallel 4
```

```json
{
  "run_name": "my-batch",
  "workdir": "/tmp/my-batch",
  "max_parallel": 3,
  "tasks": [
    {
      "key": "alpha",
      "spec": "Create alpha.txt containing exactly: alpha ready",
      "check": "test \"$(cat alpha.txt)\" = \"alpha ready\"",
      "expect_files": ["alpha.txt"]
    }
  ]
}
```

Each task gets its own directory, its own worker, its own log, and its own verdict. `check` is any shell command — exit 0 is the only thing Ringer believes.

> **Write checks that print why they fail.** A silent `exit 1` (the `git diff --quiet` style) costs you twice: the retry prompt gets no failure context to fix against, and the eval log records an undiagnosable row. `diff` beats `diff -q`; an assert with a message beats a bare test.

**Identity**: runs are stamped with an orchestrator identity (shown in Ringside and eval rows). Resolution order: `--identity` > `FLEET_IDENTITY`/`RINGER_IDENTITY` env > a `.fleet-agent` file found walking up from the working directory (drop one in a repo root to give that repo's swarms their own name) > `identity_default` in config > short hostname.

### Manifest fields

| Field | What it does |
|---|---|
| `key` | Task name — becomes the working subdirectory and the label everywhere |
| `spec` | The prompt handed to the worker |
| `check` | Shell command run after the worker exits; exit 0 = PASS |
| `expect_files` | Files that must exist and be non-empty before the check runs |
| `engine` | Which configured engine runs this task (default `codex`) |
| `model` | Which model a harness engine runs for this task — fills the engine's `{model}` placeholder (e.g. `"openrouter/moonshotai/kimi-k2.7"`); empty uses the engine's `model_default` |
| `task_type` | Optional free-form string naming the kind of work this task is, so the model-performance log can slice pass rates by task shape rather than only by model. Suggested vocabulary: `code-feature`, `code-fix`, `code-review`, `test-hardening`, `docs`, `research`, `persona-review`, `copywriting`, `site-build`, `motion-design`, `image-gen`, `data-pipeline`, `format-conversion`, `probe`, `bakeoff`. Empty is allowed; the log just reports it under `(none)`. |
| `timeout_s` | Per-task kill timer (default 900) |
| `engine_args` | Extra CLI flags for this task's worker, spliced in at the engine's `{engine_args}` placeholder — e.g. `["-c", "model_reasoning_effort=low"]` so the orchestrator picks reasoning depth per task |
| `verified` | One plain-English sentence saying what the check proves — shown on the results page next to "finished & checked" |
| `full_access` | Worker runs unsandboxed — required for workers that spawn their own sub-workers; must also be enabled in config |
| `worktrees` (run-level) | Give each task an isolated git worktree of `repo` so parallel workers can't collide |

> **Worktree footgun:** on PASS the task's worktree is removed — including anything written inside it. In worktrees mode, worker logs live outside task worktrees in `workdir/logs/`; have workers write deliverables outside the worktree too, or have your `check` copy artifacts out before it exits 0.

Not sure what your tasks even are yet? [`docs/interview-prompt.md`](docs/interview-prompt.md) is a prompt you paste into any chatbot; it interviews you about the job and hands back a brief your orchestrating agent can turn into a manifest. Ready-made skeletons for the patterns that work live in [`templates/`](templates/).

## Lint

Lint checks a manifest for the mistakes that make swarms hard to trust: checks that cannot fail, silent checks, worktree deliverables that disappear, worker commits that die with deleted worktrees, serial fan-out, write collisions, and underspecified specs.

```bash
ringer lint templates/review-swarm.json
lint: clean (1 tasks)
```

`run` and `demo` also print any lint findings as non-blocking warnings after the manifest loads. They teach at the moment of use; they do not stop a run.

A check that cannot fail is trusting the worker with extra steps.

## Make your agent actually use this

Between swarms, agents drift back to invisible inline work. Reminders decay, so enforcement ships with the product.

Run one command:

```bash
ringer install-agent
```

It installs the ringer skill — the orchestrator playbook — user-level for Claude Code, and registers two gentle hooks: a Bash hook that notices model-calling or harness commands running outside a live Ringer run, and an edit-loop hook that notices batch editing without a run. Each hook nudges ONCE per session, pointing the agent at the skill.

The hooks never block anything. A user who says "just do it inline" is obeyed; uninstall with `ringer uninstall-agent`.

For CI and evals, `config.sample.toml` includes `[engines.mock]` so the enforcement stack can be tested without an API bill.

> **Migrating from the Python ringer:** if you keep a non-default `state_dir` in your config, also set `$RINGER_HOME` to that same path — otherwise the nudge hook's live-run suppression won't see your active runs (harmless; you'll just get an occasional extra advisory nudge during a live run). Also, `ringer uninstall-agent` only recognizes hooks it installed itself (matched by the `nudge-hook` marker in the hook command) — it will not remove a legacy `python3 …/ringer_nudge.py` hook left behind by the old Python `install-agent`. Run the old Python `uninstall-agent` first, or delete that hook entry from `settings.json` by hand.

## Engines are pluggable

![Identical workers, each under its own light](docs/engines.png)

Ringer ships with three worker lanes: **Codex CLI** is the built-in default, and `config.sample.toml` carries verified engine blocks for **Grok Build CLI** (works as-is once you `grok login`) and **OpenCode + OpenRouter** (one edit: point `bin` at the sandbox wrapper in your clone). Anything else with a headless CLI is a config block away:

```toml
[engines.mymodel]
bin = "/usr/local/bin/mycli"
args_template = ["run", "{spec}", "--dir", "{taskdir}"]
```

Per-task `"engine": "mymodel"` routes work to it — the invariants (stdin closed, process-group kill, executed verification, raw logs) apply to every engine identically.

### The universal harness: OpenCode + OpenRouter

Unless a model ships its own first-class harness (Codex does), OpenCode is the harness that runs it — one engine block covers every OpenRouter-served model. `config.sample.toml` includes a ready-to-uncomment engine whose `{model}` placeholder is filled per task from the manifest's `"model"` field, with `model_default` as the fallback. The shipped default is OpenRouter's `z-ai/glm-5.2` — roughly $0.74/M input and $2.33/M output (2026-07), about 20-30x cheaper output than frontier coding models; a complete write-code-and-pass-the-check task lands around a penny.

OpenCode ships no OS sandbox, so the engine's `bin` points at an absolute path to `engines/opencode-sandboxed.sh` (ringer does not resolve engine bins relative to the repo): a macOS Seatbelt wrapper that leaves network and reads open but confines writes to the task dir, a per-run scratch dir (wired as the agent's `TMPDIR`/`XDG_CACHE_HOME`), and OpenCode's own state/config dirs. Its `--dangerously-skip-permissions` flag only silences OpenCode's interactive prompts; Seatbelt is the actual containment. Task paths reach the profile as `sandbox-exec -D` parameters rather than string interpolation, so a task dir with quotes or parens can't inject sandbox rules. `--no-sandbox` is wired as the engine's `full_access_args`, so ringer's `allow_full_access` gate still governs escapes. Non-macOS installs need their own sandbox (or full-access mode).

Setting it up takes about five minutes:

```bash
# 1) Install the OpenCode CLI (pick one)
curl -fsSL https://opencode.ai/install | bash
# or: npm install -g opencode-ai
# or: brew install anomalyco/tap/opencode

# 2) Connect OpenRouter — create a key at https://openrouter.ai/settings/keys
opencode auth login   # select OpenRouter, paste the key

# 3) In ~/.config/ringer/config.toml, uncomment [engines.opencode] and set
#    bin to the ABSOLUTE path of engines/opencode-sandboxed.sh in this clone.
#    (Linux/WSL: the wrapper is macOS-only — set bin to the opencode binary
#    itself; there is no OS write-confinement then, so keep manifests scoped.)
```

Route with per-task `"engine": "opencode"`, pick the model with per-task `"model": "openrouter/<any-model>"`, and set reasoning effort via `engine_args`: `["--variant", "low|high|max"]`. A sensible split: mechanical or tightly-specced tasks on the cheap lane, gnarly ones on your frontier engine — the executed check catches shortfalls either way, and `swarm_runs` rows tell you whether the cheap lane's pass rate holds.

### The plan lane: Grok Build CLI

If you already pay for SuperGrok or X Premium Plus, Grok Build is a second flat-rate worker lane — no per-token bill:

```bash
# 1) Install (pick one)
curl -fsSL https://x.ai/cli/install.sh | bash
# or: npm install -g @xai-official/grok

# 2) Sign in — OAuth on a SuperGrok or X Premium Plus plan
grok login

# 3) In ~/.config/ringer/config.toml, uncomment [engines.grok]
```

Route with per-task `"engine": "grok"` and pick the model with `"model": "grok-build"` or `"model": "grok-composer-2.5-fast"` (the shipped default — the speed pick). Grok brings its own OS sandbox on macOS (profile `workspace`: read everywhere, writes confined to the task dir, temp, and `~/.grok`), and its JSON output exposes no token counts — plan-billed workers report cost as included in plan.

## Ringside — mission control

![Ringside in the browser: a run's live results page with per-worker status and verification](docs/ringside.png)

Ringside is a local web page — no install, no account, nothing leaves your machine. Your first run opens it automatically; every later run streams into the same tab:

```bash
ringer run manifest.json        # starts Ringside and opens the tab for you
ringer hud                      # or open it any time → http://127.0.0.1:8700
```

The top of the page is the run's live results document: what the job is, a progress bar of rounds, and "The work" — every deliverable each worker filed, with a plain-English line saying what the check proved and the raw check output one click away. Below it, the agents: expand a worker to see the exact brief it was handed, which engine and model are typing, and its live work stream. Past runs stay in a versioned library, and a swarm whose orchestrator *died* without finishing gets its own unmissable state — the failure mode every dashboard forgets.

Multiple swarms at once is the designed-for case: run three batches under three identities and Ringside shows all three, live. `--browser` opens a simpler per-run fallback dashboard, and `--no-dashboard` runs headless.

## The eval loop

![Timed, verified, logged](docs/eval-loop.png)

Every worker attempt — pass, fail, timeout, retry — is logged with its spec, engine, duration, token count, and the raw check output. Local SQLite by default (`<state_dir>/ringer.db`); `ringer db export` dumps it to JSONL to aggregate across machines. Failure rows are the point: they tell you which spec styles, engines, and task shapes actually work, so the swarm gets better on evidence instead of vibes.

## Model performance log

Every task attempt is logged **automatically and locally** to a SQLite database at `<state_dir>/ringer.db` (`~/.ringer/ringer.db` by default) — no setup, no account, nothing leaves your machine. Each row carries the per-attempt verdict straight from the EXECUTED check, plus duration, tokens, the resolved `model`, the task's `task_type` (if the manifest set one), and the `retry` number.

Read it with:

```bash
ringer models                # per-(model, task_type) scoreboard across the local log
```

The scoreboard reports, per model and task_type: tasks, attempts, `pass_rate`, `first_try_pass_rate`, median duration and token count, and `last_seen`. The signal for routing is `first_try_pass_rate` — the share of tasks that passed on attempt 1 without a retry; `pass_rate` is the rescued rate after Ringer's single retry, so the gap between the two is the cost of the retry lane. Slice the log with `--task-type`, `--model`, `--engine`, `--since`, or `--json` for piping elsewhere.

History from before the `model` / `task_type` / `retry` columns existed can be imported in one pass — `ringer db import` reads a legacy JSONL log and inserts the rows into the eval database, backfilling `model` from each run's on-disk state and `task_type` from an optional mapping file:

```bash
ringer db import \
  --jsonl ~/.ringer/runs.jsonl \
  --runs-dir ~/.ringer/runs \
  --mapping mapping.json
```

The `--mapping` file joins old log rows to a `task_type`. Each line uses one of three key forms, applied in order:

- `run_id:task_key` — names one task in one run (most specific).
- `run_id` — names every task in that run.
- `name:prefix` — names every task whose key begins with `prefix`, across all runs (least specific, the usual way to cover a whole kit's keys).

Rows that match nothing keep their old `task_type` (empty); rows whose run-state JSON can't be found keep their old `model`.

`docs/MODEL-NOTES.md` is where the human-readable judgment lives on top of these numbers — the scoreboard tells you the pass rates; the notes tell you why a model shines or chokes on a given task shape.

### Evidence-based routing

The scoreboard only knows models you've already run. To reason about models you *haven't* tried yet, Ringer keeps a local snapshot of the OpenRouter catalog and a change log alongside the runs log:

```bash
ringer catalog                        # fetch/refresh ~/.ringer/openrouter-catalog.json
```

| Flag | What it does |
|---|---|
| `--refresh` | Force a re-fetch even if the snapshot is fresh |
| `--source URL_OR_PATH` | Pull from a non-default URL or local file instead of the live OpenRouter API |
| `--file PATH` | Read a catalog document you already have on disk, no network |
| `--free` | Filter to models with a $0 price — promo models included |
| `--changes` | Print the recorded add/remove/price_change/went_free/went_paid events from `.changes.jsonl` |
| `--json` | Emit the snapshot (or, with `--changes`, the event log) as JSON for piping |

The snapshot lives at `~/.ringer/openrouter-catalog.json`; the change log sits beside it as `~/.ringer/openrouter-catalog.changes.jsonl`, appending one row per added, removed, price-changed, went-free, or went-paid event between snapshots. Free promos get their own call-out (`went_free`) because a temporarily-free model is a zero-cost experiment — the cheapest way to audition a new model is to catch it while someone else is paying for it.

Catalog fetches are throttled to once per 24 hours. A `run` triggers that refresh in the background on its way up; it never blocks or fails a run — if the fetch is slow or the network is down, Ringer carries on with the snapshot it has. The throttle and the auto-refresh-on-run are both documented in `ringer run --help` and can be turned off there.

Once you have a catalog and a log, `models --explore` joins them into a routing recommendation:

```bash
ringer models --explore                       # tiers across all task types
ringer models --explore --task-type docs      # tiers for one task shape
```

Models with local evidence are sorted into tiers:

- **proven** — 3+ tasks of this `task_type` logged, with `first_try_pass_rate >= 0.67`. The lane you trust with heavy work.
- **probation** — some attempts logged but not enough volume or not enough first-try passes. Use it; don't lean on it.
- **untested** — nothing in the log yet. Pulled from the catalog: text→text, 32k+ context window, up to 10 candidates, FREE models first then cheapest. These are your audition queue.

The promotion ladder is the point. A model enters as **untested**. You spend a small slice of suitable runs — about one task per run — auditioning cheap or free candidates on small, low-stakes work where the executed check is strong and the single retry absorbs the failure: docs sweeps, mechanical edits, persona reviews. While evidence accumulates the model sits on **probation**. At 3+ tasks with `first_try_pass_rate >= 0.67` it's **proven** for that task type and earns a lane on the heavy work. The recommendation flow is the same one this ladder implies: exploit proven models for the load-bearing tasks, and keep spending that small slice auditioning untested candidates so the bench refills itself.

The per-user philosophy, stated plainly: every user's workload is different, so the scoreboard learns what works for *your* tasks on *your* machine. A model that's proven in someone else's log is untested in yours until you've run it. The numbers are not portable between users, and the routing recommendations get personal as the log grows — which is exactly why the catalog and the change log stay local and the explore tiers are computed from your own eval database, not from anyone's aggregate.

## Hard-won invariants

Four rules are baked into every worker invocation. They all cost us real debugging hours; you get them for free:

1. **stdin is always closed** (`< /dev/null`) — headless CLI agents hang forever waiting on a TTY that isn't there.
2. **Sandbox mode is always explicit** — default sandboxes silently resolve to read-only in temp directories and block every artifact write.
3. **Verification executes the artifact** — an agent's own "done" is not evidence. Exit codes are.
4. **Raw output only** — logs and eval rows carry verbatim worker output, never a summary. Anything that needs judgment reads the raw data.

## License

[PolyForm Shield 1.0.0](LICENSE.md) — free to use, modify, and share, including inside your own commercial work. The one thing you can't do is offer Ringer or Ringside (or a derivative that competes with them) as a product or service of your own. Commercial rights to the tool itself belong to Nate Jones Media LLC.

See [PROVENANCE.md](PROVENANCE.md) for the project's origins and credits — Ringer and Ringside were created by Jonathan Edwards; this repository is a Go rewrite that keeps their product design intact.

## Requirements

Ringer ships as a single static binary — macOS and Linux (Windows via WSL). To run it, that's the whole list; you also need at least one agent CLI (Codex works out of the box).

To build from source: Go 1.26 and `./build.sh` (the only supported entry point — it generates the templ views, formats, vets, and builds; add `--test` to run the test suite).

![Between rounds](docs/between-rounds.png)

---

Built by [Jon Edwards](https://limitededitionjonathan.com) and his agent fleet — a Claude orchestrator wrote the specs and reviewed the diffs, Codex swarms wrote the implementation, and this repo's own eval table caught its first three bugs. The tool is its own proof of concept.
