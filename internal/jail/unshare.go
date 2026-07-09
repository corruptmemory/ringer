package jail

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// UnshareJail manages a chroot environment using unprivileged user namespaces.
// No root privileges required. Mounts are deferred to Command() time and
// live inside the namespace — they vanish when the process exits.
type UnshareJail struct {
	root     string
	mounts   []Mount
	dropUser string // if set, use runuser -u <user> before exec
	chdir    string // if set, cd here (post-chroot path) before exec
}

// NewUnshareJail creates a new UnshareJail rooted at the given directory.
// Call CheckUnsharePreflight() before using this to verify system support.
func NewUnshareJail(root string) *UnshareJail {
	return &UnshareJail{root: root}
}

// SetDropUser configures the jail to use runuser to switch to the given
// username before exec'ing the command. This is required when the jailed
// process refuses to run as root (e.g. Claude Code's permission bypass
// safety check). The username must exist in /etc/passwd inside the jail.
func (j *UnshareJail) SetDropUser(username string) {
	j.dropUser = username
}

// SetChdir configures the working directory the jailed command starts in,
// as an in-jail (post-chroot) path. chroot(1) leaves the child's cwd at the
// new root ("/"); ringer's spawn contract requires cwd = taskdir, so the
// script wraps the final exec in `/bin/sh -c 'cd <dir> && exec …'` when
// this is set.
func (j *UnshareJail) SetChdir(dir string) {
	j.chdir = dir
}

// Root returns the jail's root directory path.
func (j *UnshareJail) Root() string {
	return j.root
}

// Setup records the mount table and creates the root directory structure.
// Actual mounts are deferred to Command() time (they run inside the namespace).
func (j *UnshareJail) Setup(mounts []Mount) error {
	if err := os.MkdirAll(j.root, 0755); err != nil {
		return fmt.Errorf("create jail root: %w", err)
	}

	j.mounts = make([]Mount, len(mounts))
	copy(j.mounts, mounts)

	// Pre-create target directories for bind mounts so files can be
	// copied into the jail root before Command() is called (e.g. stubs).
	for _, m := range mounts {
		if m.Condition != nil && !m.Condition() {
			continue
		}
		if err := os.MkdirAll(m.Target, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", m.Target, err)
		}
	}

	return nil
}

// Command returns an *exec.Cmd that creates a user namespace, performs all
// mounts inside it, then chroots and runs the given command.
func (j *UnshareJail) Command(name string, args ...string) *exec.Cmd {
	script := j.buildScript(name, args...)

	return exec.Command("unshare",
		"--fork", "--pid", "--mount",
		"--map-auto", "--map-root-user",
		"--setuid", "0", "--setgid", "0",
		"--", "bash", "-c", script,
	)
}

// Script returns the bash script that sets up mounts, chroots, and execs the given command.
// Use with UnshareArgs() to run via tmux or other external process managers.
func (j *UnshareJail) Script(name string, args ...string) string {
	return j.buildScript(name, args...)
}

// UnshareArgs returns the unshare flags (without the trailing bash -c).
// Combine with Script() written to a file: unshare <flags> -- bash <script-file>
func (j *UnshareJail) UnshareArgs() []string {
	return []string{
		"--fork", "--pid", "--mount",
		"--map-auto", "--map-root-user",
		"--setuid", "0", "--setgid", "0",
	}
}

// Teardown is a no-op for UnshareJail. Mounts live inside the namespace
// and are cleaned up when the unshare process exits.
func (j *UnshareJail) Teardown() error {
	return nil
}

// buildScript generates a bash script that sets up mounts and chroots.
func (j *UnshareJail) buildScript(name string, args ...string) string {
	var sb strings.Builder
	root := j.root

	sb.WriteString("set -e\n")

	// Self-bind the root to make it a mountpoint (arch-chroot pattern).
	fmt.Fprintf(&sb, "mount --bind %s %s\n", shellQuote(root), shellQuote(root))

	// Base mounts: unshare-style (lighter than root-mode).
	j.writeUnshareMounts(&sb, root)

	// Agent-specific bind mounts recorded from Setup().
	for _, m := range j.mounts {
		if m.Condition != nil && !m.Condition() {
			continue
		}
		fmt.Fprintf(&sb, "mkdir -p %s\n", shellQuote(m.Target))
		if m.FSType != "" {
			fmt.Fprintf(&sb, "mount -t %s %s %s\n", m.FSType, m.Source, shellQuote(m.Target))
		} else {
			fmt.Fprintf(&sb, "mount --bind %s %s\n", shellQuote(m.Source), shellQuote(m.Target))
			if m.ReadOnly {
				fmt.Fprintf(&sb, "mount -o remount,bind,ro %s\n", shellQuote(m.Target))
			}
		}
	}

	// Chroot and exec. If drop privileges configured, use runuser to
	// switch identity. runuser doesn't need PAM and works inside user
	// namespaces — it changes effective UID/GID without requiring nested
	// namespaces or setpriv UID remapping.
	cmdParts := []string{shellQuote(name)}
	for _, a := range args {
		cmdParts = append(cmdParts, shellQuote(a))
	}
	target := strings.Join(cmdParts, " ")
	if j.chdir != "" {
		// One bash word: the inner sh sees `cd '<dir>' && exec '<cmd>' …`.
		target = "/bin/sh -c " + shellQuote(fmt.Sprintf("cd %s && exec %s", shellQuote(j.chdir), strings.Join(cmdParts, " ")))
	}
	if j.dropUser != "" {
		// Make all PTY devices accessible before dropping privileges.
		// runuser changes to a UID that doesn't own the PTY in the namespace.
		fmt.Fprintf(&sb, "chmod 666 %s/dev/pts/* 2>/dev/null || true\n", shellQuote(root))
		fmt.Fprintf(&sb, "exec chroot %s runuser -u %s -- %s\n", shellQuote(root), j.dropUser, target)
	} else {
		fmt.Fprintf(&sb, "exec chroot %s %s\n", shellQuote(root), target)
	}

	return sb.String()
}

