//go:build spike

// internal/jail/spike_opencode_test.go
//
// SPIKE (design spec §13.1): does the opencode CLI tolerate running as
// namespace-UID-0? If yes, jailed workers need no SetDropUser, files land
// owned by the invoking user, and the UID-mapping cleanup tax vanishes.
//
// Run manually: go test -tags=spike -run TestSpikeOpencodeNsRoot -v ./internal/jail/
//
// This machine's ground truth (verified by the controller, not the paraphrased
// brief): opencode lives at the absolute path /home/jim/.opencode/bin/opencode
// (v1.17.15), NOT on PATH — exec.LookPath("opencode") would wrongly skip this
// test. Guard on os.Stat of the absolute path instead. opencode is already
// authenticated (OpenCode Zen), so --version needs no auth.
package jail

import (
	"os"
	"path/filepath"
	"testing"
)

// opencodeBin is the absolute host path to the opencode binary on this
// machine. It is not on PATH, so exec.LookPath would incorrectly skip.
const opencodeBin = "/home/jim/.opencode/bin/opencode"

// opencodeHome is the host directory containing the opencode binary plus its
// bundled node_modules. Bind-mounting this (read-only) at the same absolute
// path inside the jail is the cleanest way to make opencode reachable by its
// host-identical path from inside the chroot.
const opencodeHome = "/home/jim/.opencode"

func TestSpikeOpencodeNsRoot(t *testing.T) {
	if _, err := os.Stat(opencodeBin); err != nil {
		t.Skipf("opencode not installed at %s: %v", opencodeBin, err)
	}
	if r := CheckUnsharePreflight(); !r.OK() {
		t.Skipf("userns preflight failed: %s", r.Error())
	}

	root := t.TempDir()
	hostWorkspace := t.TempDir()

	j := NewUnshareJail(root)

	// Assemble: read-only host toolchain, a writable workspace bind, a
	// tmpfs scratch, and a read-only bind of the opencode install so it's
	// reachable inside the jail at its host-identical absolute path.
	mounts := append(HostMounts(root),
		BindMount(hostWorkspace, filepath.Join(root, "workspace"), false),
		TmpfsMount(filepath.Join(root, "tmp")),
		BindMount(opencodeHome, filepath.Join(root, opencodeHome), true),
	)

	// NOTE: no SetDropUser — that is the whole experiment. We run as
	// namespace-UID-0 (ns-root) throughout.
	if err := j.Setup(mounts); err != nil {
		t.Fatalf("jail setup: %v", err)
	}
	defer j.Teardown()

	// Probe 1: does opencode start at all as ns-root?
	out, err := j.Command(opencodeBin, "--version").CombinedOutput()
	t.Logf("opencode --version (ns-root):\nerr=%v\n%s", err, out)

	// Probe 2: id inside the jail, for the record — what does ns-root map to?
	out2, idErr := j.Command("id").CombinedOutput()
	t.Logf("id inside jail: err=%v\n%s", idErr, out2)

	if err != nil {
		t.Logf("VERDICT: opencode refuses ns-root (record in findings doc): %v", err)
		return
	}
	t.Log("VERDICT: opencode tolerates ns-root — SetDropUser not needed for the opencode lane")
}
