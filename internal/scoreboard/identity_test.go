package scoreboard

import "testing"

const testRegistry = `
[engines.codex]
harness = "Codex CLI"
access = "OAuth plan"
default_model_key = "gpt-5.5"
[engines.codex.models."gpt-5.5"]
display = "GPT-5.5"
confidence = "verified"
source = "x"

[engines.opencode]
harness = "OpenCode"
access = "OpenRouter API"
[engines.opencode.models."openrouter/z-ai/glm-5.2"]
display = "GLM 5.2"
confidence = "verified"
`

func TestResolve(t *testing.T) {
	reg, err := ParseRegistry([]byte(testRegistry))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		engine, key                       string
		wantDisplay, wantHarness, wantAcc string
	}{
		{"codex", "gpt-5.5", "GPT-5.5", "Codex CLI", "OAuth plan"},                        // listed
		{"codex", "", "GPT-5.5", "Codex CLI", "OAuth plan"},                               // engine default_model_key
		{"opencode", "openrouter/z-ai/glm-5.2", "GLM 5.2", "OpenCode", "OpenRouter API"},  // listed
		{"opencode", "openrouter/x/unlisted", "x/unlisted", "OpenCode", "OpenRouter API"}, // prefix-strip fallback
		{"ghost", "whatever", "ghost", "ghost", "unknown"},                                // unknown engine
	}
	for _, c := range cases {
		got := reg.Resolve(c.engine, c.key)
		if got.ModelDisplay != c.wantDisplay || got.Harness != c.wantHarness || got.Access != c.wantAcc {
			t.Errorf("Resolve(%q,%q)=%+v want display=%q harness=%q access=%q",
				c.engine, c.key, got, c.wantDisplay, c.wantHarness, c.wantAcc)
		}
	}
}

func TestLoadRegistryEmbeddedFallback(t *testing.T) {
	// Empty override path -> embedded registry loads and resolves the shipped codex default.
	reg := LoadRegistry("")
	if got := reg.Resolve("codex", "gpt-5.5"); got.Harness == "" || got.Harness == "codex" {
		t.Fatalf("embedded registry did not load codex identity: %+v", got)
	}
}

// TestLoadRegistryMissingOverrideIsEmpty guards the read-failure path: a
// non-empty override path that can't be read must degrade to an EMPTY registry
// (raw-slug fallback), NOT silently fall back to the embedded registry. If it
// leaked the embedded default, Resolve("codex","gpt-5.5") would return the
// shipped "GPT-5.5"/"Codex CLI" identity instead of the unknown fallback.
func TestLoadRegistryMissingOverrideIsEmpty(t *testing.T) {
	reg := LoadRegistry("/no/such/registry.toml")
	got := reg.Resolve("codex", "gpt-5.5")
	if got.ModelDisplay != "codex" || got.Harness != "codex" || got.Access != "unknown" {
		t.Fatalf("missing override should yield empty registry (unknown fallback), got embedded/other: %+v", got)
	}
}

// TestResolveEngineMetaFallback covers the engine-meta fallback branch: a
// listed engine but an unlisted model key resolves to the raw key as display,
// the engine's harness/access, and confidence "fallback".
func TestResolveEngineMetaFallback(t *testing.T) {
	reg, err := ParseRegistry([]byte(testRegistry))
	if err != nil {
		t.Fatal(err)
	}
	got := reg.Resolve("codex", "some-unlisted-key")
	if got.ModelDisplay != "some-unlisted-key" || got.Harness != "Codex CLI" || got.Confidence != "fallback" {
		t.Fatalf("engine-meta fallback wrong: %+v", got)
	}
}
