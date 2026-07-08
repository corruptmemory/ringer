package jail

import (
	"fmt"
	"os"
	"path/filepath"
)

// Mount represents a single mount operation.
type Mount struct {
	Source    string
	Target    string
	Options   []string
	ReadOnly  bool
	FSType    string      // if set, use "mount -t FSType" instead of bind mount
	Condition func() bool // if non-nil, mount is skipped when false
}

// ShouldMount returns true if Condition is nil or returns true.
func (m Mount) ShouldMount() bool {
	if m.Condition == nil {
		return true
	}
	return m.Condition()
}

// BaseMounts returns the 7 pseudo-fs mounts needed for a working chroot,
// using the same mount options as arch-chroot.
func BaseMounts(targetPrefix string) []Mount {
	return []Mount{
		{
			Source:  "proc",
			Target:  filepath.Join(targetPrefix, "proc"),
			Options: []string{"-t", "proc", "-o", "nosuid,noexec,nodev"},
		},
		{
			Source:  "sys",
			Target:  filepath.Join(targetPrefix, "sys"),
			Options: []string{"-t", "sysfs", "-o", "nosuid,noexec,nodev,ro"},
		},
		{
			Source:  "udev",
			Target:  filepath.Join(targetPrefix, "dev"),
			Options: []string{"-t", "devtmpfs", "-o", "mode=0755,nosuid"},
		},
		{
			Source:  "devpts",
			Target:  filepath.Join(targetPrefix, "dev/pts"),
			Options: []string{"-t", "devpts", "-o", "mode=0620,gid=5,nosuid,noexec"},
		},
		{
			Source:  "shm",
			Target:  filepath.Join(targetPrefix, "dev/shm"),
			Options: []string{"-t", "tmpfs", "-o", "mode=1777,nosuid,nodev"},
		},
		{
			Source:  "run",
			Target:  filepath.Join(targetPrefix, "run"),
			Options: []string{"-t", "tmpfs", "-o", "nosuid,nodev,mode=0755"},
		},
		// Note: /tmp is intentionally NOT mounted as tmpfs here.
		// Stub scripts are copied into $root/tmp/stubs/ before commands run,
		// and a tmpfs mount would shadow them.
	}
}

// HostMounts returns read-only bind mounts for the host toolchain
// (/usr, /etc) plus symlink-or-bind entries for /bin, /lib, /lib64, /sbin.
// These provide bash, git, go, and other tools inside the chroot.
func HostMounts(targetPrefix string) []Mount {
	mounts := []Mount{
		BindMount("/usr", filepath.Join(targetPrefix, "usr"), true),
		BindMount("/etc", filepath.Join(targetPrefix, "etc"), true),
	}

	// On merged-usr distros (Arch, Fedora) /bin, /lib, /lib64, /sbin are
	// symlinks into /usr. On traditional layouts they're real directories.
	// Bind-mount real directories; skip symlinks (they'll be created as
	// part of the jail setup since bind-mounting a symlink doesn't work).
	for _, dir := range []string{"/bin", "/lib", "/lib64", "/sbin"} {
		mounts = append(mounts, conditionalDirMount(dir, filepath.Join(targetPrefix, dir)))
	}

	return mounts
}

// conditionalDirMount creates a read-only bind mount that only activates
// if source is a real directory (not a symlink).
func conditionalDirMount(source, target string) Mount {
	return Mount{
		Source:   source,
		Target:   target,
		Options:  []string{"-o", "bind"},
		ReadOnly: true,
		Condition: func() bool {
			info, err := os.Lstat(source)
			return err == nil && info.IsDir()
		},
	}
}

// createHostSymlinks creates symlinks in the jail root for directories that
// are symlinks on the host (e.g., /bin → usr/bin on merged-usr systems).
// Reads the actual symlink target from the host rather than hardcoding it,
// so it works on Arch (/lib64 → usr/lib), Fedora (/lib64 → usr/lib64), etc.
func createHostSymlinks(root string) error {
	for _, path := range []string{"/bin", "/lib", "/lib64", "/sbin"} {
		hostInfo, err := os.Lstat(path)
		if err != nil || hostInfo.Mode()&os.ModeSymlink == 0 {
			continue // doesn't exist or not a symlink on host, skip
		}
		target, err := os.Readlink(path)
		if err != nil {
			continue
		}
		jailPath := filepath.Join(root, path)
		os.Remove(jailPath)
		if err := os.Symlink(target, jailPath); err != nil {
			return fmt.Errorf("symlink %s → %s: %w", jailPath, target, err)
		}
	}
	return nil
}

// TmpfsMount creates a tmpfs mount at the given target path.
func TmpfsMount(target string) Mount {
	return Mount{
		Source: "tmpfs",
		Target: target,
		FSType: "tmpfs",
	}
}

// BindMount creates a bind mount entry. If readOnly is true, the mount
// method will first bind, then remount as read-only.
func BindMount(source, target string, readOnly bool) Mount {
	return Mount{
		Source:   source,
		Target:   target,
		Options:  []string{"-o", "bind"},
		ReadOnly: readOnly,
	}
}
