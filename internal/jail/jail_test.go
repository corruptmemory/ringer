package jail

import (
	"os"
	"path/filepath"
	"testing"
)

func requireRoot(t *testing.T) {
	t.Helper()
	if os.Getuid() != 0 {
		t.Skip("test requires root")
	}
}

func TestBaseMountsCount(t *testing.T) {
	mounts := BaseMounts("/mnt")
	if len(mounts) != 6 {
		t.Fatalf("expected 6 base mounts, got %d", len(mounts))
	}
}

func TestBaseMountsTargets(t *testing.T) {
	prefix := "/mnt"
	mounts := BaseMounts(prefix)

	expected := []string{
		filepath.Join(prefix, "proc"),
		filepath.Join(prefix, "sys"),
		filepath.Join(prefix, "dev"),
		filepath.Join(prefix, "dev/pts"),
		filepath.Join(prefix, "dev/shm"),
		filepath.Join(prefix, "run"),
	}

	if len(mounts) != len(expected) {
		t.Fatalf("expected %d mounts, got %d", len(expected), len(mounts))
	}

	for i, want := range expected {
		if mounts[i].Target != want {
			t.Errorf("mount[%d].Target = %q, want %q", i, mounts[i].Target, want)
		}
	}
}

func TestBindMountReadOnly(t *testing.T) {
	m := BindMount("/src", "/dst", true)
	if !m.ReadOnly {
		t.Error("expected ReadOnly to be true")
	}
	if m.Source != "/src" {
		t.Errorf("Source = %q, want %q", m.Source, "/src")
	}
	if m.Target != "/dst" {
		t.Errorf("Target = %q, want %q", m.Target, "/dst")
	}
}

func TestBindMountReadWrite(t *testing.T) {
	m := BindMount("/src", "/dst", false)
	if m.ReadOnly {
		t.Error("expected ReadOnly to be false")
	}
}

func TestConditionalMountSkipped(t *testing.T) {
	m := Mount{
		Source: "test",
		Target: "/test",
		Condition: func() bool {
			return false
		},
	}
	if m.ShouldMount() {
		t.Error("expected ShouldMount() to return false")
	}
}

func TestConditionalMountApplied(t *testing.T) {
	m := Mount{
		Source: "test",
		Target: "/test",
		Condition: func() bool {
			return true
		},
	}
	if !m.ShouldMount() {
		t.Error("expected ShouldMount() to return true")
	}

	// Also test nil condition (should default to true)
	m2 := Mount{
		Source: "test",
		Target: "/test",
	}
	if !m2.ShouldMount() {
		t.Error("expected ShouldMount() to return true when Condition is nil")
	}
}

func TestJailSetupTeardownAsRoot(t *testing.T) {
	requireRoot(t)

	root := t.TempDir()
	j := NewRootJail(root)

	err := j.Setup(nil)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	points := j.MountPoints()
	if len(points) < 6 {
		t.Errorf("expected at least 6 mount points, got %d: %v", len(points), points)
	}

	err = j.Teardown()
	if err != nil {
		t.Fatalf("Teardown failed: %v", err)
	}

	points = j.MountPoints()
	if len(points) != 0 {
		t.Errorf("expected 0 mount points after teardown, got %d", len(points))
	}
}

func TestJailBindMountAsRoot(t *testing.T) {
	requireRoot(t)

	root := t.TempDir()
	srcDir := t.TempDir()

	// Create a test file in the source directory.
	testFile := filepath.Join(srcDir, "hello.txt")
	err := os.WriteFile(testFile, []byte("hello from bind mount"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	j := NewRootJail(root)

	targetDir := filepath.Join(root, "bound")
	mounts := []Mount{
		BindMount(srcDir, targetDir, true),
	}

	err = j.Setup(mounts)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer j.Teardown()

	// Verify file is visible inside jail.
	boundFile := filepath.Join(targetDir, "hello.txt")
	data, err := os.ReadFile(boundFile)
	if err != nil {
		t.Fatalf("failed to read bound file: %v", err)
	}
	if string(data) != "hello from bind mount" {
		t.Errorf("bound file contents = %q, want %q", string(data), "hello from bind mount")
	}

	// Verify read-only prevents writes.
	err = os.WriteFile(filepath.Join(targetDir, "should-fail.txt"), []byte("nope"), 0644)
	if err == nil {
		t.Error("expected write to read-only bind mount to fail, but it succeeded")
	}
}
