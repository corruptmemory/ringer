package config

import (
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
