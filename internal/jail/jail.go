// Package jail is vendored from github.com/corruptmemory/flywheel (jail/,
// as of 2026-03-29). Rootless Linux isolation via unprivileged user
// namespaces + mount namespace + chroot. Do not diverge from upstream
// without noting it here; deps are stdlib + golang.org/x/sys only.
//
// Package jail provides chroot jail lifecycle management with pluggable
// implementations for root-based and user-namespace-based isolation.
package jail

import "os/exec"

// Jail manages a chroot environment. Implementations handle mount setup,
// command execution inside the chroot, and teardown.
type Jail interface {
	// Setup prepares the jail with base mounts plus additional bind mounts.
	// What "setup" means depends on the implementation: RootJail mounts
	// immediately; UnshareJail may defer mounts to Command time.
	Setup(mounts []Mount) error

	// Command returns an *exec.Cmd configured to run inside the jail.
	// The caller controls stdout/stderr and calls Run() or Start().
	Command(name string, args ...string) *exec.Cmd

	// Teardown cleans up mounts and resources. May be a no-op if the
	// implementation relies on namespace lifecycle for cleanup.
	Teardown() error

	// Root returns the jail's root directory path on the host filesystem.
	Root() string
}
