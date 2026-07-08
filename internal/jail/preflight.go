package jail

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

// PreflightResult holds the results of unshare capability checks.
type PreflightResult struct {
	UnshareFound  bool
	UserNSEnabled bool
	SubUIDMapped  bool
	SubGIDMapped  bool
	MountNSUsable bool
	Errors        []string
}

// OK returns true if all preflight checks passed.
func (r PreflightResult) OK() bool {
	return len(r.Errors) == 0
}

// Error returns a combined error message, or empty string if OK.
func (r PreflightResult) Error() string {
	return strings.Join(r.Errors, "; ")
}

// CheckUnsharePreflight verifies that the system supports unprivileged user
// namespaces, which are required for UnshareJail.
func CheckUnsharePreflight() PreflightResult {
	var result PreflightResult

	// 1. Check that unshare binary exists.
	if _, err := exec.LookPath("unshare"); err != nil {
		result.Errors = append(result.Errors, "unshare binary not found (install util-linux)")
	} else {
		result.UnshareFound = true
	}

	// 2. Check unprivileged_userns_clone.
	// If the file doesn't exist, the kernel doesn't gate it (allowed).
	// If it exists and is "0", unprivileged user namespaces are disabled.
	data, err := os.ReadFile("/proc/sys/kernel/unprivileged_userns_clone")
	if err != nil {
		// File doesn't exist — kernel doesn't restrict it.
		result.UserNSEnabled = true
	} else {
		val := strings.TrimSpace(string(data))
		if val == "1" {
			result.UserNSEnabled = true
		} else {
			result.Errors = append(result.Errors,
				fmt.Sprintf("unprivileged user namespaces disabled (kernel.unprivileged_userns_clone=%s); run: sudo sysctl kernel.unprivileged_userns_clone=1", val))
		}
	}

	// 3. Check /etc/subuid and /etc/subgid for current user.
	u, err := user.Current()
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("cannot determine current user: %v", err))
		return result
	}

	result.SubUIDMapped = hasSubIDEntry("/etc/subuid", u.Username)
	if !result.SubUIDMapped {
		result.Errors = append(result.Errors,
			fmt.Sprintf("no /etc/subuid entry for %s; run: sudo usermod --add-subuids 100000-165535 %s", u.Username, u.Username))
	}

	result.SubGIDMapped = hasSubIDEntry("/etc/subgid", u.Username)
	if !result.SubGIDMapped {
		result.Errors = append(result.Errors,
			fmt.Sprintf("no /etc/subgid entry for %s; run: sudo usermod --add-subgids 100000-165535 %s", u.Username, u.Username))
	}

	// 4. Actually attempt the mount-namespace operation UnshareJail relies
	// on. Checks 1-3 above are necessary but not sufficient: some hardened
	// hosts (observed on GitHub Actions ubuntu-latest runners) pass all
	// three yet still refuse the mount-propagation change unshare performs
	// when creating a new mount namespace, failing with:
	//   "unshare: cannot change root filesystem propagation: Permission denied"
	// Only probe if the binary exists — no point invoking a command that's
	// already known to be missing.
	if result.UnshareFound {
		// Mirrors the exact flag set UnshareJail.Command() uses (see
		// unshare.go), swapping the real bash script for a no-op so the
		// probe is cheap and side-effect-free.
		probe := exec.Command("unshare",
			"--fork", "--pid", "--mount",
			"--map-auto", "--map-root-user",
			"--setuid", "0", "--setgid", "0",
			"--", "true",
		)
		out, err := probe.CombinedOutput()
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf(
				"unshare cannot create a usable mount namespace (%s): %s", err, strings.TrimSpace(string(out))))
		} else {
			result.MountNSUsable = true
		}
	}

	return result
}

// hasSubIDEntry checks whether the given username has an entry in a subuid/subgid file.
// Format is: username:start:count (one per line).
func hasSubIDEntry(path, username string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	prefix := username + ":"
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}
