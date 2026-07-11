package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestRenderDocumentedRoundTrips(t *testing.T) {
	out, err := RenderDocumented(ExampleConfig())
	if err != nil {
		t.Fatal(err)
	}
	// It must contain comments (from doc tags) and section headers.
	for _, want := range []string{"# ", "[engines.codex]", "[engines.opencode]", "[logging]", "level = \"INFO\"", "allow_full_access ="} {
		if !strings.Contains(out, want) {
			t.Errorf("generated config missing %q:\n%s", want, out)
		}
	}
	// And it must decode back into an AppConfig with the strict loader's rules
	// (no unknown keys), proving the generated keys are all real.
	var c AppConfig
	md, err := toml.Decode(out, &c)
	if err != nil {
		t.Fatalf("generated config did not decode: %v", err)
	}
	if u := md.Undecoded(); len(u) != 0 {
		t.Fatalf("generated config has keys the struct doesn't accept: %v", u)
	}
	if _, ok := c.Engines["codex"]; !ok {
		t.Fatalf("round-tripped config lost the codex engine")
	}
	// slog.Level rendered as text, not int (a bare int wouldn't UnmarshalText).
	if strings.Contains(out, "level = 0") {
		t.Fatalf("slog.Level rendered as int, not text")
	}
	// *bool (artifact.enabled) rendered as a bool literal, not a pointer/nil.
	if !strings.Contains(out, "enabled = true") {
		t.Fatalf("*bool did not render as a bool literal:\n%s", out)
	}
	// []string (args_template) rendered as a TOML array.
	if !strings.Contains(out, "args_template = [") {
		t.Fatalf("[]string did not render as an array:\n%s", out)
	}
}

// TestRenderDocumentedLoadsCleanly hardens the drift-lock: the generated config
// must be a *valid loadable* config, not merely toml-decodable. It runs the
// output through the real config.Load, which additionally validates values
// (isolation in {"","none","jail"}, logging.format in {"","text","json"}) —
// so a future doc/example change producing an invalid isolation/format is
// caught here instead of only at manual smoke.
func TestRenderDocumentedLoadsCleanly(t *testing.T) {
	out, err := RenderDocumented(ExampleConfig())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("generated config failed the strict loader: %v\n%s", err, out)
	}
	// Spot-check that values survived the real loader, not just decoded.
	if got := c.Engines["codex"].Bin; got != "codex" {
		t.Errorf("codex engine Bin = %q, want \"codex\"", got)
	}
	if got := c.HudPort(); got != 8700 {
		t.Errorf("HudPort() = %d, want 8700", got)
	}
}
