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
