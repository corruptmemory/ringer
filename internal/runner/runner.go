package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/engine"
	"github.com/corruptmemory/ringer/internal/isolate"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/manifest"
	"github.com/corruptmemory/ringer/internal/state"
	"github.com/corruptmemory/ringer/internal/store"
	"github.com/corruptmemory/ringer/internal/verify"
)

const defaultTimeoutS = 900

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

// Options configures a Run. Store may be nil to skip eval logging; Logger nil
// falls back to logging.Default(); MaxParallel <= 0 means "unbounded" (one
// goroutine per task).
type Options struct {
	Manifest    *manifest.Manifest
	Engines     map[string]config.EngineConfig
	StateDir    string
	Identity    string
	Store       *store.Store // may be nil (skip eval logging)
	Stdout      io.Writer
	Logger      logging.Logger   // nil -> logging.Default()
	MaxParallel int              // 0 -> len(tasks)
	Isolator    isolate.Isolator // required iff any task's engine sets isolation="jail"
}

// TaskResult is one task's final outcome in a RunResult.
type TaskResult struct {
	Key      string
	Verdict  string
	Attempts int
	Tokens   int64
}

// RunResult is the outcome of a full Run.
type RunResult struct {
	RunID     string
	Results   []TaskResult
	AllPassed bool
}

// Run executes the manifest end-to-end: prepare each task dir, run its worker
// (bounded by MaxParallel), verify, retry once with failure context appended,
// log each attempt to Store, and flush run-state each second. Headless.
func Run(ctx context.Context, opts Options) (RunResult, error) {
	m := opts.Manifest
	lg := opts.Logger
	if lg == nil {
		lg = logging.Default()
	}
	if m.Worktrees && m.Repo == "" {
		return RunResult{}, fmt.Errorf("worktrees mode requires repo (manifest validation should have caught this)")
	}

	keys := make([]string, len(m.Tasks))
	for i, t := range m.Tasks {
		keys[i] = t.Key
	}

	runID := fmt.Sprintf("%s-%d", m.RunName, time.Now().UnixNano())
	startedAt := time.Now().UTC().Format(time.RFC3339)

	// Seed each TaskView's Engine/Model at construction from the same
	// resolution runTask itself will apply (resolveTaskEngine), so the
	// written run-state JSON reports the effective engine/model from the
	// first snapshot onward instead of serializing "" until the task
	// actually starts running. Best-effort: an unresolvable engine name is
	// still surfaced here (as the raw/defaulted name) even though runTask
	// will fail that task fast — display, not execution, is what this seeds.
	engineByKey := make(map[string]string, len(m.Tasks))
	modelByKey := make(map[string]string, len(m.Tasks))
	for _, t := range m.Tasks {
		engineName, _, model, _ := resolveTaskEngine(opts.Engines, t)
		engineByKey[t.Key] = engineName
		modelByKey[t.Key] = model
	}

	a := newActor(runID, m.RunName, opts.Identity, keys, engineByKey, modelByKey, lg)
	a.start()
	defer a.stopAndWait()

	// newCollector takes runID + logger (review fix: logged double-stop) —
	// the brief's literal newCollector(256 << 10) predates that signature.
	col := newCollector(256<<10, runID, lg) // 256KB recent output per task (token scrape now, live HUD later)
	col.start()
	defer col.stopAndWait()

	// RegisterActiveRun's real signature carries workdir as a 5th field
	// (Python-parity review fix) and orders identity/runName/workdir before
	// pid/startedAt — the brief's literal call predates that signature.
	//
	// Register/unregister failures are non-fatal (the run itself doesn't
	// depend on active-runs.json) but must not be silent — logged at Warn,
	// mirroring how writeState's WriteRunState failures are logged just below.
	if err := state.RegisterActiveRun(opts.StateDir, runID, opts.Identity, m.RunName, m.Workdir, os.Getpid(), startedAt); err != nil {
		lg.Warnf("run %s: register active run: %v", runID, err)
	}
	defer func() {
		if err := state.UnregisterActiveRun(opts.StateDir, runID); err != nil {
			lg.Warnf("run %s: unregister active run: %v", runID, err)
		}
	}()

	writeState := func(done bool) {
		s := a.snapshot()
		s.PID = os.Getpid()
		s.StartedAt = startedAt
		s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		s.Done = done
		if err := state.WriteRunState(opts.StateDir, s); err != nil {
			lg.Warnf("run %s: write state: %v", runID, err)
		}
	}

	flushDone := make(chan struct{})
	tickerDone := make(chan struct{}) // closed once the ticker goroutine has fully exited
	go func() {
		defer close(tickerDone)
		t := time.NewTicker(1 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-flushDone:
				return
			case <-t.C:
				writeState(false)
			}
		}
	}()

	maxPar := opts.MaxParallel
	if maxPar <= 0 {
		maxPar = len(m.Tasks)
	}
	if maxPar < 1 {
		maxPar = 1
	}
	sem := make(chan struct{}, maxPar)
	var wg sync.WaitGroup
	for _, task := range m.Tasks {
		wg.Add(1)
		go func(task manifest.Task) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			runTask(ctx, opts, a, col, lg, runID, task)
		}(task)
	}
	wg.Wait()

	close(flushDone)
	<-tickerDone     // join: guarantees no in-flight writeState(false) can land after the final write below
	writeState(true) // final flush, Done=true

	// Result from the authoritative actor snapshot.
	snap := a.snapshot()
	res := RunResult{RunID: runID, AllPassed: true}
	for _, tv := range snap.Tasks {
		verdict := statusToVerdict(tv.Status)
		if verdict != "PASS" {
			res.AllPassed = false
		}
		res.Results = append(res.Results, TaskResult{
			Key: tv.Key, Verdict: verdict, Attempts: tv.Attempt, Tokens: tv.Tokens,
		})
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		// Teardown already ran: final state flushed (done:true), actor and
		// collector stopping via defers, active-runs unregistering via
		// defer. Surface the interruption so the CLI exits non-zero.
		return res, fmt.Errorf("run %s interrupted: %w", runID, ctxErr)
	}
	return res, nil
}

