package isolate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/jail"
)

func TestJailWrapShapesTheSpawn(t *testing.T) {
	base := t.TempDir()
	taskDir := t.TempDir()
	roBind := t.TempDir()
	iso := &JailIsolator{Base: base}
	w, err := iso.Wrap(WrapSpec{
		Key: "t1", Bin: "/usr/bin/tool", Argv: []string{"--flag", "value with spaces"},
		TaskDir: taskDir, ROBinds: []string{roBind}, RepoRO: "",
	})
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if w.Bin != "unshare" {
		t.Fatalf("Bin = %q, want unshare", w.Bin)
	}
	script := w.Argv[len(w.Argv)-1] // …, "--", "bash", "-c", script
	if w.Argv[len(w.Argv)-2] != "-c" || w.Argv[len(w.Argv)-3] != "bash" {
		t.Fatalf("argv tail not bash -c <script>: %v", w.Argv)
	}
	for _, wantSub := range []string{
		"mount --bind '" + taskDir + "'", // taskdir rw at host-identical path
		// The cd wrapper is nested inside the outer shell quoting, so the
		// raw script carries the escaped form. Exact quoting is pinned by
		// internal/jail's TestScriptSetChdir; here we just prove Wrap set it.
		"cd '\\''" + taskDir + "'\\''", // §9.3 cwd
		"'/usr/bin/tool'",
		"remount,bind,ro", // some ro remount present (toolchain + roBind)
	} {
		if !strings.Contains(script, wantSub) {
			t.Fatalf("script missing %q:\n%s", wantSub, script)
		}
	}
	wantEnv := map[string]bool{"TMPDIR=/tmp": false, "XDG_CACHE_HOME=/tmp": false}
	for _, e := range w.Env {
		if _, ok := wantEnv[e]; ok {
			wantEnv[e] = true
		}
	}
	for k, seen := range wantEnv {
		if !seen {
			t.Fatalf("Env missing %s: %v", k, w.Env)
		}
	}
	// Cleanup removes the per-task jail root.
	root := filepath.Join(base, "t1")
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("jail root not created: %v", err)
	}
	if err := w.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("jail root survived Cleanup (stat err = %v)", err)
	}
}

func TestJailWrapRejectsMissingROBind(t *testing.T) {
	iso := &JailIsolator{Base: t.TempDir()}
	_, err := iso.Wrap(WrapSpec{
		Key: "t1", Bin: "tool", TaskDir: t.TempDir(),
		ROBinds: []string{"/nonexistent/engine/install"},
	})
	if err == nil || !strings.Contains(err.Error(), "/nonexistent/engine/install") {
		t.Fatalf("err = %v, want loud failure naming the missing ro bind", err)
	}
}

func TestJailWrapLive(t *testing.T) {
	if r := jail.CheckUnsharePreflight(); !r.OK() {
		t.Skipf("userns preflight failed: %s", r.Error())
	}
	shBin, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh not on PATH: %v", err)
	}
	taskDir := t.TempDir()
	iso := &JailIsolator{Base: t.TempDir()}
	// The probe writes into its cwd (must land in taskDir on the host) and
	// reads a path that exists on the host but is NOT mounted in the jail
	// (must be invisible: default-deny reads).
	invisible := t.TempDir()
	if err := os.WriteFile(filepath.Join(invisible, "secret.txt"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	probe := "pwd && echo made-it > made.txt && (ls " + invisible + " 2>/dev/null && echo VISIBLE || echo DENIED)"
	w, err := iso.Wrap(WrapSpec{Key: "live", Bin: shBin, Argv: []string{"-c", probe}, TaskDir: taskDir})
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	defer w.Cleanup()
	out, err := exec.Command(w.Bin, w.Argv...).CombinedOutput()
	if err != nil {
		t.Fatalf("jailed probe: %v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, taskDir) {
		t.Fatalf("cwd inside jail is not the taskdir:\n%s", text)
	}
	if !strings.Contains(text, "DENIED") {
		t.Fatalf("unmounted host path was visible inside the jail:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(taskDir, "made.txt")); err != nil {
		t.Fatalf("write in jail cwd did not land in host taskdir: %v", err)
	}
}
