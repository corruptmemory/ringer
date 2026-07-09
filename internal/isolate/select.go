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