// resolveTaskEngine resolves a task's effective engine name, engine config,
// and model in one place: engineName applies the "" -> "codex" default (the
// same default engine.Resolve itself applies, kept explicit here so callers
// can key maps by it before engine.Resolve runs), then engine.Resolve (not a
// raw map lookup) so engine:"" / "codex" falls back to engine.BuiltinCodex()
// when the config's Engines map has no explicit "codex" entry — this is the
// same resolution Preflight already runs, so a manifest that passes
// Preflight must not die here with "unknown engine" for the documented
// default. model applies task.Model, falling back to the resolved engine's
// ModelDefault when the task didn't pin one (only meaningful when err is
// nil; on error engConf is the zero value and ModelDefault is "").
//
// Used both to seed each TaskView's Engine/Model at actor construction
// (before any attempt runs, in Run) and by runTask itself, so the
// initially-displayed values and the values actually used to run can never
// diverge.
func resolveTaskEngine(engines map[string]config.EngineConfig, task manifest.Task) (engineName string, engConf config.EngineConfig, model string, err error) {
	engineName = task.Engine
	if engineName == "" {
		engineName = "codex"
	}
	engConf, err = engine.Resolve(engines, engineName)
	model = task.Model
	if model == "" {
		model = engConf.ModelDefault
	}
	return engineName, engConf, model, err
}

