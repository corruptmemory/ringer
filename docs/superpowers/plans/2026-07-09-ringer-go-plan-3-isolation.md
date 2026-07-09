# Ringer Go Plan 3 — Isolation Implementation Plan

> **STATUS: EXECUTED 2026-07-09** (branch `go-isolation`, 10 tasks + fixes, whole-branch review = READY TO MERGE). **The code on the branch is authoritative over the snippets below** where they drift. Corrections found during execution (the snippet had a latent bug; the shipped code is right):
> - **Task 5** — the write-collision scan snippet keys the map on `config.ExpandUser(p)` (expanded path); Python parity (ringer.py:592-605) requires filtering on the expanded form but keying/printing the *original literal* `p`. Shipped code keys on `p`.
> - **Task 7** — (a) the worktrees E2E's `failer` task used `Check: "true"`, which always PASSes because the verdict is decided by the check alone, never the worker's exit code (§9 invariant); shipped test uses a failing check. (b) The plan had no workdir/repo absolutization; shipped code normalizes both to absolute + `ExpandUser` at manifest load (ringer.py:489/494 parity) — a real relative-`workdir` bug (`git -C <repo>` vs Go fs calls resolve a relative taskdir differently).
> - **Task 9** — the ABI-probe snippet calls `unix.LandlockCreateRuleset`, which does **not** exist in the pinned `golang.org/x/sys v0.44.0`; shipped code uses the raw `unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)` (verified against go-landlock v0.9.0's own internal shim). Also added: the `landlock-exec` trampoline self-refuses via `isolate.LandlockABI()` before `RestrictPaths`, because go-landlock's `BestEffort()` returns nil even on a Landlock-less kernel (silent-unconfined gap).
> - **Task 10** — the jailed worktrees E2E's `Check: "true"` was vacuous (same footgun as Task 7); shipped test makes the jailed `git status` write a host-visible artifact and asserts a success marker, so a broken `RepoRO` mount actually fails the test (empirically confirmed: fails with real `fatal: not a git repository` when `RepoRO` is disabled).
>
> **Whole-branch review Minors, accepted to ship (follow-ups, none gate merge):** redundant host-toolchain double-mount (isolate `HostMounts` + jail `writeUnshareMounts` both mount /usr,/etc — pick one owner); `.jail`/`.scratch` base dirs left as empty litter after per-key cleanup; interrupt teardown test wires no real Store (no-eval-row-on-interrupt is inspection-verified); Landlock refusal branch untestable on a Landlock-capable host; `landlock.V5` kept (V9 available); Landlock RO/RW sets intentionally broader than the jail (the weaker fallback tier, divergence #7). Informational: graceful SIGTERM is inert against a jailed PID-namespace tree, so every jailed interrupt/timeout waits the full 5s `termGrace` before SIGKILL — within the §9.3 contract, not a hang.
>
> **Out-of-scope carry-forward for a later plan:** `allow_full_access` config gating is still unenforced (spec §6 says `full_access` is gated by it; Plan 2 shipped it ungated; Plan 3 only implements "full_access = no jail"). Pre-cutover item.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `isolation = "jail"` engines run inside per-task rootless sandboxes (userns jail, Landlock fallback, refusal when neither is available), worktrees mode works end-to-end, and Ctrl-C tears a run down cleanly — plus the Plan-2 carry-forward fixes (pidAlive guard, failure-context cap, Checkpoint run-end caller, retry-log append, W6 lint).

**Architecture:** A new `internal/isolate` package owns the `Isolator` abstraction: `Wrap(spec)` transforms a plain worker spawn (bin+argv in a taskdir) into an isolated one, leaving the §9.3 spawn path (`runWorker`: tee, timeout, Setpgid group kill) untouched. The jail backend builds an `internal/jail.UnshareJail` per task (host toolchain ro, taskdir rw at its host-identical path, tmpfs `/tmp`, state dirs rw, engine installs ro, worktree parent repo ro). The Landlock backend re-execs the ringer binary through a hidden `landlock-exec` trampoline subcommand that applies a Landlock ruleset to itself and then execs the engine. `isolate.Select` implements the jail > Landlock > refuse chain, logging any fallback at Warn.

**Tech Stack:** Go 1.26, `internal/jail` (vendored flywheel, userns/chroot), `github.com/landlock-lsm/go-landlock` (new dep, Landlock LSM), `golang.org/x/sys/unix` (ABI probe), git CLI (worktrees), existing `internal/{runner,engine,config,state,store,lint,manifest,logging}` packages.

## Global Constraints

- Build/test ONLY via `./build.sh --test` (never raw `go build` / `go test`). `gofmt` clean (build.sh enforces).
- Design spec §9 frozen contracts hold. The four invariants verbatim: **stdin closed; sandbox mode explicit; verification executes the artifact; logs carry raw worker output only.**
- §9.3 spawn contract unchanged by isolation: process group via `Setpgid`, timeout kill = SIGTERM → 5s grace (`termGrace`) → SIGKILL to `-pgid`, output tee via `io.MultiWriter`, stdin from `/dev/null`. Isolation only substitutes WHICH bin+argv is spawned.
- Spec §6: jailed tasks get a read-only host toolchain (`jail.HostMounts`: /usr /etc /bin /lib …), taskdir read-write, tmpfs scratch wired as `TMPDIR`/`XDG_CACHE_HOME`, `jail_state_dirs` read-write. Default-deny reads: unmounted paths do not exist inside the namespace. Network stays open (no netns). `full_access: true` = no jail (unchanged semantics).
- Spec §6: on macOS, `isolation = "jail"` fails preflight with a clear message pointing at the Seatbelt wrapper lane (`engines/opencode-sandboxed.sh`).
- Isolation fallback chain (settled decision): **jail > Landlock > refuse**. A fallback is logged at Warn; refusal is an error naming both missing capabilities. Never silently downgrade.
- Errors route through the injected `logging.Logger` — never `_ =` an error silently; non-fatal errors log at Warn, fatal at Error. No silent failures, ever.
- `internal/jail` is vendored from flywheel: any divergence MUST be noted in the DIVERGENCE block at the top of `internal/jail/jail.go`. Deps for that package stay stdlib + `golang.org/x/sys` only.
- Actors keep TYPED command channels (no `chan func()`); no actor changes are planned here, but any incidental actor edit follows that rule.
- Isolation/worktree integration tests AUTO-SKIP (never fail) on hosts without the capability: userns tests gate on `jail.CheckUnsharePreflight().OK()`, Landlock tests on the ABI probe, git tests on `exec.LookPath("git")`. CI (GitHub runners) restricts userns — jail tests skip there; Landlock tests DO run on ubuntu-latest.
- New dependency allowed: `github.com/landlock-lsm/go-landlock` only. `golang.org/x/sys` may move from indirect to direct.
- Worker logs live at `<workdir>/logs/<key>.worker.log` in ALL modes (Plan-2 shipped layout; worktrees-compatible because logs must survive worktree removal). Within a task, attempts APPEND to one log file (ringer.py parity: unlink once per task, then open `"ab"` per attempt).
- `active-runs.json` keeps Python parity; `runs/<id>.json` is Go-authoritative (§9.4 adjudication 2026-07-09). Nothing in this plan changes either schema.
- Python reference lines cited as `ringer.py:NNN` refer to the copy at the repo root; port behavior, not style.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/config/config.go` | +`JailRoBinds` engine key; `ExpandUser` exported (was `expandHome`) |
| `internal/runner/worker.go` | `WorkerOutcome.Canceled`, append-mode log, `extraEnv` param |
| `internal/runner/runner.go` | cancel path in `runTask`, interrupted-run error, failure-context cap, isolator wiring, worktree prepare/cleanup call sites |
| `internal/runner/worktrees.go` (new) | `prepareTaskDir`, `cleanupWorktreeOnPass`, report snapshot |
| `internal/state/state.go` | `pidAlive` pid<=0 guard |
| `internal/lint/lint.go` | W6 write-collision + two worktree rules |
| `internal/manifest/manifest.go` | worktrees validation: require repo, reserved `logs` dir, key-escape |
| `internal/jail/unshare.go` | `SetChdir` (cwd seam fix; noted divergence) |
| `internal/isolate/isolate.go` (new) | `Isolator`, `WrapSpec`, `Wrapped` |
| `internal/isolate/jail.go` (new) | `JailIsolator` |
| `internal/isolate/landlock.go` (new) | `LandlockIsolator` (portable part) |
| `internal/isolate/landlock_probe_linux.go` / `_other.go` (new) | `LandlockABI()` probe |
| `internal/isolate/select.go` (new) | `Select`: jail > Landlock > refuse |
| `cmd/ringer/run.go` | `signal.NotifyContext`, Checkpoint caller, isolator selection |
| `cmd/ringer/demo.go` | pass the signal context through |
| `cmd/ringer/landlockexec.go` (new, linux-only) | hidden `landlock-exec` trampoline |
| `internal/engine/engine.go` | Preflight: drop the "jail lands in Plan 3" error |
| `config.sample.toml` | isolation key examples |
| `docs/superpowers/specs/2026-07-08-ringer-go-rewrite-design.md` | §6 addendum: `jail_ro_binds` |

Task order: config first (everything reads it), then the worker/signal seam, then the small carry-forwards, then lint/manifest/worktrees (pure-Go, no kernel deps), then jail/isolate/Landlock, and finally the Select chain + end-to-end wiring.

---

### Task 1: Config — `jail_ro_binds` key, `ExpandUser`, sample config, spec addendum

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`
- Modify: `config.sample.toml`
- Modify: `docs/superpowers/specs/2026-07-08-ringer-go-rewrite-design.md` (§6, one sentence)

**Interfaces:**
- Consumes: existing `EngineConfig` (`internal/config/config.go:23-32`).
- Produces: `EngineConfig.JailRoBinds []string` (toml `jail_ro_binds`) — read by Task 8/9 isolators via `WrapSpec.ROBinds`. `config.ExpandUser(p string) string` — exported rename of the existing `expandHome`; used by Tasks 5, 8, 9.

**Why:** spec §6 gives jailed engines rw `jail_state_dirs`, but an engine installed under `$HOME` (the real opencode lives at `~/.opencode`) also needs its install tree visible read-only inside the sandbox. `jail_ro_binds` is that key. `ExpandUser` gets exported because three packages (config, lint, isolate) otherwise each grow a private copy of the same 6 lines.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
func TestEngineJailKeysDecode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[engines.opencode]
bin = "opencode"
args_template = ["run", "{spec}"]
isolation = "jail"
jail_state_dirs = ["~/.config/opencode", "~/.local/share/opencode"]
jail_ro_binds = ["~/.opencode"]
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	e := c.Engines["opencode"]
	if e.Isolation != "jail" {
		t.Fatalf("Isolation = %q, want jail", e.Isolation)
	}
	if len(e.JailStateDirs) != 2 || len(e.JailRoBinds) != 1 || e.JailRoBinds[0] != "~/.opencode" {
		t.Fatalf("jail dirs = %v / %v", e.JailStateDirs, e.JailRoBinds)
	}
}

func TestExpandUser(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	cases := []struct{ in, want string }{
		{"~/x", filepath.Join(home, "x")},
		{"~", home},
		{"/abs/path", "/abs/path"},
		{"rel/path", "rel/path"},
	}
	for _, c := range cases {
		if got := ExpandUser(c.in); got != c.want {
			t.Errorf("ExpandUser(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — `e.JailRoBinds undefined` and `undefined: ExpandUser`.

- [ ] **Step 3: Implement**

In `internal/config/config.go`, add the field to `EngineConfig`:

```go
type EngineConfig struct {
	Bin            string   `toml:"bin"`
	ArgsTemplate   []string `toml:"args_template"`
	SandboxArgs    []string `toml:"sandbox_args"`
	FullAccessArgs []string `toml:"full_access_args"`
	TokenRegex     string   `toml:"token_regex"`
	ModelDefault   string   `toml:"model_default"`
	Isolation      string   `toml:"isolation"`       // "", "none", "jail"
	JailStateDirs  []string `toml:"jail_state_dirs"` // rw binds/rules inside the sandbox (engine state)
	JailRoBinds    []string `toml:"jail_ro_binds"`   // ro binds/rules (engine installs outside the host toolchain, e.g. ~/.opencode)
}
```

Rename `expandHome` → `ExpandUser` (same body, exported, doc comment) and update its two internal callers (`StateDirPath`, `DBPath`):

```go
// ExpandUser expands a leading "~" or "~/" to the current user's home
// directory, mirroring Python's Path.expanduser for the paths ringer's
// config and manifests carry. Non-tilde paths pass through unchanged.
func ExpandUser(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS (whole suite; the rename is mechanical, callers are in the same file).

- [ ] **Step 5: Sample config + spec addendum**

Append to the engines section of `config.sample.toml` (keep existing content; add commented example):

```toml
# Linux rootless isolation (spec §6). isolation = "none" (default) | "jail".
# With "jail" the engine runs inside a per-task user-namespace chroot:
# read-only host toolchain, taskdir read-write, tmpfs /tmp; if user
# namespaces are unavailable the run falls back to Landlock path rules,
# and refuses to run when neither is available.
#
# [engines.opencode]
# bin = "/home/you/.opencode/bin/opencode"
# args_template = ["run", ...]
# isolation = "jail"
# jail_state_dirs = ["~/.config/opencode", "~/.local/share/opencode"]  # read-write in the sandbox
# jail_ro_binds = ["~/.opencode"]  # read-only in the sandbox (engine installs outside /usr etc.)
```

In the design spec §6, after the TOML block's `jail_state_dirs` line, add:

```
jail_ro_binds = ["~/.opencode"]   # ro binds — engine installs living outside the host-toolchain mounts (Plan 3 addendum 2026-07-09)
```

- [ ] **Step 6: Commit**

```bash
git add internal/config config.sample.toml docs/superpowers/specs/2026-07-08-ringer-go-rewrite-design.md
git commit -m "feat(config): jail_ro_binds engine key + exported ExpandUser"
```

---

### Task 2: Worker seam — cancel-aware outcome, append-mode log, extra env

**Files:**
- Modify: `internal/runner/worker.go`
- Modify: `internal/runner/runner.go` (call site + per-task log unlink)
- Test: `internal/runner/worker_test.go`

**Interfaces:**
- Consumes: existing `runWorker(ctx, bin, argv, taskDir, logPath, w, timeout)` and `WorkerOutcome{ExitCode, TimedOut, Err}` (`internal/runner/worker.go`).
- Produces: `runWorker(ctx context.Context, bin string, argv []string, taskDir, logPath string, w io.Writer, timeout time.Duration, extraEnv []string) WorkerOutcome` and `WorkerOutcome{ExitCode int; TimedOut bool; Canceled bool; Err error}`. Task 3 branches on `Canceled`; Task 10 passes the isolator's `Env` as `extraEnv`.

**Why:** three seams the rest of the plan needs, all in the one function. (a) Parent-context cancellation currently mislabels as `TimedOut` — signal handling (Task 3) needs to tell "user interrupted" from "attempt timed out". (b) `os.Create` truncates the log on attempt 2, destroying attempt 1's output — ringer.py unlinks once per task and opens `"ab"` per attempt (ringer.py:7222, 7107), so both attempts accumulate in one file. (c) Isolators need to inject `TMPDIR`/`XDG_CACHE_HOME`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/runner/worker_test.go`:

```go
func TestRunWorkerCanceledNotTimedOut(t *testing.T) {
	sleepBin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("sleep not on PATH: %v", err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	out := runWorker(ctx, sleepBin, []string{"30"}, dir,
		filepath.Join(dir, "w.log"), io.Discard, 25*time.Second, nil)
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("cancel did not cut the worker short (elapsed %v)", elapsed)
	}
	if !out.Canceled {
		t.Fatalf("outcome = %+v, want Canceled=true", out)
	}
	if out.TimedOut {
		t.Fatalf("outcome = %+v: user cancellation must not be labeled a timeout", out)
	}
}

func TestRunWorkerLogAppendsAcrossAttempts(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "w.log")
	for _, msg := range []string{"attempt-one", "attempt-two"} {
		out := runWorker(context.Background(), "sh", []string{"-c", "echo " + msg},
			dir, logPath, io.Discard, 5*time.Second, nil)
		if out.Err != nil || out.ExitCode != 0 {
			t.Fatalf("worker failed: %+v", out)
		}
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "attempt-one") || !strings.Contains(string(data), "attempt-two") {
		t.Fatalf("log lost an attempt (ringer.py appends both):\n%s", data)
	}
}

func TestRunWorkerExtraEnv(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	out := runWorker(context.Background(), "sh", []string{"-c", "printf '%s' \"$RINGER_TEST_MARKER\""},
		dir, filepath.Join(dir, "w.log"), &buf, 5*time.Second,
		[]string{"RINGER_TEST_MARKER=isolated-env-ok"})
	if out.Err != nil || out.ExitCode != 0 {
		t.Fatalf("worker failed: %+v", out)
	}
	if got := buf.String(); got != "isolated-env-ok" {
		t.Fatalf("env not injected: got %q", got)
	}
}
```

Add any missing imports (`bytes`, `strings`, `io`, `context`, `os/exec`, `path/filepath`, `time`) to the test file's import block.

- [ ] **Step 2: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — compile errors: `too many arguments in call to runWorker`, `out.Canceled undefined`.

- [ ] **Step 3: Implement**

In `internal/runner/worker.go`:

```go
// WorkerOutcome reports how a worker process finished.
type WorkerOutcome struct {
	ExitCode int
	TimedOut bool
	Canceled bool // parent context canceled (user interrupt) — distinct from a per-attempt timeout
	Err      error
}
```

Change `runWorker`'s signature and the log-open and env lines; the doc comment gains the append + env sentences. Full updated function:

```go
// runWorker executes bin with argv in taskDir. Stdin is closed (backed by
// /dev/null); stdout and stderr are merged and teed to a log file at
// logPath and to the caller-supplied writer w (the caller composes w, e.g.
// via io.MultiWriter, to also forward output to a collector sink). The log
// file is opened in APPEND mode so a retry accumulates onto the same file
// (ringer.py parity: unlink once per task, append per attempt) — the caller
// owns removing a stale log before the first attempt. extraEnv entries
// (KEY=VALUE) are appended to the inherited environment; nil means inherit
// unchanged. The process runs in its own process group (Setpgid) so that on
// timeout or cancellation the whole group can be signaled: SIGTERM first,
// then SIGKILL after a 5s grace period if it hasn't exited. Cancellation of
// the parent ctx is reported as Canceled (not TimedOut). cmd.Wait() joins
// os/exec's internal copy goroutines, so once it returns all writes to w
// have completed.
func runWorker(ctx context.Context, bin string, argv []string, taskDir, logPath string, w io.Writer, timeout time.Duration, extraEnv []string) WorkerOutcome {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return WorkerOutcome{Err: err}
	}
	defer devNull.Close()

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return WorkerOutcome{Err: err}
	}
	defer logFile.Close()

	mw := io.MultiWriter(logFile, w)

	cmd := exec.Command(bin, argv...)
	cmd.Dir = taskDir
	cmd.Stdin = devNull
	cmd.Stdout = mw
	cmd.Stderr = mw
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}

	if err := cmd.Start(); err != nil {
		return WorkerOutcome{Err: err}
	}
	pgid := cmd.Process.Pid

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	var timedOut, canceled bool
	var waitErr error
	select {
	case waitErr = <-waitDone:
	case <-timeoutCtx.Done():
		// The child may have exited at essentially the same instant the
		// timeout fired, in which case waitDone is already buffered even
		// though this branch of the select was chosen. Recheck
		// non-blockingly before signaling: if the process is already
		// reaped, sending to -pgid could hit a recycled process group
		// instead of a no-op, and the outcome must not be mislabeled as
		// timed out.
		select {
		case waitErr = <-waitDone:
		default:
			// Same group-kill machinery either way; the label depends on
			// WHY timeoutCtx fired. Parent cancellation (user interrupt)
			// wins the label when both are pending.
			if ctx.Err() != nil {
				canceled = true
			} else {
				timedOut = true
			}
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
			select {
			case waitErr = <-waitDone:
			case <-time.After(termGrace):
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
				waitErr = <-waitDone
			}
		}
	}

	outcome := WorkerOutcome{TimedOut: timedOut, Canceled: canceled}
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			outcome.ExitCode = exitErr.ExitCode()
		} else {
			outcome.Err = waitErr
		}
	}
	return outcome
}
```

In `internal/runner/runner.go` `runTask`, right after `logPath := ...` add the once-per-task unlink, and pass `nil` at the call site:

```go
	logPath := filepath.Join(logsDir, task.Key+".worker.log")
	// Append-mode log (ringer.py:7107): clear once per task so a rerun in
	// the same workdir starts fresh, then both attempts accumulate.
	if err := os.Remove(logPath); err != nil && !os.IsNotExist(err) {
		lg.Warnf("task %s: remove stale worker log: %v", task.Key, err)
	}
```

```go
		outcome := runWorker(ctx, bin, argv, taskDir, logPath, w, timeout, nil)
```

Update every existing `runWorker(` call in `internal/runner/worker_test.go` to pass a trailing `nil`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS, including the three new tests. `TestRunWorkerCanceledNotTimedOut` should complete in well under a second (SIGTERM kills `sleep` instantly).

- [ ] **Step 5: Commit**

```bash
git add internal/runner
git commit -m "feat(runner): cancel-aware worker outcome, append-mode logs, extraEnv seam"
```

---

### Task 3: Signal handling — Ctrl-C tears the run down cleanly

**Files:**
- Modify: `internal/runner/runner.go`
- Modify: `cmd/ringer/run.go`
- Modify: `cmd/ringer/demo.go`
- Test: `internal/runner/runner_test.go`

**Interfaces:**
- Consumes: `WorkerOutcome.Canceled` (Task 2); existing `runManifestFile(manifestPath string, maxParallelOverride int, identityFlag string, dryRun bool) error` (`cmd/ringer/run.go:40`); existing `Run(ctx, opts)`.
- Produces: `runManifestFile(ctx context.Context, manifestPath string, maxParallelOverride int, identityFlag string, dryRun bool) error`; `signalContext() (context.Context, context.CancelFunc)` in `cmd/ringer/run.go` (used by run + demo); `Run` returns a wrapped `context.Canceled` error when interrupted.

**Why (carry-forward, Plan 2 final review):** Ctrl-C currently kills only the `ringer` process: Setpgid workers are orphaned alive, the run-state file is left `done:false` forever, and `active-runs.json` keeps a dead entry until the next prune. The kill machinery already exists in `runWorker` — this task wires a cancelable context to it and makes the teardown path (final state write, active-runs unregister, actor stops) run on the way out.

**Behavior contract:** first Ctrl-C → context cancels → every running worker group gets SIGTERM (→SIGKILL after grace), no retries start, `writeState(true)` lands, active-runs unregisters, process exits non-zero with a "run interrupted" error. Second Ctrl-C → default signal disposition (immediate death) because the handler has been unregistered by then.

- [ ] **Step 1: Write the failing test**

Append to `internal/runner/runner_test.go`:

```go
func TestRunInterruptedTearsDownCleanly(t *testing.T) {
	sleepBin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("sleep not on PATH: %v", err)
	}
	workdir := t.TempDir()
	stateDir := t.TempDir()
	m := &manifest.Manifest{
		RunName: "interrupt-e2e",
		Workdir: workdir,
		Tasks: []manifest.Task{
			{Key: "snoozer", Spec: "sleep", Check: "true", Engine: "snooze", TimeoutS: 60},
		},
	}
	engines := map[string]config.EngineConfig{
		"snooze": {Bin: sleepBin, ArgsTemplate: []string{"30"}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	res, err := Run(ctx, Options{
		Manifest: m, Engines: engines, StateDir: stateDir,
		Identity: "test", Stdout: io.Discard, Logger: logging.Default(),
	})
	if time.Since(start) > 15*time.Second {
		t.Fatal("interrupt did not cut the run short")
	}
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want wrapped context.Canceled", err)
	}
	if len(res.Results) != 1 || res.Results[0].Verdict == "PASS" {
		t.Fatalf("results = %+v, want one non-PASS result", res.Results)
	}
	if res.Results[0].Attempts != 1 {
		t.Fatalf("attempts = %d: an interrupted task must not retry", res.Results[0].Attempts)
	}
	// The final state file must be flushed with done:true — a run killed by
	// Ctrl-C must not read as still-running forever.
	data, err := os.ReadFile(filepath.Join(stateDir, "runs", res.RunID+".json"))
	if err != nil {
		t.Fatalf("run-state file: %v", err)
	}
	var s state.RunState
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if !s.Done {
		t.Fatalf("run-state done = false, want true after interrupt")
	}
	// active-runs.json must not still list this run.
	active, err := state.ReadActiveRuns(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := active[res.RunID]; ok {
		t.Fatal("active-runs.json still lists the interrupted run")
	}
}
```

Add missing imports to the test file (`errors`, `encoding/json`, `os/exec`, `github.com/corruptmemory/ringer/internal/state`, etc. — check the existing block first).

- [ ] **Step 2: Run test to verify it fails**

Run: `./build.sh --test`
Expected: FAIL — `Run` returns `err = nil` today (`want wrapped context.Canceled`), and the task retries (attempts = 2) because nothing breaks the loop on cancellation.

- [ ] **Step 3: Implement the runner side**

In `internal/runner/runner.go` `runTask`, immediately after the spawn-error check inside the attempt loop:

```go
		outcome := runWorker(ctx, bin, argv, taskDir, logPath, w, timeout, nil)
		if outcome.Err != nil {
			lg.Errorf("task %s: spawn error: %v", task.Key, outcome.Err)
		}
		if outcome.Canceled || ctx.Err() != nil {
			// User interrupt: no verify, no eval row (nothing meaningful
			// ran to completion), no retry — mirror Python, where Ctrl-C
			// aborts before _log_attempt. The actor still records the
			// final status below so the last state flush is truthful.
			lg.Warnf("task %s: interrupted", task.Key)
			verdict = "ERROR"
			break
		}
```

In `Run`, after the final `writeState(true)` and result assembly, return the interruption (keep the existing `return res, nil` for the normal path):

```go
	if ctxErr := ctx.Err(); ctxErr != nil {
		// Teardown already ran: final state flushed (done:true), actor and
		// collector stopping via defers, active-runs unregistering via
		// defer. Surface the interruption so the CLI exits non-zero.
		return res, fmt.Errorf("run %s interrupted: %w", runID, ctxErr)
	}
	return res, nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `./build.sh --test`
Expected: PASS. Note: the test takes ~0.3s + SIGTERM latency, NOT 30s.

- [ ] **Step 5: Wire the CLI boundary**

In `cmd/ringer/run.go`:

```go
// signalContext returns a context canceled by the first SIGINT/SIGTERM.
// After that first signal the handler unregisters itself, so a second
// Ctrl-C falls back to default disposition and kills the process
// immediately — graceful teardown must never trap an impatient user.
func signalContext() (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	return ctx, stop
}
```

Change `runCmd.Execute` and `runManifestFile`:

```go
func (c *runCmd) Execute(args []string) error {
	ctx, stop := signalContext()
	defer stop()
	return runManifestFile(ctx, c.Args.Manifest, c.MaxParallel, c.Identity, c.DryRun)
}
```

```go
func runManifestFile(ctx context.Context, manifestPath string, maxParallelOverride int, identityFlag string, dryRun bool) error {
```

and replace `context.Background()` in the `runner.Run` call with `ctx`. Add `"os/signal"` and `"syscall"` imports (and drop the now-unneeded direct `context.Background()` usage).

In `cmd/ringer/demo.go`, `demoCmd.Execute` currently ends with `return runManifestFile(manifestPath, c.MaxParallel, c.Identity, c.DryRun)`; make it:

```go
	ctx, stop := signalContext()
	defer stop()
	return runManifestFile(ctx, manifestPath, c.MaxParallel, c.Identity, c.DryRun)
```

- [ ] **Step 6: Run tests + manual smoke**

Run: `./build.sh --test`
Expected: PASS.

Manual smoke (optional but cheap): `./ringer demo` in one terminal, Ctrl-C mid-run → prompt returns quickly, exit code non-zero, `ps` shows no orphaned mock workers.

- [ ] **Step 7: Commit**

```bash
git add internal/runner cmd/ringer
git commit -m "feat: Ctrl-C cancels the run cleanly (NotifyContext -> group kill -> final state flush)"
```

---

### Task 4: Carry-forward minors — pidAlive guard, failure-context cap, Checkpoint run-end caller

**Files:**
- Modify: `internal/state/state.go:72-78`
- Modify: `internal/runner/runner.go` (retry spec construction)
- Modify: `cmd/ringer/run.go` (after `runner.Run`)
- Test: `internal/state/state_test.go`, `internal/runner/runner_test.go`

**Interfaces:**
- Consumes: `pidAlive` (unexported, `internal/state/state.go:72`), `Store.Checkpoint()` (`internal/store/store.go:138`).
- Produces: `capTail(s string, max int) string` (unexported, `internal/runner/runner.go`) and `const failureContextMax = 6000`. No signature changes elsewhere.

**Why (three deferred Plan-2 review findings):** (a) `syscall.Kill(0, 0)` probes the caller's own process group and `Kill(-1, 0)` probes *all* processes — both "succeed", so a corrupt `active-runs.json` entry with pid 0 would look alive forever and never prune. (b) The retry spec appends the FULL check output; a pathological check can blow past Linux's per-argument `MAX_ARG_STRLEN` (~128KiB) and make the retry spawn fail with E2BIG. ringer.py caps failure context at its last 6000 chars (ringer.py:7671) — port the cap (bytes, not runes; a cut can land mid-rune, which is acceptable for prompt text and mirrors the byte-vs-rune stance already noted for lint thresholds). (c) `Store.Checkpoint` (WAL TRUNCATE, cznic #179) was fixed in Plan 2 but nothing calls it at run end.

- [ ] **Step 1: Write the failing tests**

Append to `internal/state/state_test.go`:

```go
func TestPruneDropsNonPositivePIDs(t *testing.T) {
	stateDir := t.TempDir()
	// Register a run, then corrupt its PID to 0 on disk — the shared file is
	// written by two eras of code, so defensive reading is part of the contract.
	if err := RegisterActiveRun(stateDir, "run-zero", "id", "name", "/wd", os.Getpid(), "2026-07-09T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, "active-runs.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	corrupted := strings.Replace(string(data), fmt.Sprintf(`"pid": %d`, os.Getpid()), `"pid": 0`, 1)
	if corrupted == string(data) {
		t.Fatal("test setup: pid substitution did not take")
	}
	if err := os.WriteFile(path, []byte(corrupted), 0o644); err != nil {
		t.Fatal(err)
	}
	runs, err := ReadActiveRuns(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := runs["run-zero"]; ok {
		t.Fatal("pid=0 entry survived pruning; kill(0,0) probes our own process group, not a process")
	}
}
```

Append to `internal/runner/runner_test.go`:

```go
func TestCapTail(t *testing.T) {
	if got := capTail("short", 6000); got != "short" {
		t.Fatalf("short input mangled: %q", got)
	}
	long := strings.Repeat("x", 7000) + "TAIL-MARKER"
	got := capTail(long, 6000)
	if len(got) != 6000 {
		t.Fatalf("len = %d, want 6000", len(got))
	}
	if !strings.HasSuffix(got, "TAIL-MARKER") {
		t.Fatal("cap must keep the TAIL (most recent output is the actionable part)")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — `undefined: capTail`; the prune test fails because the pid=0 entry survives.

- [ ] **Step 3: Implement**

`internal/state/state.go` — guard `pidAlive`:

```go
func pidAlive(pid int) bool {
	if pid <= 0 {
		// kill(0, 0) probes the caller's own process group and kill(-1, 0)
		// probes every process we may signal — both "exist", so a zero or
		// negative pid from a corrupt entry would otherwise never prune.
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM) // exists but not ours
}
```

`internal/runner/runner.go` — add near `defaultTimeoutS`:

```go
// failureContextMax caps the check output appended to a retry spec, in
// bytes; mirrors ringer.py build_failure_context's 6000-char cap
// (ringer.py:7671). The spec travels as ONE argv element, and Linux caps a
// single argument at MAX_ARG_STRLEN (~128KiB) — an uncapped check output
// would make the retry spawn fail with E2BIG.
const failureContextMax = 6000

// capTail returns at most max trailing bytes of s — the most recent output
// is the actionable part of a failure log.
func capTail(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}
```

and use it in the retry branch of `runTask`:

```go
			spec = task.Spec + "\n\n--- Previous attempt failed. Check output:\n" + capTail(vres.Output, failureContextMax)
```

`cmd/ringer/run.go` — checkpoint the eval store once the run is over, success or not (a failed run wrote rows too), before the error check on `runner.Run`'s return:

```go
	res, err := runner.Run(ctx, runner.Options{
		Manifest: m, Engines: engines, StateDir: cfg.StateDirPath(),
		Identity: identity, Store: st, Stdout: os.Stdout, Logger: lg,
		MaxParallel: m.MaxParallel,
	})
	if st != nil {
		// Run-end WAL checkpoint (spec §7, cznic #179): without an explicit
		// TRUNCATE checkpoint the WAL grows without bound under modernc.
		// Non-fatal — the data is durable either way — but never silent.
		if cerr := st.Checkpoint(); cerr != nil {
			lg.Warnf("eval store checkpoint: %v", cerr)
		}
	}
	if err != nil {
		return err
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS. The Checkpoint caller is exercised by the existing demo/run E2E paths (store non-nil); its failure path is Warn-logged, matching `WriteRunState` handling.

- [ ] **Step 5: Commit**

```bash
git add internal/state internal/runner cmd/ringer
git commit -m "fix: pidAlive pid<=0 guard, 6000-byte retry-context cap, run-end WAL checkpoint"
```

---

### Task 5: Lint — write-collision rule (W6) + two worktree rules

**Files:**
- Modify: `internal/lint/lint.go`
- Test: `internal/lint/lint_test.go`

**Interfaces:**
- Consumes: `manifest.Manifest`/`manifest.Task`; `config.ExpandUser` (Task 1); existing `Finding{TaskKey, Rule, Message}` and rule-constant style (`internal/lint/lint.go:24-30`).
- Produces: rule constants `RuleWriteCollision = "write-collision"`, `RuleWorktreeDeliverable = "worktree-deliverable"`, `RuleWorktreeCommit = "worktree-commit"`; helpers `anyRelativeExpectFile(paths []string) bool`, `instructsGitCommit(spec string) bool` (both unexported).

**Why:** ports the three remaining run-safety heuristics from ringer.py's `lint_manifest`. W6 (ringer.py:592-605): two tasks listing the same ABSOLUTE `expect_files` path silently overwrite each other (relative paths resolve inside each task's own dir and cannot collide) — skipped in worktrees mode, as upstream. The worktree pair (ringer.py:556-562): a relative deliverable dies with the worktree removed on PASS, and a spec instructing `git commit` loses the commit the same way. These two go live exactly when Tasks 6–7 make worktrees runnable, so they land first.

- [ ] **Step 1: Write the failing tests**

Append to `internal/lint/lint_test.go` (match the file's existing test style — table-driven against `Check` with rule filtering; add a small `findRule` helper if one doesn't already exist):

```go
func rulesOf(findings []Finding, rule string) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

func TestWriteCollisionRule(t *testing.T) {
	longSpec := strings.Repeat("write the thing carefully with lots of detail ", 4)
	m := &manifest.Manifest{
		RunName: "r", Workdir: "/w",
		Tasks: []manifest.Task{
			{Key: "a", Spec: longSpec, Check: "test -f /shared/out.md", ExpectFiles: []string{"/shared/out.md", "local.md"}},
			{Key: "b", Spec: longSpec, Check: "test -f /shared/out.md", ExpectFiles: []string{"/shared/out.md"}},
			{Key: "c", Spec: longSpec, Check: "test -f local.md", ExpectFiles: []string{"local.md"}},
		},
	}
	got := rulesOf(Check(m), RuleWriteCollision)
	if len(got) != 1 {
		t.Fatalf("findings = %+v, want exactly one collision", got)
	}
	msg := got[0].Message
	if !strings.Contains(msg, "/shared/out.md") || !strings.Contains(msg, "a, b") {
		t.Fatalf("message %q must name the path and both tasks", msg)
	}
	// Relative paths (task c + the locals) never collide: they resolve
	// inside each task's own directory.
	if strings.Contains(msg, "local.md") {
		t.Fatalf("relative path leaked into collision finding: %q", msg)
	}

	// Worktrees mode skips the rule entirely (upstream parity: taskdirs are
	// whole checkouts; the heuristic doesn't apply).
	m.Worktrees = true
	m.Repo = "/repo"
	if got := rulesOf(Check(m), RuleWriteCollision); len(got) != 0 {
		t.Fatalf("worktrees mode must skip write-collision, got %+v", got)
	}
}

func TestWorktreeDeliverableRule(t *testing.T) {
	longSpec := strings.Repeat("write the thing carefully with lots of detail ", 4)
	m := &manifest.Manifest{
		RunName: "r", Workdir: "/w", Worktrees: true, Repo: "/repo",
		Tasks: []manifest.Task{
			{Key: "rel", Spec: longSpec, Check: "true", ExpectFiles: []string{"out/report.md"}},
			{Key: "abs", Spec: longSpec, Check: "true", ExpectFiles: []string{"/exports/report.md"}},
			{Key: "home", Spec: longSpec, Check: "true", ExpectFiles: []string{"~/exports/report.md"}},
		},
	}
	got := rulesOf(Check(m), RuleWorktreeDeliverable)
	if len(got) != 1 || got[0].TaskKey != "rel" {
		t.Fatalf("findings = %+v, want exactly one for task rel", got)
	}
	m.Worktrees = false
	if got := rulesOf(Check(m), RuleWorktreeDeliverable); len(got) != 0 {
		t.Fatalf("non-worktrees mode must not fire, got %+v", got)
	}
}

func TestWorktreeCommitRule(t *testing.T) {
	pad := strings.Repeat("x", 80)
	cases := []struct {
		name string
		spec string
		want bool
	}{
		{"plain instruction", "Implement the feature, then git commit the result. " + pad, true},
		{"uppercase", "GIT COMMIT when done. " + pad, true},
		{"negated", "Fix the bug. Do not git commit. " + pad, false},
		{"negated contraction", "Fix it, but don't run `git commit`. " + pad, false},
		{"negated then instructed", "Don't git commit yet; later git commit the fix. " + pad, true},
		{"absent", "Just write the file. " + pad, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &manifest.Manifest{
				RunName: "r", Workdir: "/w", Worktrees: true, Repo: "/repo",
				Tasks: []manifest.Task{{Key: "t", Spec: c.spec, Check: "true"}},
			}
			got := rulesOf(Check(m), RuleWorktreeCommit)
			if (len(got) == 1) != c.want {
				t.Fatalf("spec %q: findings %+v, want fired=%v", c.spec, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — `undefined: RuleWriteCollision` etc.

- [ ] **Step 3: Implement**

In `internal/lint/lint.go`, extend the constants block:

```go
const (
	RuleCheckCannotFail     = "check-cannot-fail"
	RuleCheckSilent         = "check-silent"
	RuleSpecUnderspecified  = "spec-underspecified"
	RuleSpecFilePointer     = "spec-file-pointer"
	RuleMissingExpectFiles  = "missing-expect-files"
	RuleWriteCollision      = "write-collision"
	RuleWorktreeDeliverable = "worktree-deliverable"
	RuleWorktreeCommit      = "worktree-commit"
)
```

Inside `Check`'s per-task loop, add the worktree pair (message text ported from ringer.py:556-562):

```go
		if m.Worktrees && anyRelativeExpectFile(t.ExpectFiles) {
			findings = append(findings, Finding{
				TaskKey: t.Key,
				Rule:    RuleWorktreeDeliverable,
				Message: "deliverable would be deleted with the worktree; write it outside the worktree or export it in the check.",
			})
		}
		if m.Worktrees && instructsGitCommit(t.Spec) {
			findings = append(findings, Finding{
				TaskKey: t.Key,
				Rule:    RuleWorktreeCommit,
				Message: "worker commits die with the worktree; have the worker leave changes uncommitted and export the diff in the check.",
			})
		}
```

After the per-task loop, add the manifest-level collision scan (ringer.py:592-605), sorted for deterministic output:

```go
	if !m.Worktrees {
		// Relative expect_files resolve inside each task's own directory and
		// cannot collide; only a shared absolute path is a real collision.
		pathsToTasks := map[string][]string{}
		for _, t := range m.Tasks {
			for _, p := range t.ExpectFiles {
				if expanded := config.ExpandUser(p); filepath.IsAbs(expanded) {
					pathsToTasks[expanded] = append(pathsToTasks[expanded], t.Key)
				}
			}
		}
		paths := make([]string, 0, len(pathsToTasks))
		for p := range pathsToTasks {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			if keys := pathsToTasks[p]; len(keys) >= 2 {
				findings = append(findings, Finding{
					Rule:    RuleWriteCollision,
					Message: fmt.Sprintf("write collision on %s: listed by %s.", p, strings.Join(keys, ", ")),
				})
			}
		}
	}
```

Add the helpers at the bottom of the file (behavior ported from ringer.py:768-792):

```go
// anyRelativeExpectFile reports whether any declared deliverable is a
// relative path — in worktrees mode those live inside the checkout and are
// destroyed with it on PASS. "~"-prefixed paths count as absolute, matching
// Python's expanduser-then-is_absolute.
func anyRelativeExpectFile(paths []string) bool {
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if !strings.HasPrefix(p, "~") && !filepath.IsAbs(p) {
			return true
		}
	}
	return false
}

// negatedGitCommitRe matches a "do not / don't / never / no [run]" phrase
// ENDING immediately before a "git commit" occurrence (ringer.py:785-792).
var negatedGitCommitRe = regexp.MustCompile(
	`(?:do\s+not|don't|never|no)[\s` + "`" + `'"()\[\]{}:;,.!?-]*(?:run[\s` + "`" + `'"()\[\]{}:;,.!?-]*)?$`)

// instructsGitCommit reports whether the spec tells the worker to run
// `git commit`, ignoring occurrences negated within the preceding 48
// characters (ringer.py:772-782).
func instructsGitCommit(spec string) bool {
	lower := strings.ToLower(spec)
	start := 0
	for {
		idx := strings.Index(lower[start:], "git commit")
		if idx == -1 {
			return false
		}
		idx += start
		from := idx - 48
		if from < 0 {
			from = 0
		}
		if !negatedGitCommitRe.MatchString(lower[from:idx]) {
			return true
		}
		start = idx + len("git commit")
	}
}
```

Add imports as needed: `fmt`, `path/filepath`, `sort`, and `github.com/corruptmemory/ringer/internal/config`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS, including all three new tests and the existing lint table.

- [ ] **Step 5: Commit**

```bash
git add internal/lint
git commit -m "feat(lint): write-collision (W6) + worktree deliverable/commit rules"
```

---

### Task 6: Manifest — worktrees validation goes live

**Files:**
- Modify: `internal/manifest/manifest.go`
- Test: `internal/manifest/manifest_test.go`

**Interfaces:**
- Consumes: existing `FromBytes` validation shape (joined errors).
- Produces: manifests with `worktrees: true` VALIDATE (the "lands in Plan 3" rejection is removed); new rejections: worktrees without `repo`; any task key that lexically escapes the workdir; any task key colliding with the reserved `logs` directory.

**Why:** three guards move from Python runtime checks to Go load-time validation. (a) `worktrees` without `repo` in Python silently degrades to plain directories (ringer.py:6987 guards on `repo is not None`) — a silent semantic downgrade; Go fails loud instead (deliberate divergence). (b) The taskdir-escape check (ringer.py:7231-7236, raised at runtime) becomes a load-time error: `key: "../evil"` must never produce a taskdir outside workdir. (c) The reserved-`logs` collision (ringer.py:504-515, worktrees-only in Python) applies in ALL modes here because Go's log layout is always `<workdir>/logs/` (Plan-2 shipped layout — a `logs` task key would have its taskdir shadow the log directory today; stricter than Python, deliberately).

- [ ] **Step 1: Write the failing tests**

Append to `internal/manifest/manifest_test.go` (follow the file's existing FromBytes-error test style):

```go
func TestWorktreesValidation(t *testing.T) {
	base := func(extra string) []byte {
		return []byte(`{
			"run_name": "wt", "workdir": "/tmp/wt-work", ` + extra + `
			"tasks": [{"key": "t1", "spec": "do the thing", "check": "true"}]
		}`)
	}
	// worktrees + repo: valid now (Plan 3).
	if _, err := FromBytes(base(`"worktrees": true, "repo": "/tmp/parent-repo",`)); err != nil {
		t.Fatalf("worktrees+repo must validate in Plan 3: %v", err)
	}
	// worktrees without repo: loud failure, not Python's silent downgrade.
	if _, err := FromBytes(base(`"worktrees": true,`)); err == nil || !strings.Contains(err.Error(), "repo") {
		t.Fatalf("worktrees without repo: err = %v, want repo requirement", err)
	}
}

func TestTaskKeyEscapesWorkdir(t *testing.T) {
	body := []byte(`{
		"run_name": "esc", "workdir": "/tmp/esc-work",
		"tasks": [{"key": "../evil", "spec": "do the thing", "check": "true"}]
	}`)
	if _, err := FromBytes(body); err == nil || !strings.Contains(err.Error(), "escapes workdir") {
		t.Fatalf("err = %v, want escape rejection", err)
	}
}

func TestTaskKeyReservedLogsDir(t *testing.T) {
	for _, key := range []string{"logs", "logs/nested"} {
		body := []byte(`{
			"run_name": "logs-clash", "workdir": "/tmp/lg-work",
			"tasks": [{"key": "` + key + `", "spec": "do the thing", "check": "true"}]
		}`)
		if _, err := FromBytes(body); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("key %q: err = %v, want reserved-logs rejection", key, err)
		}
	}
	// "logs-report" merely shares the prefix — must stay valid.
	body := []byte(`{
		"run_name": "ok", "workdir": "/tmp/ok-work",
		"tasks": [{"key": "logs-report", "spec": "do the thing", "check": "true"}]
	}`)
	if _, err := FromBytes(body); err != nil {
		t.Fatalf("logs-report is not reserved: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — worktrees manifests are still rejected wholesale; `../evil` and `logs` keys are currently accepted.

- [ ] **Step 3: Implement**

In `internal/manifest/manifest.go` `FromBytes`, replace the worktrees rejection:

```go
	if m.Worktrees && m.Repo == "" {
		// ringer.py silently falls back to plain directories when repo is
		// missing (worktree ops guard on `repo is not None`) — a silent
		// semantic downgrade. Fail loud instead: deliberate divergence.
		errs = append(errs, errors.New("worktrees mode requires repo (the parent repository each task worktree is checked out from)"))
	}
```

Inside the per-task loop (after the duplicate-key handling, where `tk.Key != ""`), add:

```go
			// The key becomes the taskdir path component: it must stay
			// inside workdir (ringer.py:7231-7236, moved to load time) and
			// must not shadow the reserved <workdir>/logs directory (Go
			// always writes worker logs there; stricter than Python, which
			// reserves it only in worktrees mode).
			rel, relErr := filepath.Rel(m.Workdir, filepath.Join(m.Workdir, tk.Key))
			if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				errs = append(errs, fmt.Errorf("task key escapes workdir: %s", tk.Key))
			} else if rel == "logs" || strings.HasPrefix(rel, "logs"+string(filepath.Separator)) {
				errs = append(errs, fmt.Errorf("task key %q collides with the reserved logs directory", tk.Key))
			}
```

Add `path/filepath` and `strings` to the imports.

Also DELETE the now-stale rejection case in `internal/manifest/manifest_test.go`'s error table (`{"worktrees", `+"`"+`{"run_name":"r","workdir":"/x","worktrees":true,...}`+"`"+`, "Plan 3"}` at manifest_test.go:30) — worktrees-with-repo is valid now, and worktrees-WITHOUT-repo is covered by the new `TestWorktreesValidation`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS. NOTE: `runner.Run` still returns "worktrees mode lands in Plan 3" for worktrees manifests until Task 7 — manifests validate, execution follows next task.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest
git commit -m "feat(manifest): worktrees validation — require repo, key-escape + reserved-logs guards"
```

---

### Task 7: Runner — worktrees mode end-to-end

**Files:**
- Create: `internal/runner/worktrees.go`
- Modify: `internal/runner/runner.go`
- Test: `internal/runner/worktrees_test.go`

**Interfaces:**
- Consumes: `manifest.Manifest{Worktrees, Repo}` (validated by Task 6); `logging.Logger`.
- Produces: `prepareTaskDir(m *manifest.Manifest, taskDir string) error` and `cleanupWorktreeOnPass(m *manifest.Manifest, lg logging.Logger, taskKey, taskDir, logsDir string)` (both unexported, called from `runTask`); Task 10 relies on worktrees mode working to add the jail interplay (`WrapSpec.RepoRO`).

**Why:** worktrees mode gives each task a full `git worktree add` checkout of the repo's HEAD as its taskdir, and removes it on PASS (spec §9.4 log-location contract; ringer.py:6987-7031). Before removal, `report.md`/`report.html` are snapshotted next to the worker log (ringer.py:7033-7056, `TASK_REPORT_FILENAMES`) so Plan 4's artifacts can still render them. Failures keep the worktree for debugging. Worker logs already live at `<workdir>/logs/` (Plan-2 layout), which is exactly why they survive removal.

- [ ] **Step 1: Write the failing E2E test**

Create `internal/runner/worktrees_test.go`:

```go
package runner

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/manifest"
)

// gitFixtureRepo creates a repo with one commit containing seed.txt.
func gitFixtureRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "parent-repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "seed.txt"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo
}

func TestWorktreesModeEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	ringerBin := buildRingerBinary(t)
	repo := gitFixtureRepo(t)
	workdir := filepath.Join(t.TempDir(), "wt-work")

	m := &manifest.Manifest{
		RunName: "wt-e2e", Workdir: workdir, Worktrees: true, Repo: repo,
		Tasks: []manifest.Task{
			// Passing task: leaves a report for the snapshot, deliverable
			// visible to the check while the worktree still exists. NOTE
			// the frozen grammar (§9.9): "MOCK_FILE: <path>" with colon.
			{Key: "passer", Engine: "mock", TimeoutS: 30,
				Spec:  "MOCK_FILE: report.md\nwt report body\nMOCK_END\nMOCK_FILE: out.txt\ndone\nMOCK_END",
				Check: "test -f out.txt && test -f seed.txt"},
			// Failing task: worktree must survive for debugging. MOCK_FAIL
			// must be an exact line (the grammar matches the whole line).
			{Key: "failer", Engine: "mock", TimeoutS: 30,
				Spec:  "MOCK_FAIL",
				Check: "true"},
		},
	}
	engines := map[string]config.EngineConfig{
		"mock": {Bin: ringerBin, ArgsTemplate: []string{"mock-worker", "{spec}"}},
	}
	res, err := Run(context.Background(), Options{
		Manifest: m, Engines: engines, StateDir: t.TempDir(),
		Identity: "test", Stdout: io.Discard, Logger: logging.Default(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	verdicts := map[string]string{}
	for _, r := range res.Results {
		verdicts[r.Key] = r.Verdict
	}
	// The check ran INSIDE the worktree: seed.txt (from the repo commit)
	// and out.txt (from the worker) were both present.
	if verdicts["passer"] != "PASS" {
		t.Fatalf("passer = %q, want PASS (check saw repo checkout + worker output)", verdicts["passer"])
	}
	// PASS ⇒ worktree removed…
	if _, err := os.Stat(filepath.Join(workdir, "passer")); !os.IsNotExist(err) {
		t.Fatalf("passer worktree not removed on PASS (stat err = %v)", err)
	}
	// …report snapshotted next to the log first…
	snap := filepath.Join(workdir, "logs", "passer.worker.reports", "report.md")
	if _, err := os.Stat(snap); err != nil {
		t.Fatalf("report snapshot missing at %s: %v", snap, err)
	}
	// …and the worker log survives outside the worktree.
	if _, err := os.Stat(filepath.Join(workdir, "logs", "passer.worker.log")); err != nil {
		t.Fatalf("worker log missing: %v", err)
	}
	// FAIL ⇒ worktree kept for debugging.
	if _, err := os.Stat(filepath.Join(workdir, "failer")); err != nil {
		t.Fatalf("failer worktree must survive: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./build.sh --test`
Expected: FAIL — `Run` still returns `worktrees mode lands in Plan 3`.

- [ ] **Step 3: Implement**

Create `internal/runner/worktrees.go`:

```go
package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/manifest"
)

// taskReportFilenames mirrors ringer.py TASK_REPORT_FILENAMES: the report
// files snapshotted out of a worktree before it is removed on PASS.
var taskReportFilenames = []string{"report.md", "report.html"}

// prepareTaskDir creates the task's working directory. In worktrees mode
// the taskdir is a fresh `git worktree add` checkout of the manifest repo's
// HEAD (ringer.py:6987-7010); a pre-existing taskdir is an error there — a
// stale worktree would silently reuse another run's checkout. Default mode
// is a plain MkdirAll.
func prepareTaskDir(m *manifest.Manifest, taskDir string) error {
	if !m.Worktrees {
		return os.MkdirAll(taskDir, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(taskDir), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(taskDir); err == nil {
		return fmt.Errorf("worktree taskdir already exists: %s", taskDir)
	}
	out, err := exec.Command("git", "-C", m.Repo, "worktree", "add", taskDir, "HEAD").CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// cleanupWorktreeOnPass snapshots the task's report files out of the doomed
// worktree, then removes it (ringer.py:7012-7056). Failures are logged at
// Warn, never fatal: a leftover worktree is an inconvenience, not a broken
// run. No-op outside worktrees mode.
func cleanupWorktreeOnPass(m *manifest.Manifest, lg logging.Logger, taskKey, taskDir, logsDir string) {
	if !m.Worktrees {
		return
	}
	reportsDir := filepath.Join(logsDir, taskKey+".worker.reports")
	for _, name := range taskReportFilenames {
		src := filepath.Join(taskDir, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyFile(src, filepath.Join(reportsDir, name)); err != nil {
			lg.Warnf("task %s: report snapshot %s: %v", taskKey, name, err)
		}
	}
	out, err := exec.Command("git", "-C", m.Repo, "worktree", "remove", "--force", taskDir).CombinedOutput()
	if err != nil {
		lg.Warnf("task %s: git worktree remove: %v: %s", taskKey, err, strings.TrimSpace(string(out)))
	}
}

// copyFile is a whole-file copy for small report files.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
```

In `internal/runner/runner.go`:

1. Replace the `Run` guard (`if m.Worktrees { return … lands in Plan 3 }`) with defense-in-depth (manifest validation is the real gate):

```go
	if m.Worktrees && m.Repo == "" {
		return RunResult{}, fmt.Errorf("worktrees mode requires repo (manifest validation should have caught this)")
	}
```

2. In `runTask`, replace the bare `os.MkdirAll(taskDir, …)` block with:

```go
	taskDir := filepath.Join(opts.Manifest.Workdir, task.Key)
	if err := prepareTaskDir(opts.Manifest, taskDir); err != nil {
		lg.Errorf("task %s: prepare taskdir: %v", task.Key, err)
		a.setResult(task.Key, "failed", -1, task.Verified, "")
		return
	}
```

3. At the end of `runTask`, after the attempt loop and before `a.setResult`, add:

```go
	if verdict == "PASS" {
		cleanupWorktreeOnPass(opts.Manifest, lg, task.Key, taskDir, logsDir)
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `./build.sh --test`
Expected: PASS — both worktree assertions (removed on PASS, kept on FAIL) and the report snapshot.

- [ ] **Step 5: Commit**

```bash
git add internal/runner
git commit -m "feat(runner): worktrees mode — worktree-add taskdirs, report snapshot + removal on PASS"
```

---

### Task 8: Jail cwd seam (`SetChdir`) + `internal/isolate` with the jail backend

**Files:**
- Modify: `internal/jail/unshare.go`, `internal/jail/jail.go` (divergence note)
- Create: `internal/isolate/isolate.go`, `internal/isolate/jail.go`
- Test: `internal/jail/unshare_test.go`, `internal/isolate/jail_test.go`

**Interfaces:**
- Consumes: `jail.NewUnshareJail`, `jail.HostMounts`, `jail.TmpfsMount`, `jail.BindMount`, `jail.UnshareJail.{Setup,Script,UnshareArgs}` (`internal/jail/`); `config.ExpandUser` (Task 1).
- Produces:
  - `(*jail.UnshareJail).SetChdir(dir string)` — post-chroot working directory.
  - `isolate.WrapSpec{Key, Bin, Argv, TaskDir, StateDirs, ROBinds, RepoRO string/…}` and `isolate.Wrapped{Bin string; Argv []string; Env []string; Cleanup func() error}`.
  - `isolate.Isolator interface { Name() string; Wrap(WrapSpec) (Wrapped, error) }`.
  - `isolate.JailIsolator{Base string}` — Task 10 constructs it via `Select` and calls `Wrap` from `runTask`.

**Why:** two things. (a) The cwd seam (Plan-2 carry-forward): `chroot(1)` leaves the child's cwd at the new root — the §9.3 contract requires cwd = taskdir, and the Plan-1 spikes worked around it with `git -C /workspace`. `SetChdir` closes the seam inside the jail script. (b) The `Isolator` abstraction decouples the runner from HOW isolation happens: `Wrap` transforms `(bin, argv, taskdir)` and the §9.3 spawn path runs the result unchanged. The jail backend realizes spec §6's mount table; because the taskdir is bound at its HOST-IDENTICAL path, the argv `engine.BuildArgv` already produced (it embeds `{taskdir}`) works unmodified inside the namespace.

- [ ] **Step 1: Write the failing SetChdir tests**

Append to `internal/jail/unshare_test.go` (match its existing style — it has script-content unit tests and preflight-gated live tests):

```go
func TestScriptSetChdir(t *testing.T) {
	j := NewUnshareJail("/jail/root")
	j.SetChdir("/work/task-1")
	script := j.Script("/usr/bin/env", "FOO=1", "tool")
	want := "exec chroot '/jail/root' /bin/sh -c 'cd '\\''/work/task-1'\\'' && exec '\\''/usr/bin/env'\\'' '\\''FOO=1'\\'' '\\''tool'\\'''"
	if !strings.Contains(script, "cd ") || !strings.Contains(script, "/work/task-1") {
		t.Fatalf("script lacks the chdir wrapper:\n%s", script)
	}
	if !strings.Contains(script, want) {
		t.Fatalf("script chdir line mismatch.\nwant substring:\n%s\ngot script:\n%s", want, script)
	}
}

func TestScriptWithoutChdirUnchanged(t *testing.T) {
	j := NewUnshareJail("/jail/root")
	script := j.Script("tool", "arg")
	if strings.Contains(script, "/bin/sh -c") {
		t.Fatalf("no-chdir script must keep the direct exec form:\n%s", script)
	}
}

func TestLiveChdirLandsInTaskdir(t *testing.T) {
	if r := CheckUnsharePreflight(); !r.OK() {
		t.Skipf("userns preflight failed: %s", r.Error())
	}
	root := t.TempDir()
	taskDir := t.TempDir()
	j := NewUnshareJail(root)
	// tmpfs /tmp FIRST, then the taskdir bind underneath it (t.TempDir()
	// lives under /tmp — same ordering gotcha the Plan-1 worktree spike hit).
	mounts := append(HostMounts(root), TmpfsMount(filepath.Join(root, "tmp")))
	mounts = append(mounts, BindMount(taskDir, filepath.Join(root, taskDir), false))
	if err := j.Setup(mounts); err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer j.Teardown()
	j.SetChdir(taskDir)
	out, err := j.Command("pwd").CombinedOutput()
	if err != nil {
		t.Fatalf("pwd in jail: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != taskDir {
		t.Fatalf("cwd inside jail = %q, want %q (the §9.3 cwd seam)", got, taskDir)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — `j.SetChdir undefined`.

- [ ] **Step 3: Implement SetChdir**

In `internal/jail/unshare.go`, add the field and setter:

```go
type UnshareJail struct {
	root     string
	mounts   []Mount
	dropUser string // if set, use runuser -u <user> before exec
	chdir    string // if set, cd here (post-chroot path) before exec
}
```

```go
// SetChdir configures the working directory the jailed command starts in,
// as an in-jail (post-chroot) path. chroot(1) leaves the child's cwd at the
// new root ("/"); ringer's spawn contract requires cwd = taskdir, so the
// script wraps the final exec in `/bin/sh -c 'cd <dir> && exec …'` when
// this is set.
func (j *UnshareJail) SetChdir(dir string) {
	j.chdir = dir
}
```

In `buildScript`, replace the final exec block (the `cmdParts` assembly through the closing `else`) with:

```go
	cmdParts := []string{shellQuote(name)}
	for _, a := range args {
		cmdParts = append(cmdParts, shellQuote(a))
	}
	target := strings.Join(cmdParts, " ")
	if j.chdir != "" {
		// One bash word: the inner sh sees `cd '<dir>' && exec '<cmd>' …`.
		target = "/bin/sh -c " + shellQuote(fmt.Sprintf("cd %s && exec %s", shellQuote(j.chdir), strings.Join(cmdParts, " ")))
	}
	if j.dropUser != "" {
		// Make all PTY devices accessible before dropping privileges.
		// runuser changes to a UID that doesn't own the PTY in the namespace.
		fmt.Fprintf(&sb, "chmod 666 %s/dev/pts/* 2>/dev/null || true\n", shellQuote(root))
		fmt.Fprintf(&sb, "exec chroot %s runuser -u %s -- %s\n", shellQuote(root), j.dropUser, target)
	} else {
		fmt.Fprintf(&sb, "exec chroot %s %s\n", shellQuote(root), target)
	}
```

Append to the DIVERGENCE block in `internal/jail/jail.go`'s header comment:

```go
// DIVERGENCE from upstream flywheel (2026-07-09): UnshareJail gained
// SetChdir(dir) — ringer's spawn contract requires the jailed command to
// start in its taskdir, while chroot alone lands cwd at "/". The script
// wraps the exec in `/bin/sh -c 'cd <dir> && exec …'` when set.
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS (the live test auto-skips on hosts without userns).

- [ ] **Step 5: Commit the seam**

```bash
git add internal/jail
git commit -m "feat(jail): SetChdir — close the chroot-cwd seam (noted divergence)"
```

- [ ] **Step 6: Write the failing isolate tests**

Create `internal/isolate/jail_test.go`:

```go
package isolate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/jail"
)

func TestJailWrapShapesTheSpawn(t *testing.T) {
	base := t.TempDir()
	taskDir := t.TempDir()
	roBind := t.TempDir()
	iso := &JailIsolator{Base: base}
	w, err := iso.Wrap(WrapSpec{
		Key: "t1", Bin: "/usr/bin/tool", Argv: []string{"--flag", "value with spaces"},
		TaskDir: taskDir, ROBinds: []string{roBind}, RepoRO: "",
	})
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if w.Bin != "unshare" {
		t.Fatalf("Bin = %q, want unshare", w.Bin)
	}
	script := w.Argv[len(w.Argv)-1] // …, "--", "bash", "-c", script
	if w.Argv[len(w.Argv)-2] != "-c" || w.Argv[len(w.Argv)-3] != "bash" {
		t.Fatalf("argv tail not bash -c <script>: %v", w.Argv)
	}
	for _, wantSub := range []string{
		"mount --bind '" + taskDir + "'", // taskdir rw at host-identical path
		// The cd wrapper is nested inside the outer shell quoting, so the
		// raw script carries the escaped form. Exact quoting is pinned by
		// internal/jail's TestScriptSetChdir; here we just prove Wrap set it.
		"cd '\\''" + taskDir + "'\\''", // §9.3 cwd
		"'/usr/bin/tool'",
		"remount,bind,ro", // some ro remount present (toolchain + roBind)
	} {
		if !strings.Contains(script, wantSub) {
			t.Fatalf("script missing %q:\n%s", wantSub, script)
		}
	}
	wantEnv := map[string]bool{"TMPDIR=/tmp": false, "XDG_CACHE_HOME=/tmp": false}
	for _, e := range w.Env {
		if _, ok := wantEnv[e]; ok {
			wantEnv[e] = true
		}
	}
	for k, seen := range wantEnv {
		if !seen {
			t.Fatalf("Env missing %s: %v", k, w.Env)
		}
	}
	// Cleanup removes the per-task jail root.
	root := filepath.Join(base, "t1")
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("jail root not created: %v", err)
	}
	if err := w.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("jail root survived Cleanup (stat err = %v)", err)
	}
}

func TestJailWrapRejectsMissingROBind(t *testing.T) {
	iso := &JailIsolator{Base: t.TempDir()}
	_, err := iso.Wrap(WrapSpec{
		Key: "t1", Bin: "tool", TaskDir: t.TempDir(),
		ROBinds: []string{"/nonexistent/engine/install"},
	})
	if err == nil || !strings.Contains(err.Error(), "/nonexistent/engine/install") {
		t.Fatalf("err = %v, want loud failure naming the missing ro bind", err)
	}
}

func TestJailWrapLive(t *testing.T) {
	if r := jail.CheckUnsharePreflight(); !r.OK() {
		t.Skipf("userns preflight failed: %s", r.Error())
	}
	shBin, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh not on PATH: %v", err)
	}
	taskDir := t.TempDir()
	iso := &JailIsolator{Base: t.TempDir()}
	// The probe writes into its cwd (must land in taskDir on the host) and
	// reads a path that exists on the host but is NOT mounted in the jail
	// (must be invisible: default-deny reads).
	invisible := t.TempDir()
	if err := os.WriteFile(filepath.Join(invisible, "secret.txt"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	probe := "pwd && echo made-it > made.txt && (ls " + invisible + " 2>/dev/null && echo VISIBLE || echo DENIED)"
	w, err := iso.Wrap(WrapSpec{Key: "live", Bin: shBin, Argv: []string{"-c", probe}, TaskDir: taskDir})
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	defer w.Cleanup()
	out, err := exec.Command(w.Bin, w.Argv...).CombinedOutput()
	if err != nil {
		t.Fatalf("jailed probe: %v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, taskDir) {
		t.Fatalf("cwd inside jail is not the taskdir:\n%s", text)
	}
	if !strings.Contains(text, "DENIED") {
		t.Fatalf("unmounted host path was visible inside the jail:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(taskDir, "made.txt")); err != nil {
		t.Fatalf("write in jail cwd did not land in host taskdir: %v", err)
	}
}
```

- [ ] **Step 7: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — package `internal/isolate` does not exist yet.

- [ ] **Step 8: Implement the isolate package**

Create `internal/isolate/isolate.go`:

```go
// Package isolate provides the Isolator abstraction that turns a plain
// worker spawn (bin+argv in a taskdir) into an isolated one, leaving the
// runner's §9.3 spawn path (tee/timeout/group-kill) untouched. Backends:
// user-namespace jail (internal/jail), Landlock path rules as the
// fallback, refusal when neither is available — the chain lives in
// Select(). Threat model (spec §6): confine an honest-but-sloppy CLI, not
// a malicious one; network stays open.
package isolate

// WrapSpec describes one worker spawn to be isolated.
type WrapSpec struct {
	Key       string   // task key; names per-task scratch (jail root / landlock scratch)
	Bin       string   // engine binary (host path)
	Argv      []string // engine argv (may embed TaskDir — it stays valid because TaskDir is visible at the same path inside the sandbox)
	TaskDir   string   // host taskdir, read-write inside the sandbox at its host-identical path
	StateDirs []string // engine state dirs, read-write (config jail_state_dirs; "~" expanded here)
	ROBinds   []string // extra read-only trees, e.g. the engine's install dir (config jail_ro_binds)
	RepoRO    string   // worktrees mode: parent repo, read-only at its host-identical path ("" otherwise)
}

// Wrapped is the transformed spawn. Bin/Argv replace the originals in the
// runner's spawn path; Env entries (KEY=VALUE) are appended to the
// inherited environment; Cleanup removes per-task scratch (call it when
// the task is done; safe to call more than once).
type Wrapped struct {
	Bin     string
	Argv    []string
	Env     []string
	Cleanup func() error
}

// Isolator wraps worker spawns in an isolation backend.
type Isolator interface {
	// Name identifies the backend ("jail", "landlock") for logs and errors.
	Name() string
	Wrap(spec WrapSpec) (Wrapped, error)
}
```

Create `internal/isolate/jail.go`:

```go
package isolate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/jail"
)

// JailIsolator wraps spawns in a per-task UnshareJail (spec §6): read-only
// host toolchain, taskdir read-write at its host-identical path, tmpfs
// /tmp wired as TMPDIR/XDG_CACHE_HOME, state dirs read-write, extra ro
// binds for engine installs, and (worktrees) the parent repo read-only at
// its host-identical path — the Plan-1 spike proved the worktree's gitdir
// pointer needs exactly that. Default-deny reads: anything not mounted
// does not exist inside the namespace.
type JailIsolator struct {
	Base string // parent dir for per-task jail roots (e.g. <workdir>/.jail)
}

func (j *JailIsolator) Name() string { return "jail" }

func (j *JailIsolator) Wrap(spec WrapSpec) (Wrapped, error) {
	root := filepath.Join(j.Base, spec.Key)
	uj := jail.NewUnshareJail(root)

	// Bind mounts other than the base set, assembled separately so they can
	// be sorted parent-before-child: a mount at a deeper path must come
	// after the mount that contains it (e.g. a worktree taskdir whose
	// host-identical path nests inside the RepoRO bind).
	var binds []jail.Mount
	binds = append(binds, jail.BindMount(spec.TaskDir, filepath.Join(root, spec.TaskDir), false))
	for _, d := range spec.StateDirs {
		hostDir := config.ExpandUser(d)
		// Engine state dirs are created if missing (first run of an engine
		// on a fresh machine) — they are rw and owned by the user anyway.
		if err := os.MkdirAll(hostDir, 0o755); err != nil {
			return Wrapped{}, fmt.Errorf("jail state dir %s: %w", hostDir, err)
		}
		binds = append(binds, jail.BindMount(hostDir, filepath.Join(root, hostDir), false))
	}
	for _, d := range spec.ROBinds {
		hostDir := config.ExpandUser(d)
		// RO binds are engine installs: absence is a config error, not
		// something to create silently.
		if _, err := os.Stat(hostDir); err != nil {
			return Wrapped{}, fmt.Errorf("jail ro bind %s: %w", hostDir, err)
		}
		binds = append(binds, jail.BindMount(hostDir, filepath.Join(root, hostDir), true))
	}
	if spec.RepoRO != "" {
		binds = append(binds, jail.BindMount(spec.RepoRO, filepath.Join(root, spec.RepoRO), true))
	}
	sort.SliceStable(binds, func(a, b int) bool {
		return strings.Count(binds[a].Target, string(filepath.Separator)) <
			strings.Count(binds[b].Target, string(filepath.Separator))
	})

	// Order: host toolchain, then tmpfs /tmp, then the binds — so a bind
	// whose host path lives under /tmp (t.TempDir() in tests) lands INSIDE
	// the tmpfs instead of being shadowed by it (Plan-1 spike learning).
	mounts := append(jail.HostMounts(root), jail.TmpfsMount(filepath.Join(root, "tmp")))
	mounts = append(mounts, binds...)
	if err := uj.Setup(mounts); err != nil {
		return Wrapped{}, fmt.Errorf("jail setup: %w", err)
	}
	uj.SetChdir(spec.TaskDir) // §9.3: cwd = taskdir; chroot alone lands at /

	script := uj.Script(spec.Bin, spec.Argv...)
	argv := append(uj.UnshareArgs(), "--", "bash", "-c", script)
	return Wrapped{
		Bin:  "unshare",
		Argv: argv,
		// Spec §6: tmpfs scratch wired as TMPDIR/XDG_CACHE_HOME.
		Env: []string{"TMPDIR=/tmp", "XDG_CACHE_HOME=/tmp"},
		Cleanup: func() error {
			// Mounts died with the namespace; only scaffold dirs remain.
			return os.RemoveAll(root)
		},
	}, nil
}
```

- [ ] **Step 9: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS. `TestJailWrapLive` exercises the full loop on this machine (userns available on Arch); it auto-skips in CI.

- [ ] **Step 10: Commit**

```bash
git add internal/isolate
git commit -m "feat(isolate): Isolator abstraction + jail backend (spec §6 mount table)"
```

---

### Task 9: Landlock fallback — ABI probe, `landlock-exec` trampoline, `LandlockIsolator`

**Files:**
- Modify: `go.mod` / `go.sum` (new dep)
- Create: `internal/isolate/landlock.go` (portable), `internal/isolate/landlock_probe_linux.go`, `internal/isolate/landlock_probe_other.go`
- Create: `cmd/ringer/landlockexec.go` (linux-only build tag)
- Test: `internal/isolate/landlock_test.go`

**Interfaces:**
- Consumes: `isolate.WrapSpec`/`Wrapped`/`Isolator` (Task 8); `config.ExpandUser` (Task 1); the go-flags `parser` in `cmd/ringer/main.go:17`.
- Produces: `isolate.LandlockABI() (version int, ok bool)`; `isolate.LandlockIsolator{Self, ScratchDir string}`; hidden CLI subcommand `ringer landlock-exec --rw P … --ro P … -- BIN ARGS…`. Task 10's `Select` uses the probe and constructs the isolator.

**Why:** the fallback tier for hosts where unprivileged user namespaces are restricted (GitHub runners, hardened prod). Landlock is an LSM (Linux ≥ 5.13) that lets an UNPRIVILEGED process restrict its OWN filesystem access; the ruleset survives `execve` and is inherited by all descendants. Go can't run code between fork and exec, so the isolator re-execs the ringer binary through a hidden trampoline subcommand that (1) applies the ruleset to itself, (2) execs the engine. Same threat model as the jail — confine an honest-but-sloppy CLI; weaker in kind (path rules, not a mount namespace; best-effort degrades across ABI versions) but honest: `Select` (Task 10) refuses outright when Landlock is absent, so "best effort" never silently means "no confinement".

**Dep note:** `github.com/landlock-lsm/go-landlock` is the reference Go binding (maintained by the Landlock kernel folks). Pin whatever `go get github.com/landlock-lsm/go-landlock@latest` resolves. The code below targets its documented API (`landlock.V5.BestEffort().RestrictPaths(landlock.RODirs(...), landlock.RWDirs(...))`); if the pinned version has moved past V5 or renamed the option type, reconcile against the module's godoc at implementation time and note the actual pinned version in the commit message.

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/landlock-lsm/go-landlock@latest
```

(Yes, `go get` directly — build.sh has no dep subcommand; this is the one sanctioned exception, same as Plan 1's dep adds.) Then run `./build.sh --test` to confirm the tree still builds before any code uses it.

- [ ] **Step 2: Write the failing tests**

Create `internal/isolate/landlock_test.go`:

```go
package isolate

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLandlockWrapShapesTheSpawn(t *testing.T) {
	scratch := t.TempDir()
	taskDir := t.TempDir()
	stateDir := t.TempDir()
	iso := &LandlockIsolator{Self: "/opt/ringer/ringer", ScratchDir: scratch}
	w, err := iso.Wrap(WrapSpec{
		Key: "t1", Bin: "/usr/bin/tool", Argv: []string{"--flag", "v"},
		TaskDir: taskDir, StateDirs: []string{stateDir},
	})
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if w.Bin != "/opt/ringer/ringer" {
		t.Fatalf("Bin = %q, want the ringer binary (trampoline)", w.Bin)
	}
	if w.Argv[0] != "landlock-exec" {
		t.Fatalf("Argv[0] = %q, want landlock-exec", w.Argv[0])
	}
	joined := strings.Join(w.Argv, " ")
	sep := " -- "
	pre, post, found := strings.Cut(joined, sep)
	if !found {
		t.Fatalf("argv lacks the -- separator: %v", w.Argv)
	}
	for _, want := range []string{"--rw " + taskDir, "--rw " + stateDir, "--ro /usr", "--ro /etc"} {
		if !strings.Contains(pre, want) {
			t.Fatalf("rules missing %q in %q", want, pre)
		}
	}
	if !strings.HasPrefix(post, "/usr/bin/tool --flag v") {
		t.Fatalf("post-separator command = %q", post)
	}
	// TMPDIR points into the per-task scratch under ScratchDir.
	wantScratch := filepath.Join(scratch, "t1")
	foundTmp := false
	for _, e := range w.Env {
		if e == "TMPDIR="+wantScratch {
			foundTmp = true
		}
	}
	if !foundTmp {
		t.Fatalf("Env lacks TMPDIR=%s: %v", wantScratch, w.Env)
	}
	if _, err := os.Stat(wantScratch); err != nil {
		t.Fatalf("scratch not created: %v", err)
	}
	if err := w.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(wantScratch); !os.IsNotExist(err) {
		t.Fatal("scratch survived Cleanup")
	}
}

// TestLandlockTrampolineEnforces is the fallback-tier equivalent of the
// jail live test: it builds the ringer binary, runs a probe through the
// landlock-exec trampoline, and asserts write-inside-taskdir works while
// write-outside is denied. Runs on any Linux ≥ 5.13 — including GitHub
// runners, where the jail tests skip.
func TestLandlockTrampolineEnforces(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("landlock is Linux-only")
	}
	if _, ok := LandlockABI(); !ok {
		t.Skip("kernel lacks Landlock")
	}
	shBin, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh not on PATH: %v", err)
	}
	bin := buildRingerForIsolate(t)
	taskDir := t.TempDir()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	// The denied probe targets $HOME: normally user-writable, NOT covered
	// by any rule — only Landlock denies it. (It is only ever written if
	// enforcement is broken, i.e. on test failure.)
	denied := filepath.Join(home, ".ringer-landlock-probe-denied.txt")
	defer os.Remove(denied) // belt-and-braces: clean up if enforcement failed
	probe := "echo ok > allowed.txt && (echo x > " + denied + " 2>/dev/null && echo WROTE || echo DENIED) && cat /etc/hostname >/dev/null && echo READ-OK"
	iso := &LandlockIsolator{Self: bin, ScratchDir: t.TempDir()}
	w, err := iso.Wrap(WrapSpec{Key: "ll", Bin: shBin, Argv: []string{"-c", probe}, TaskDir: taskDir})
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	defer w.Cleanup()
	cmd := exec.Command(w.Bin, w.Argv...)
	cmd.Dir = taskDir
	cmd.Env = append(os.Environ(), w.Env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("trampoline: %v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "DENIED") {
		t.Fatalf("write outside the rules was NOT denied:\n%s", text)
	}
	if !strings.Contains(text, "READ-OK") {
		t.Fatalf("toolchain read failed under landlock:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(taskDir, "allowed.txt")); err != nil {
		t.Fatalf("write inside taskdir failed: %v", err)
	}
}

// buildRingerForIsolate compiles the ringer binary for trampoline tests
// (mirrors internal/runner's buildRingerBinary, which lives in another
// package).
func buildRingerForIsolate(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ringer")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/corruptmemory/ringer/cmd/ringer")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — `undefined: LandlockIsolator`, `undefined: LandlockABI`.

- [ ] **Step 4: Implement the probe**

Create `internal/isolate/landlock_probe_linux.go`:

```go
//go:build linux

package isolate

import "golang.org/x/sys/unix"

// LandlockABI probes the kernel's Landlock ABI version via
// landlock_create_ruleset(NULL, 0, LANDLOCK_CREATE_RULESET_VERSION).
// ok=false means Landlock is unavailable (kernel < 5.13, or the LSM is
// disabled at boot) — the caller must refuse, not degrade silently.
func LandlockABI() (int, bool) {
	v, err := unix.LandlockCreateRuleset(nil, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if err != nil || v < 1 {
		return 0, false
	}
	return v, true
}
```

Create `internal/isolate/landlock_probe_other.go`:

```go
//go:build !linux

package isolate

// LandlockABI reports Landlock as unavailable off Linux.
func LandlockABI() (int, bool) { return 0, false }
```

(If the pinned `x/sys` spells the probe call differently — the signature has drifted across releases — reconcile against its godoc; the syscall is `landlock_create_ruleset(attr=NULL, size=0, flags=LANDLOCK_CREATE_RULESET_VERSION)` and the version comes back as the return value.)

- [ ] **Step 5: Implement the isolator**

Create `internal/isolate/landlock.go`:

```go
package isolate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/corruptmemory/ringer/internal/config"
)

// LandlockIsolator is the fallback when user namespaces are unavailable
// (e.g. GitHub runners). It re-execs the ringer binary through the hidden
// `landlock-exec` subcommand, which applies a Landlock ruleset to itself
// and then execs the engine — the ruleset survives execve and is inherited
// by every descendant. Weaker than the jail (path rules instead of a mount
// namespace; best-effort degrades across kernel ABI versions) but the same
// threat model, and Select() refuses outright when Landlock is absent so
// the degradation is never silent.
type LandlockIsolator struct {
	Self       string // absolute path to the running ringer binary (the trampoline)
	ScratchDir string // parent for per-task scratch dirs (rw + TMPDIR)
}

func (l *LandlockIsolator) Name() string { return "landlock" }

// landlockRODirs is the host toolchain a worker may read. Anything not
// listed here or granted per-spec is denied — including $HOME dotfiles
// (~/.ssh, ~/.claude.json), matching the jail's default-deny posture.
// /tmp is read-only; writes go to the per-task scratch via TMPDIR.
var landlockRODirs = []string{
	"/usr", "/etc", "/bin", "/lib", "/lib64", "/sbin", "/opt",
	"/proc", "/sys", "/run", "/var", "/tmp",
}

// landlockRWDirs are always-writable device paths: shells and runtimes
// write /dev/null, /dev/tty, /dev/shm as a matter of course (a bare
// `2>/dev/null` opens the node for WRITING), and the jail likewise exposes
// writable device nodes. Device nodes are not exfiltration targets.
var landlockRWDirs = []string{"/dev"}

func (l *LandlockIsolator) Wrap(spec WrapSpec) (Wrapped, error) {
	scratch := filepath.Join(l.ScratchDir, spec.Key)
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return Wrapped{}, fmt.Errorf("landlock scratch: %w", err)
	}
	rw := append([]string{spec.TaskDir, scratch}, existingOnly(landlockRWDirs)...)
	for _, d := range spec.StateDirs {
		hostDir := config.ExpandUser(d)
		if err := os.MkdirAll(hostDir, 0o755); err != nil {
			return Wrapped{}, fmt.Errorf("landlock state dir %s: %w", hostDir, err)
		}
		rw = append(rw, hostDir)
	}
	ro := existingOnly(landlockRODirs)
	for _, d := range spec.ROBinds {
		hostDir := config.ExpandUser(d)
		if _, err := os.Stat(hostDir); err != nil {
			return Wrapped{}, fmt.Errorf("landlock ro path %s: %w", hostDir, err)
		}
		ro = append(ro, hostDir)
	}
	if spec.RepoRO != "" {
		ro = append(ro, spec.RepoRO)
	}

	argv := []string{"landlock-exec"}
	for _, p := range rw {
		argv = append(argv, "--rw", p)
	}
	for _, p := range ro {
		argv = append(argv, "--ro", p)
	}
	argv = append(argv, "--", spec.Bin)
	argv = append(argv, spec.Argv...)
	return Wrapped{
		Bin:  l.Self,
		Argv: argv,
		Env:  []string{"TMPDIR=" + scratch, "XDG_CACHE_HOME=" + scratch},
		Cleanup: func() error {
			return os.RemoveAll(scratch)
		},
	}, nil
}

// existingOnly filters to paths that exist: Landlock rules require
// openable paths, and e.g. /lib64 or /opt may be absent on a given distro.
func existingOnly(paths []string) []string {
	var out []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}
```

- [ ] **Step 6: Implement the trampoline subcommand**

Create `cmd/ringer/landlockexec.go` (linux-only: go-landlock and the exec path have no business compiling into the macOS binary, where `Select` refuses before ever reaching here):

```go
//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/landlock-lsm/go-landlock/landlock"
)

// landlockExecCmd is the hidden trampoline behind the Landlock isolator:
// `ringer landlock-exec --rw P … --ro P … -- BIN ARGS…` applies a Landlock
// ruleset to THIS process, then execs BIN — the ruleset survives execve
// and is inherited by every descendant. Hidden: process plumbing, not user
// surface.
type landlockExecCmd struct {
	RW   []string `long:"rw" description:"path allowed read-write"`
	RO   []string `long:"ro" description:"path allowed read-only"`
	Args struct {
		Argv []string `positional-arg-name:"CMD" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func (c *landlockExecCmd) Execute(args []string) error {
	rules := make([]landlock.Rule, 0, len(c.RO)+len(c.RW))
	for _, p := range c.RO {
		rules = append(rules, landlock.RODirs(p))
	}
	for _, p := range c.RW {
		rules = append(rules, landlock.RWDirs(p))
	}
	// BestEffort enforces the newest ABI the kernel offers and degrades on
	// older kernels. Select() has already refused when Landlock is entirely
	// absent, so best-effort can never mean "no confinement at all". A
	// restriction failure here is fatal: exec'ing UNCONFINED would be a
	// silent isolation downgrade.
	if err := landlock.V5.BestEffort().RestrictPaths(rules...); err != nil {
		return fmt.Errorf("landlock restrict: %w", err)
	}
	bin, err := exec.LookPath(c.Args.Argv[0])
	if err != nil {
		return err
	}
	return syscall.Exec(bin, c.Args.Argv, os.Environ())
}

func init() {
	cmd, err := parser.AddCommand("landlock-exec",
		"Apply a Landlock ruleset and exec a command (internal)",
		"Internal trampoline used by the isolation fallback; not for direct use.",
		&landlockExecCmd{})
	if err == nil {
		cmd.Hidden = true
	}
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `./build.sh --test`
Expected: PASS. `TestLandlockTrampolineEnforces` runs for real on this machine (Arch, recent kernel) — confirm in the output that it was NOT skipped. `TestLandlockWrapShapesTheSpawn` runs everywhere.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum internal/isolate cmd/ringer/landlockexec.go
git commit -m "feat(isolate): Landlock fallback — ABI probe, landlock-exec trampoline, path-rule isolator"
```

---

### Task 10: Select chain + preflight + runner/CLI wiring + jailed E2E

**Files:**
- Create: `internal/isolate/select.go`
- Modify: `internal/engine/engine.go` (Preflight), `internal/engine/engine_test.go`
- Modify: `internal/runner/runner.go` (Options + runTask wiring)
- Modify: `cmd/ringer/run.go` (isolator selection)
- Test: `internal/isolate/select_test.go`, `internal/runner/isolation_e2e_test.go`

**Interfaces:**
- Consumes: `JailIsolator` (Task 8), `LandlockIsolator`/`LandlockABI` (Task 9), `jail.CheckUnsharePreflight`, `logging.Logger`.
- Produces: `isolate.Select(lg logging.Logger, workdir, self string) (Isolator, error)`; `runner.Options.Isolator isolate.Isolator`; `engine.Preflight` no longer rejects `isolation = "jail"`. This is the task that makes `isolation = "jail"` WORK end to end.

**Why:** the last mile. `Select` encodes the settled fallback chain — jail > Landlock > refuse — with the fallback logged at Warn and refusal naming both missing capabilities (macOS gets pointed at the Seatbelt lane per spec §6). The runner wraps each attempt's spawn (the retry attempt has a DIFFERENT spec baked into argv, so wrapping happens per attempt); cleanups collect and run once at task end, Warn-logged. The E2E proves the full loop: a jailed mock engine writes into its taskdir through the namespace and the host-side verifier sees it; worktrees×jail proves the spike's probe-C finding (parent repo ro at host-identical path) in the real pipeline.

- [ ] **Step 1: Write the failing Select test**

Create `internal/isolate/select_test.go`:

```go
package isolate

import (
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/jail"
	"github.com/corruptmemory/ringer/internal/logging"
)

// Select picks by capability, so the assertable surface on any given host
// is: it returns SOMETHING sensible for THIS host, scratch roots land
// under the workdir, and the returned backend matches the host's actual
// capabilities.
func TestSelectMatchesHostCapabilities(t *testing.T) {
	workdir := t.TempDir()
	iso, err := Select(logging.Default(), workdir, "/opt/ringer/ringer")
	jailOK := jail.CheckUnsharePreflight().OK()
	_, landlockOK := LandlockABI()
	switch {
	case jailOK:
		if err != nil {
			t.Fatalf("jail available but Select failed: %v", err)
		}
		j, ok := iso.(*JailIsolator)
		if !ok {
			t.Fatalf("iso = %T, want *JailIsolator on a userns-capable host", iso)
		}
		if j.Base != filepath.Join(workdir, ".jail") {
			t.Fatalf("jail Base = %q", j.Base)
		}
	case landlockOK:
		if err != nil {
			t.Fatalf("landlock available but Select failed: %v", err)
		}
		l, ok := iso.(*LandlockIsolator)
		if !ok {
			t.Fatalf("iso = %T, want *LandlockIsolator fallback", iso)
		}
		if l.Self != "/opt/ringer/ringer" || l.ScratchDir != filepath.Join(workdir, ".scratch") {
			t.Fatalf("landlock fields = %+v", l)
		}
	default:
		if err == nil {
			t.Fatalf("no backend available but Select returned %T", iso)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./build.sh --test`
Expected: FAIL — `undefined: Select`.

- [ ] **Step 3: Implement Select**

Create `internal/isolate/select.go`:

```go
package isolate

import (
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/corruptmemory/ringer/internal/jail"
	"github.com/corruptmemory/ringer/internal/logging"
)

// Select picks the strongest available isolation backend for a run whose
// engines request isolation: jail (user namespaces) first, Landlock
// second, refusal third. The fallback is logged at Warn — a run silently
// downgrading isolation would be a silent failure. workdir seeds the
// per-task scratch locations; self is the running ringer binary (the
// Landlock trampoline re-execs it).
func Select(lg logging.Logger, workdir, self string) (Isolator, error) {
	pre := jail.CheckUnsharePreflight()
	if pre.OK() {
		return &JailIsolator{Base: filepath.Join(workdir, ".jail")}, nil
	}
	if abi, ok := LandlockABI(); ok {
		lg.Warnf("jail unavailable (%s); falling back to Landlock (ABI v%d): path rules instead of a mount namespace", pre.Error(), abi)
		return &LandlockIsolator{Self: self, ScratchDir: filepath.Join(workdir, ".scratch")}, nil
	}
	if runtime.GOOS == "darwin" {
		return nil, fmt.Errorf("isolation=\"jail\" is Linux-only; on macOS use the Seatbelt wrapper engine (engines/opencode-sandboxed.sh) with isolation=\"none\" (jail preflight: %s)", pre.Error())
	}
	return nil, fmt.Errorf("no isolation backend available — jail: %s; landlock: kernel support missing (needs Linux >= 5.13 with the Landlock LSM enabled)", pre.Error())
}
```

- [ ] **Step 4: Unblock preflight**

In `internal/engine/engine.go` `Preflight`, delete the clause:

```go
		if e.Isolation == "jail" {
			errs = append(errs, fmt.Errorf("engine %q uses isolation=\"jail\", which lands in Plan 3; use \"none\" for now", name))
			continue
		}
```

(The `continue` dies with it; the LookPath check now runs for jailed engines too — an absolute `bin` path passes LookPath when it exists and is executable.) Update the `engine_test.go` test at engine_test.go:67-70 that asserts `Preflight` rejects `{Bin: "sh", Isolation: "jail"}` with a "Plan 3" error: invert it — a jailed engine whose bin resolves must now PASS preflight (isolation enforcement moved to `Select`/runner).

- [ ] **Step 5: Run tests**

Run: `./build.sh --test`
Expected: PASS (Select test now green; engine tests updated).

- [ ] **Step 6: Write the failing E2E**

Create `internal/runner/isolation_e2e_test.go`:

```go
package runner

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/isolate"
	"github.com/corruptmemory/ringer/internal/jail"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/manifest"
)

// TestJailedMockEndToEnd runs the full pipeline with isolation="jail": the
// mock engine (the ringer binary itself) executes inside a user-namespace
// chroot, writes its deliverable into the taskdir through the
// host-identical bind, and the HOST-side verifier confirms it. The ringer
// binary lives outside the host-toolchain mounts, so jail_ro_binds carries
// its directory — exercising that key for real.
func TestJailedMockEndToEnd(t *testing.T) {
	if r := jail.CheckUnsharePreflight(); !r.OK() {
		t.Skipf("userns preflight failed: %s", r.Error())
	}
	ringerBin := buildRingerBinary(t)
	workdir := filepath.Join(t.TempDir(), "jailed-work")

	m := &manifest.Manifest{
		RunName: "jail-e2e", Workdir: workdir,
		Tasks: []manifest.Task{
			{Key: "jt", Engine: "mock", TimeoutS: 60,
				Spec:  "MOCK_FILE: out.txt\njailed hello\nMOCK_END",
				Check: "grep -q 'jailed hello' out.txt"},
		},
	}
	engines := map[string]config.EngineConfig{
		"mock": {
			Bin: ringerBin, ArgsTemplate: []string{"mock-worker", "{spec}"},
			Isolation:   "jail",
			JailRoBinds: []string{filepath.Dir(ringerBin)},
		},
	}
	res, err := Run(context.Background(), Options{
		Manifest: m, Engines: engines, StateDir: t.TempDir(),
		Identity: "test", Stdout: io.Discard, Logger: logging.Default(),
		Isolator: &isolate.JailIsolator{Base: filepath.Join(workdir, ".jail")},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.AllPassed {
		t.Fatalf("results = %+v, want PASS (worker wrote through the jail into the host taskdir)", res.Results)
	}
	// Per-task jail scaffolding cleaned up.
	if _, err := os.Stat(filepath.Join(workdir, ".jail", "jt")); !os.IsNotExist(err) {
		t.Fatalf("jail root not cleaned (stat err = %v)", err)
	}
}

// TestWorktreesJailEndToEnd is the spike's probe C in the real pipeline:
// the engine runs `git status` INSIDE the jail, inside a worktree taskdir,
// which only works if the parent repo is bind-mounted read-only at its
// host-identical path (WrapSpec.RepoRO).
func TestWorktreesJailEndToEnd(t *testing.T) {
	if r := jail.CheckUnsharePreflight(); !r.OK() {
		t.Skipf("userns preflight failed: %s", r.Error())
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	repo := gitFixtureRepo(t)
	workdir := filepath.Join(t.TempDir(), "wtjail-work")

	m := &manifest.Manifest{
		RunName: "wtjail-e2e", Workdir: workdir, Worktrees: true, Repo: repo,
		Tasks: []manifest.Task{
			// The ENGINE is git itself: `git -C <taskdir> status` exercises
			// the worktree gitdir pointer from inside the namespace.
			{Key: "gt", Engine: "gitstatus", TimeoutS: 60,
				Spec:  "unused",
				Check: "true"},
		},
	}
	engines := map[string]config.EngineConfig{
		"gitstatus": {
			Bin: gitBin, ArgsTemplate: []string{"-C", "{taskdir}", "status"},
			Isolation: "jail",
		},
	}
	res, err := Run(context.Background(), Options{
		Manifest: m, Engines: engines, StateDir: t.TempDir(),
		Identity: "test", Stdout: io.Discard, Logger: logging.Default(),
		Isolator: &isolate.JailIsolator{Base: filepath.Join(workdir, ".jail")},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.AllPassed {
		t.Fatalf("results = %+v, want PASS (git status inside jail+worktree needs the RepoRO mount)", res.Results)
	}
}
```

- [ ] **Step 7: Run tests to verify they fail**

Run: `./build.sh --test`
Expected: FAIL — `Options` has no `Isolator` field; `runTask` still errors with "jail isolation lands in Plan 3".

- [ ] **Step 8: Wire the runner**

In `internal/runner/runner.go`:

1. Import `github.com/corruptmemory/ringer/internal/isolate` and add the field:

```go
type Options struct {
	Manifest    *manifest.Manifest
	Engines     map[string]config.EngineConfig
	StateDir    string
	Identity    string
	Store       *store.Store // may be nil (skip eval logging)
	Stdout      io.Writer
	Logger      logging.Logger // nil -> logging.Default()
	MaxParallel int            // 0 -> len(tasks)
	Isolator    isolate.Isolator // required iff any task's engine sets isolation="jail"
}
```

2. In `runTask`, replace the "lands in Plan 3" block with:

```go
	var iso isolate.Isolator
	if engConf.Isolation == "jail" && !task.FullAccess {
		// Spec §6: full_access: true = no jail (unchanged semantics) — the
		// task explicitly asked for the unconfined lane.
		iso = opts.Isolator
		if iso == nil {
			lg.Errorf("task %s: isolation=\"jail\" but no isolator was selected (CLI preflight bug)", task.Key)
			a.setResult(task.Key, "failed", -1, task.Verified, "")
			return
		}
	}
```

3. Before the attempt loop, add the cleanup collector:

```go
	var cleanups []func() error
	defer func() {
		for _, c := range cleanups {
			if cerr := c(); cerr != nil {
				lg.Warnf("task %s: isolation cleanup: %v", task.Key, cerr)
			}
		}
	}()
	repoRO := ""
	if opts.Manifest.Worktrees {
		repoRO = opts.Manifest.Repo
	}
```

4. Inside the loop, wrap per attempt (the retry's argv embeds a different spec) between `BuildArgv` and the spawn:

```go
		bin, argv := engine.BuildArgv(engConf, taskDir, spec, model, task.EngineArgs, task.FullAccess)
		var extraEnv []string
		if iso != nil {
			wrapped, werr := iso.Wrap(isolate.WrapSpec{
				Key: task.Key, Bin: bin, Argv: argv, TaskDir: taskDir,
				StateDirs: engConf.JailStateDirs, ROBinds: engConf.JailRoBinds,
				RepoRO: repoRO,
			})
			if werr != nil {
				lg.Errorf("task %s: isolate (%s): %v", task.Key, iso.Name(), werr)
				verdict = "ERROR"
				break
			}
			bin, argv, extraEnv = wrapped.Bin, wrapped.Argv, wrapped.Env
			if wrapped.Cleanup != nil {
				cleanups = append(cleanups, wrapped.Cleanup)
			}
		}
		lg.Infof("task %s: attempt %d: %s", task.Key, attempt, bin)
		w := io.MultiWriter(opts.Stdout, col.sink(task.Key)) // tee live output into the collector
		outcome := runWorker(ctx, bin, argv, taskDir, logPath, w, timeout, extraEnv)
```

(Keep the existing `lg.Infof` / `w :=` lines — the snippet shows their final relative order.)

- [ ] **Step 9: Wire the CLI**

In `cmd/ringer/run.go` `runManifestFile`, after the `engine.Preflight` call:

```go
	// Isolation backend: selected once per run, only when some task will
	// actually jail (spec §6 preflight rule) — a full_access task takes
	// the unconfined lane and must not trigger selection (or a refusal)
	// on its own. Selection failures are refusals — precise, actionable,
	// before any task starts.
	var iso isolate.Isolator
	for _, t := range m.Tasks {
		if t.FullAccess {
			continue
		}
		e, rerr := engine.Resolve(engines, t.Engine)
		if rerr != nil || e.Isolation != "jail" {
			continue
		}
		self, serr := os.Executable()
		if serr != nil {
			return fmt.Errorf("resolve own binary for isolation trampoline: %w", serr)
		}
		iso, err = isolate.Select(lg, m.Workdir, self)
		if err != nil {
			return err
		}
		lg.Infof("isolation backend: %s", iso.Name())
		break
	}
```

(`engine.Resolve` applies the `"" -> "codex"` default itself, so passing `t.Engine` directly is correct.)

and pass it through:

```go
	res, err := runner.Run(ctx, runner.Options{
		Manifest: m, Engines: engines, StateDir: cfg.StateDirPath(),
		Identity: identity, Store: st, Stdout: os.Stdout, Logger: lg,
		MaxParallel: m.MaxParallel, Isolator: iso,
	})
```

Import `github.com/corruptmemory/ringer/internal/isolate`.

- [ ] **Step 10: Run tests to verify everything passes**

Run: `./build.sh --test`
Expected: PASS — including both E2Es live on this machine. Then run the race build once (local-only per house rules):

Run: `./build.sh --test --race`
Expected: PASS.

- [ ] **Step 11: Manual demo smoke + commit**

```bash
./ringer demo          # unchanged behavior (mock engine, no isolation)
git add internal/isolate internal/engine internal/runner cmd/ringer
git commit -m "feat: isolation live end-to-end — Select chain (jail > Landlock > refuse), runner/CLI wiring"
```

---

## Plan-3 Deliberate Divergences (for reviewers)

Recorded here so review findings can be adjudicated against intent:

1. **Worktrees without `repo` is a load-time ERROR** (Task 6). Python silently degrades to plain directories. Loud beats silent.
2. **Reserved `logs` task key applies in ALL modes** (Task 6). Python reserves it only in worktrees mode; Go's log layout is always `<workdir>/logs/` (Plan-2 shipped), so the collision exists in all modes here.
3. **Worktree-add/remove failures log via `logging.Logger`**, not appended into the worker log (Task 7). Python appends `[ringer.py] git worktree …` lines into the worker log; §9.8 says worker logs carry RAW WORKER OUTPUT ONLY, which the Python behavior technically violated. The Go HUD (Plan 4) reads run-state + logger output.
4. **Failure-context cap is bytes, not chars** (Task 4) — consistent with the byte-vs-rune stance recorded for lint thresholds in the Plan-2 ledger.
5. **`landlock-exec` is Linux-only by build tag** (Task 9); macOS binaries never contain it because `Select` refuses on darwin first.
6. **Retry log = single append-mode file** (Task 2). The Plan-2 ledger recorded the finding as "retry log overwrite (attempt suffix)" — the suffix was the reviewer's suggested fix; ringer.py's actual behavior is unlink-once-then-append (ringer.py:7222, 7107), and parity wins over the suggestion.
7. **Landlock `/dev` is read-write** (Task 9). The jail exposes writable device nodes; a bare `2>/dev/null` opens the node for writing. Device nodes are not exfiltration targets.

## Out of Scope (carry-forwards left for Plan 4+)

- `SetDropUser`/UID-mapping lane: the Plan-1 spike proved opencode tolerates ns-root; revisit only if a real engine refuses ns-root.
- `allow_full_access` config gating: spec §6 says `full_access` is "gated by config `allow_full_access`", but Plan 2 shipped `full_access` ungated (the key decodes, nothing enforces it). This plan only implements "full_access = no jail"; the gate itself is a pre-cutover item (record in the ledger).
- Serial-run lint nudge (`>=3 tasks && max_parallel==1`), reserved scoreboard run_name, verified/task_type nudges — HUD/analytics adjacent, Plan 4/5.
- Store driver seam (ncruces) stays conceptual; nothing here touches the store beyond the Checkpoint caller.
- HUD surfacing of isolation backend per run — Plan 4.



