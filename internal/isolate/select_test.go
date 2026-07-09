package isolate

import (
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/jail"
	"github.com/corruptmemory/ringer/internal/logging"
)

// Select picks by capability, so the assertable surface on any given host
// is: it returns SOMETHING sensible for THIS host, scratch roots land
// under the workdir, and the returned backend matches the host's actual
// capabilities.
func TestSelectMatchesHostCapabilities(t *testing.T) {
	workdir := t.TempDir()
	iso, err := Select(logging.Default(), workdir, "/opt/ringer/ringer")
	jailOK := jail.CheckUnsharePreflight().OK()
	_, landlockOK := LandlockABI()
	switch {
	case jailOK:
		if err != nil {
			t.Fatalf("jail available but Select failed: %v", err)
		}
		j, ok := iso.(*JailIsolator)
		if !ok {
			t.Fatalf("iso = %T, want *JailIsolator on a userns-capable host", iso)
		}
		if j.Base != filepath.Join(workdir, ".jail") {
			t.Fatalf("jail Base = %q", j.Base)
		}
	case landlockOK:
		if err != nil {
			t.Fatalf("landlock available but Select failed: %v", err)
		}
		l, ok := iso.(*LandlockIsolator)
		if !ok {
			t.Fatalf("iso = %T, want *LandlockIsolator fallback", iso)
		}
		if l.Self != "/opt/ringer/ringer" || l.ScratchDir != filepath.Join(workdir, ".scratch") {
			t.Fatalf("landlock fields = %+v", l)
		}
	default:
		if err == nil {
			t.Fatalf("no backend available but Select returned %T", iso)
		}
	}
}
