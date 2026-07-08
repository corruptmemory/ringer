package jail

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func requireUnshare(t *testing.T) {
	t.Helper()
	result := CheckUnsharePreflight()
	if !result.OK() {
		t.Skipf("unshare not available: %s", result.Error())
	}
}

func TestPreflightReportsStatus(t *testing.T) {
	result := CheckUnsharePreflight()
	// We don't assert pass/fail since it depends on the system,
	// but we verify the struct is populated sensibly.
	t.Logf("UnshareFound=%v UserNSEnabled=%v SubUIDMapped=%v SubGIDMapped=%v MountNSUsable=%v OK=%v",
		result.UnshareFound, result.UserNSEnabled, result.SubUIDMapped, result.SubGIDMapped, result.MountNSUsable, result.OK())
	if !result.OK() {
		t.Logf("Errors: %s", result.Error())
	}
}

func TestUnshareJailSetupCreatesRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "jail")
	j := NewUnshareJail(root)

	if err := j.Setup(nil); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	if _, err := os.Stat(root); err != nil {
		t.Errorf("jail root not created: %v", err)
	}
}

func TestUnshareJailSetupCreatesBindTargets(t *testing.T) {
	root := filepath.Join(t.TempDir(), "jail")
	j := NewUnshareJail(root)

	mounts := []Mount{
		BindMount("/tmp", filepath.Join(root, "workspace"), false),
	}
	if err := j.Setup(mounts); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "workspace")); err != nil {
		t.Errorf("bind target dir not created: %v", err)
	}
}

func TestUnshareJailCommandShape(t *testing.T) {
	root := filepath.Join(t.TempDir(), "jail")
	j := NewUnshareJail(root)
	j.Setup(nil)

	cmd := j.Command("bash", "/tmp/stubs/builder.sh", "/workspace", "/handoff")

	// Verify it's an unshare command.
	if cmd.Path == "" || !strings.HasSuffix(cmd.Path, "unshare") {
		// cmd.Path is the resolved path; check Args[0] instead.
		if cmd.Args[0] != "unshare" {
			t.Errorf("expected unshare command, got %q", cmd.Args[0])
		}
	}

	// Verify namespace flags are present.
	argStr := strings.Join(cmd.Args, " ")
	for _, flag := range []string{"--fork", "--pid", "--mount", "--map-auto", "--map-root-user"} {
		if !strings.Contains(argStr, flag) {
			t.Errorf("missing flag %q in command: %s", flag, argStr)
		}
	}

	// The last arg is the bash -c script, which should contain chroot and our command.
	script := cmd.Args[len(cmd.Args)-1]
	if !strings.Contains(script, "chroot") {
		t.Error("script does not contain chroot")
	}
	if !strings.Contains(script, "builder.sh") {
		t.Error("script does not contain the command name")
	}
}

func TestUnshareJailTeardownIsNoop(t *testing.T) {
	root := filepath.Join(t.TempDir(), "jail")
	j := NewUnshareJail(root)
	j.Setup(nil)

	// Teardown should always succeed (it's a no-op).
	if err := j.Teardown(); err != nil {
		t.Errorf("Teardown: %v", err)
	}
}

func TestUnshareJailRunsCommand(t *testing.T) {
	requireUnshare(t)

	root := t.TempDir()
	j := NewUnshareJail(root)

	// Bind-mount host directories so bash and libraries are available
	// inside the chroot. These are read-only — we're just providing
	// the toolchain.
	mounts := []Mount{
		BindMount("/usr", filepath.Join(root, "usr"), true),
		BindMount("/lib", filepath.Join(root, "lib"), true),
		BindMount("/lib64", filepath.Join(root, "lib64"), true),
		BindMount("/etc", filepath.Join(root, "etc"), true),
		BindMount("/bin", filepath.Join(root, "bin"), true),
	}

	// Only mount /lib64 if it exists (not all distros have it).
	mounts[2].Condition = func() bool {
		_, err := os.Stat("/lib64")
		return err == nil
	}

	// /bin may be a symlink to /usr/bin — only mount if it's a real dir.
	mounts[4].Condition = func() bool {
		info, err := os.Lstat("/bin")
		return err == nil && info.IsDir()
	}

	if err := j.Setup(mounts); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// Run a simple command inside the jail.
	cmd := j.Command("/usr/bin/bash", "-c", "echo hello")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Command.Run: %v\nOutput: %s", err, out)
	}

	got := strings.TrimSpace(string(out))
	if got != "hello" {
		t.Errorf("output = %q, want %q", got, "hello")
	}
}
