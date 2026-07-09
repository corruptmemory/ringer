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
// via io.MultiWriter, to also forward output to a collector sink). The
// process runs in its own process group (Setpgid) so that on timeout the
// whole group can be signaled: SIGTERM first, then SIGKILL after a 5s grace
// period if it hasn't exited. cmd.Wait() joins os/exec's internal copy
// goroutines, so once it returns all writes to w have completed.
func runWorker(ctx context.Context, bin string, argv []string, taskDir, logPath string, w io.Writer, timeout time.Duration) WorkerOutcome {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return WorkerOutcome{Err: err}
	}
	defer devNull.Close()

	logFile, err := os.Create(logPath)
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

	if err := cmd.Start(); err != nil {
		return WorkerOutcome{Err: err}
	}
	pgid := cmd.Process.Pid

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	var timedOut bool
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
			timedOut = true
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
			select {
			case waitErr = <-waitDone:
			case <-time.After(termGrace):
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
				waitErr = <-waitDone
			}
		}
	}

	outcome := WorkerOutcome{TimedOut: timedOut}
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			outcome.ExitCode = exitErr.ExitCode()
		} else {
			outcome.Err = waitErr
		}
	}
	return outcome
}
