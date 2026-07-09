package runner

import (
	"context"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// WorkerOutcome reports how a worker process finished.
type WorkerOutcome struct {
	ExitCode int
	TimedOut bool
	Canceled bool // parent context canceled (user interrupt) — distinct from a per-attempt timeout
	Err      error
}

// termGrace is the delay between SIGTERM and SIGKILL when a worker times
// out. Frozen at 5s per the §9.3 contract for production use; tests may
// shorten it (non-parallel, restored via t.Cleanup) to keep the SIGKILL
// fallback path fast to exercise.
var termGrace = 5 * time.Second

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
