package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/engine"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/manifest"
	"github.com/corruptmemory/ringer/internal/state"
	"github.com/corruptmemory/ringer/internal/store"
	"github.com/corruptmemory/ringer/internal/verify"
)

const defaultTimeoutS = 900

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
	Logger      logging.Logger // nil -> logging.Default()
	MaxParallel int            // 0 -> len(tasks)
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
	if m.Worktrees {
		return RunResult{}, fmt.Errorf("worktrees mode lands in Plan 3")
	}

	keys := make([]string, len(m.Tasks))
	for i, t := range m.Tasks {
		keys[i] = t.Key
	}

	runID := fmt.Sprintf("%s-%d", m.RunName, time.Now().UnixNano())
	startedAt := time.Now().UTC().Format(time.RFC3339)

	a := newActor(runID, m.RunName, opts.Identity, keys, lg)
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
	_ = state.RegisterActiveRun(opts.StateDir, runID, opts.Identity, m.RunName, m.Workdir, os.Getpid(), startedAt)
	defer func() { _ = state.UnregisterActiveRun(opts.StateDir, runID) }()

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
	return res, nil
}

// runTask runs one manifest task through up to two attempts.
func runTask(ctx context.Context, opts Options, a *actor, col *collector, lg logging.Logger, runID string, task manifest.Task) {
	engineName := task.Engine
	if engineName == "" {
		engineName = "codex"
	}
	// engine.Resolve (not a raw map lookup) so engine:"" / "codex" falls back
	// to engine.BuiltinCodex() when the config's Engines map has no explicit
	// "codex" entry — this is the same resolution Preflight already runs, so
	// a manifest that passes Preflight must not die here with "unknown
	// engine" for the documented default.
	engConf, err := engine.Resolve(opts.Engines, engineName)
	if err != nil {
		lg.Errorf("task %s: %v", task.Key, err)
		a.setResult(task.Key, "failed", -1, task.Verified, "")
		return
	}
	if engConf.Isolation == "jail" {
		lg.Errorf("task %s: jail isolation lands in Plan 3", task.Key)
		a.setResult(task.Key, "failed", -1, task.Verified, "")
		return
	}

	taskDir := filepath.Join(opts.Manifest.Workdir, task.Key)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		lg.Errorf("task %s: mkdir %s: %v", task.Key, taskDir, err)
		a.setResult(task.Key, "failed", -1, task.Verified, "")
		return
	}
	logsDir := filepath.Join(opts.Manifest.Workdir, "logs")
	_ = os.MkdirAll(logsDir, 0o755)
	logPath := filepath.Join(logsDir, task.Key+".worker.log")

	model := task.Model
	if model == "" {
		model = engConf.ModelDefault
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

	for attempt := 1; attempt <= 2; attempt++ {
		attempts = attempt
		a.setStatus(task.Key, "running", attempt)

		// Timed from worker spawn through verify completion (Python parity:
		// per-attempt wall time), so duration_s is populated for every row
		// instead of left at its zero default.
		attemptStart := time.Now()
		bin, argv := engine.BuildArgv(engConf, taskDir, spec, model, task.EngineArgs, task.FullAccess)
		lg.Infof("task %s: attempt %d: %s", task.Key, attempt, bin)
		w := io.MultiWriter(opts.Stdout, col.sink(task.Key)) // tee live output into the collector
		outcome := runWorker(ctx, bin, argv, taskDir, logPath, w, timeout)
		if outcome.Err != nil {
			lg.Errorf("task %s: spawn error: %v", task.Key, outcome.Err)
		}
		tokens = scrapeTokens(engConf.TokenRegex, col.tail(task.Key, 64<<10)) // scrape the post-exit tail

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
			spec = task.Spec + "\n\n--- Previous attempt failed. Check output:\n" + vres.Output
			lg.Warnf("task %s: attempt 1 %s; retrying", task.Key, verdict)
		}
	}

	a.setResult(task.Key, verdictToStatus(verdict), tokens, task.Verified, logPath)
	lg.Infof("task %s: %s (%d attempt(s), tokens=%d)", task.Key, verdict, attempts, tokens)
}

// scrapeTokens pulls a token count from the tail using the engine's
// token_regex (last match wins; last capture group, or whole match if none).
// Returns -1 when unknown.
func scrapeTokens(tokenRegex, tail string) int64 {
	if tokenRegex == "" {
		return -1
	}
	re, err := regexp.Compile(tokenRegex)
	if err != nil {
		return -1
	}
	matches := re.FindAllStringSubmatch(tail, -1)
	if len(matches) == 0 {
		return -1
	}
	last := matches[len(matches)-1]
	grp := last[len(last)-1]
	n, err := strconv.ParseInt(strings.TrimSpace(grp), 10, 64)
	if err != nil {
		return -1
	}
	return n
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
