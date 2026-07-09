package verify

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Result struct {
	Pass     bool
	Output   string
	ExitCode int
	TimedOut bool
	Missing  []string
}

func Verify(ctx context.Context, taskDir, check string, expectFiles []string, timeout time.Duration) Result {
	var res Result
	for _, f := range expectFiles {
		info, err := os.Stat(filepath.Join(taskDir, f))
		if err != nil || info.Size() == 0 {
			res.Missing = append(res.Missing, f)
		}
	}
	if len(res.Missing) > 0 {
		return res // check does not run if the floor isn't met
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "sh", "-c", check)
	cmd.Dir = taskDir
	out, err := cmd.CombinedOutput()
	res.Output = string(out)
	if cctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		return res
	}
	if err == nil {
		res.Pass = true
		return res
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
	} else {
		res.ExitCode = -1
	}
	return res
}
