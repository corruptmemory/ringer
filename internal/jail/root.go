package jail

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

const (
	umountRetries    = 3
	umountRetryDelay = 500 * time.Millisecond
)

// RootJail manages a chroot environment using direct mount syscalls.
// Requires root privileges (or sudo).
type RootJail struct {
	root        string
	mountPoints []string
}

// NewRootJail creates a new RootJail rooted at the given directory.
func NewRootJail(root string) *RootJail {
	return &RootJail{root: root}
}

// Root returns the jail's root directory path.
func (j *RootJail) Root() string {
	return j.root
}

// Setup creates the chroot directory structure, applies BaseMounts plus any
// additional mounts provided. On error, it tears down everything mounted so far.
func (j *RootJail) Setup(mounts []Mount) (err error) {
	defer func() {
		if err != nil {
			if teardownErr := j.Teardown(); teardownErr != nil {
				err = fmt.Errorf("setup error: %w; additionally teardown failed: %v", err, teardownErr)
			}
		}
	}()

	allMounts := BaseMounts(j.root)
	allMounts = append(allMounts, HostMounts(j.root)...)
	allMounts = append(allMounts, mounts...)

	for _, m := range allMounts {
		if !m.ShouldMount() {
			continue
		}
		if mkdirErr := os.MkdirAll(m.Target, 0755); mkdirErr != nil {
			err = fmt.Errorf("mkdir %s: %w", m.Target, mkdirErr)
			return
		}
		if mountErr := j.mount(m); mountErr != nil {
			err = fmt.Errorf("mount %s on %s: %w", m.Source, m.Target, mountErr)
			return
		}
	}

	// On merged-usr systems, create symlinks for /bin, /lib, etc.
	if err = createHostSymlinks(j.root); err != nil {
		return
	}

	return nil
}

// mount performs a single mount operation. For read-only bind mounts, it first
// mounts with bind, then remounts with bind,ro.
func (j *RootJail) mount(m Mount) error {
	args := append([]string{m.Source, m.Target}, m.Options...)
	cmd := exec.Command("mount", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, output)
	}
	j.mountPoints = append(j.mountPoints, m.Target)

	// For read-only bind mounts: remount with bind,ro
	if m.ReadOnly {
		remountArgs := []string{m.Target, "-o", "remount,bind,ro"}
		cmd = exec.Command("mount", remountArgs...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("remount read-only: %w: %s", err, output)
		}
	}
	return nil
}

// Command returns an *exec.Cmd that runs the given command inside the chroot.
func (j *RootJail) Command(name string, args ...string) *exec.Cmd {
	cmdArgs := []string{j.root, name}
	cmdArgs = append(cmdArgs, args...)
	return exec.Command("chroot", cmdArgs...)
}

// Teardown unmounts all tracked mount points in reverse order with retry.
func (j *RootJail) Teardown() error {
	var errs []error

	for i := len(j.mountPoints) - 1; i >= 0; i-- {
		target := j.mountPoints[i]
		var lastErr error
		for attempt := 0; attempt < umountRetries; attempt++ {
			cmd := exec.Command("umount", "-R", target)
			if output, err := cmd.CombinedOutput(); err != nil {
				lastErr = fmt.Errorf("umount -R %s: %w: %s", target, err, output)
				time.Sleep(umountRetryDelay)
				continue
			}
			lastErr = nil
			break
		}
		if lastErr != nil {
			errs = append(errs, lastErr)
		}
	}

	j.mountPoints = nil

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// MountPoints returns a copy of the tracked mount points for debugging.
func (j *RootJail) MountPoints() []string {
	result := make([]string, len(j.mountPoints))
	copy(result, j.mountPoints)
	return result
}
