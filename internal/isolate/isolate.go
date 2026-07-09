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