// writeUnshareMounts writes the base mount commands for the unshare path,
// mirroring arch-chroot's unshare_setup().
func (j *UnshareJail) writeUnshareMounts(sb *strings.Builder, root string) {
	// proc
	target := filepath.Join(root, "proc")
	fmt.Fprintf(sb, "mkdir -p %s\n", shellQuote(target))
	fmt.Fprintf(sb, "mount -t proc proc %s -o nosuid,noexec,nodev\n", shellQuote(target))

	// sys (rbind)
	target = filepath.Join(root, "sys")
	fmt.Fprintf(sb, "mkdir -p %s\n", shellQuote(target))
	fmt.Fprintf(sb, "mount --rbind /sys %s\n", shellQuote(target))

	// dev — bind individual device nodes instead of devtmpfs
	devDir := filepath.Join(root, "dev")
	fmt.Fprintf(sb, "mkdir -p %s\n", shellQuote(devDir))

	devices := []string{"full", "null", "random", "tty", "urandom", "zero"}
	for _, dev := range devices {
		devPath := filepath.Join(devDir, dev)
		fmt.Fprintf(sb, "touch %s && mount --bind /dev/%s %s\n",
			shellQuote(devPath), dev, shellQuote(devPath))
	}

	// devpts — needed for PTY access (e.g. Claude Code's TUI)
	ptsDir := filepath.Join(devDir, "pts")
	fmt.Fprintf(sb, "mkdir -p %s\n", shellQuote(ptsDir))
	fmt.Fprintf(sb, "mount --rbind /dev/pts %s\n", shellQuote(ptsDir))

	// dev symlinks (-n prevents dereferencing existing symlinks on re-runs)
	fmt.Fprintf(sb, "ln -sfn /proc/self/fd %s\n", shellQuote(filepath.Join(devDir, "fd")))
	fmt.Fprintf(sb, "ln -sfn /proc/self/fd/0 %s\n", shellQuote(filepath.Join(devDir, "stdin")))
	fmt.Fprintf(sb, "ln -sfn /proc/self/fd/1 %s\n", shellQuote(filepath.Join(devDir, "stdout")))
	fmt.Fprintf(sb, "ln -sfn /proc/self/fd/2 %s\n", shellQuote(filepath.Join(devDir, "stderr")))

	// Host toolchain — bind-mount /usr and /etc read-only so binaries
	// (bash, git, go, etc.) and config are available inside the chroot.
	for _, dir := range []string{"/usr", "/etc"} {
		target = filepath.Join(root, dir)
		fmt.Fprintf(sb, "mkdir -p %s\n", shellQuote(target))
		fmt.Fprintf(sb, "mount --rbind %s %s\n", dir, shellQuote(target))
		fmt.Fprintf(sb, "mount -o remount,bind,ro %s\n", shellQuote(target))
	}
	// On many distros (Arch, Fedora) /bin, /lib, /lib64, /sbin are symlinks
	// into /usr. Recreate them using the host's actual symlink target (via
	// readlink) so this works on Arch (/lib64→usr/lib), Fedora
	// (/lib64→usr/lib64), etc. On traditional layouts (Debian, Alpine)
	// they're real directories and get bind-mounted instead.
	for _, dir := range []string{"/bin", "/lib", "/lib64", "/sbin"} {
		jailPath := filepath.Join(root, dir)
		fmt.Fprintf(sb, "if [ -L %s ]; then ln -sfn \"$(readlink %s)\" %s; elif [ -d %s ]; then mkdir -p %s && mount --rbind %s %s && mount -o remount,bind,ro %s; fi\n",
			dir, dir, shellQuote(jailPath),
			dir, shellQuote(jailPath), dir, shellQuote(jailPath), shellQuote(jailPath))
	}

	// run (tmpfs)
	target = filepath.Join(root, "run")
	fmt.Fprintf(sb, "mkdir -p %s\n", shellQuote(target))
	fmt.Fprintf(sb, "mount -t tmpfs run %s -o nosuid,nodev,mode=0755\n", shellQuote(target))

	// Note: we intentionally do NOT mount tmpfs on /tmp. Files (like stub
	// scripts) are copied into $root/tmp/ before Command() runs, and a tmpfs
	// mount would shadow them. The namespace provides isolation already.
}

// shellQuote wraps a string in single quotes, escaping embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