// runTask runs one manifest task through up to two attempts.
func runTask(ctx context.Context, opts Options, a *actor, col *collector, lg logging.Logger, runID string, task manifest.Task) {
	engineName, engConf, model, err := resolveTaskEngine(opts.Engines, task)
	if err != nil {
		lg.Errorf("task %s: %v", task.Key, err)
		a.setResult(task.Key, "failed", -1, task.Verified, "", time.Now().UTC().Format(time.RFC3339))
		return
	}
	var iso isolate.Isolator
	if engConf.Isolation == "jail" && !task.FullAccess {
		// Spec §6: full_access: true = no jail (unchanged semantics) — the
		// task explicitly asked for the unconfined lane.
		iso = opts.Isolator
		if iso == nil {
			lg.Errorf("task %s: isolation=\"jail\" but no isolator was selected (CLI preflight bug)", task.Key)
			a.setResult(task.Key, "failed", -1, task.Verified, "", time.Now().UTC().Format(time.RFC3339))
			return
		}
	}

	taskDir := filepath.Join(opts.Manifest.Workdir, task.Key)
	if err := prepareTaskDir(opts.Manifest, taskDir); err != nil {
		lg.Errorf("task %s: prepare taskdir: %v", task.Key, err)
		a.setResult(task.Key, "failed", -1, task.Verified, "", time.Now().UTC().Format(time.RFC3339))
		return
	}
	logsDir := filepath.Join(opts.Manifest.Workdir, "logs")
	_ = os.MkdirAll(logsDir, 0o755)
	logPath := filepath.Join(logsDir, task.Key+".worker.log")
	// Append-mode log (ringer.py:7107): clear once per task so a rerun in
	// the same workdir starts fresh, then both attempts accumulate.
	if err := os.Remove(logPath); err != nil && !os.IsNotExist(err) {
		lg.Warnf("task %s: remove stale worker log: %v", task.Key, err)
	}

	timeoutS := task.TimeoutS
	if timeoutS == 0 {
		timeoutS = defaultTimeoutS
	}
	timeout := time.Duration(timeoutS) * time.Second

	spec := task.Spec
	verdict := "ERROR"
	var tokens int64 = -1
	attempts := 0

	// Cleanups collect across every attempt and run once at task end, after
	// runWorker has returned (the jailed process has fully exited) for the
	// LAST attempt — running one mid-task, while an earlier attempt's spec
	// is only relevant for the retry, would still be fine, but running it
	// before its OWN attempt's runWorker returns would race the namespace
	// teardown against the still-running process. Deferred here guarantees
	// "task end," which is always after every attempt's runWorker call has
	// returned.
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

	for attempt := 1; attempt <= 2; attempt++ {
		attempts = attempt
		a.setStatus(task.Key, "running", attempt, time.Now().UTC().Format(time.RFC3339))

		// Timed from worker spawn through verify completion (Python parity:
		// per-attempt wall time), so duration_s is populated for every row
		// instead of left at its zero default.
		attemptStart := time.Now()
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
		tokens = engine.ParseTokens(engConf.TokenRegex, col.tail(task.Key, 64<<10)) // scrape the post-exit tail

		vres := verify.Verify(ctx, taskDir, task.Check, task.ExpectFiles, timeout)
		durationS := time.Since(attemptStart).Seconds()
		switch {
		case outcome.TimedOut:
			verdict = "TIMEOUT"
		case outcome.Err != nil:
			verdict = "ERROR"
		case vres.Pass:
			verdict = "PASS"
		default:
			verdict = "FAIL"
		}

		if opts.Store != nil {
			if err := opts.Store.InsertAttempt(store.Attempt{
				RunID: runID, RunName: opts.Manifest.RunName, TaskKey: task.Key,
				Engine: engineName, Model: model, TaskType: task.TaskType,
				Verdict: verdict, Retry: attempt - 1, DurationS: durationS, Tokens: tokens,
				CheckOutput: vres.Output, Identity: opts.Identity,
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
			}); err != nil {
				lg.Warnf("task %s: insert attempt: %v", task.Key, err)
			}
		}

		if verdict == "PASS" {
			break
		}
		if attempt == 1 {
			// Inject failure context into the spec for the retry.
			spec = task.Spec + "\n\n--- Previous attempt failed. Check output:\n" + capTail(vres.Output, failureContextMax)
			lg.Warnf("task %s: attempt 1 %s; retrying", task.Key, verdict)
		}
	}

	if verdict == "PASS" {
		cleanupWorktreeOnPass(opts.Manifest, lg, task.Key, taskDir, logsDir)
	}

	a.setResult(task.Key, verdictToStatus(verdict), tokens, task.Verified, logPath, time.Now().UTC().Format(time.RFC3339))
	lg.Infof("task %s: %s (%d attempt(s), tokens=%d)", task.Key, verdict, attempts, tokens)
}

func statusToVerdict(status string) string {
	switch status {
	case "passed":
		return "PASS"
	case "timeout":
		return "TIMEOUT"
	case "failed":
		return "FAIL"
	default:
		return "ERROR"
	}
}

func verdictToStatus(verdict string) string {
	switch verdict {
	case "PASS":
		return "passed"
	case "TIMEOUT":
		return "timeout"
	default:
		return "failed"
	}
}
