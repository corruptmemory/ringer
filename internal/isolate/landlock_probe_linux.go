//go:build linux

package isolate

import "golang.org/x/sys/unix"

// LandlockABI probes the kernel's Landlock ABI version via
// landlock_create_ruleset(NULL, 0, LANDLOCK_CREATE_RULESET_VERSION).
// ok=false means Landlock is unavailable (kernel < 5.13, or the LSM is
// disabled at boot) — the caller must refuse, not degrade silently.
//
// golang.org/x/sys/unix (pinned v0.44.0) does not expose a high-level
// LandlockCreateRuleset wrapper — only the raw syscall number
// (unix.SYS_LANDLOCK_CREATE_RULESET) and the
// unix.LANDLOCK_CREATE_RULESET_VERSION flag constant are present. The
// go-landlock module's own internal syscall shim (landlock/syscall,
// unimportable from outside the module) does the same raw
// unix.Syscall(...VERSION) call under the hood, so this mirrors that.
func LandlockABI() (int, bool) {
	v, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 || int(v) < 1 {
		return 0, false
	}
	return int(v), true
}
