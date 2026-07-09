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
