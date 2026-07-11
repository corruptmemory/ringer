package engine

import (
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/config"
)

func TestResolveBuiltinCodex(t *testing.T) {
	e, err := Resolve(map[string]config.EngineConfig{}, "codex")
	if err != nil {
		t.Fatalf("codex must resolve from builtin: %v", err)
	}
	if e.Bin != "codex" {
		t.Errorf("builtin codex bin = %q", e.Bin)
	}
}

func TestResolveUnknown(t *testing.T) {
	_, err := Resolve(map[string]config.EngineConfig{}, "nope")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unknown engine must error naming it, got %v", err)
	}
}

func TestResolveConfigOverridesBuiltin(t *testing.T) {
	custom := config.EngineConfig{Bin: "/my/codex"}
	e, _ := Resolve(map[string]config.EngineConfig{"codex": custom}, "codex")
	if e.Bin != "/my/codex" {
		t.Errorf("config codex must override builtin, got %q", e.Bin)
	}
}

func TestBuildArgvSubstitution(t *testing.T) {
	e := config.EngineConfig{
		Bin:          "opencode",
		ArgsTemplate: []string{"run", "{spec}", "--dir", "{taskdir}", "-m", "{model}", "{engine_args}", "{sandbox_args}"},
		SandboxArgs:  []string{"--sandbox"},
	}
	bin, argv := BuildArgv(e, "/tmp/task", "build it", "glm-5.2", []string{"--variant", "low"}, false)
	if bin != "opencode" {
		t.Fatalf("bin = %q", bin)
	}
	want := []string{"run", "build it", "--dir", "/tmp/task", "-m", "glm-5.2", "--variant", "low", "--sandbox"}
	if strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv =\n %v\nwant\n %v", argv, want)
	}
}

func TestBuildArgvFullAccessSwapsArgs(t *testing.T) {
	e := config.EngineConfig{
		Bin: "x", ArgsTemplate: []string{"{access_args}"},
		SandboxArgs: []string{"--sbx"}, FullAccessArgs: []string{"--no-sandbox"},
	}
	_, sandboxed := BuildArgv(e, "/t", "s", "", nil, false)
	if len(sandboxed) != 1 || sandboxed[0] != "--sbx" {
		t.Errorf("sandboxed access_args = %v", sandboxed)
	}
	_, full := BuildArgv(e, "/t", "s", "", nil, true)
	if len(full) != 1 || full[0] != "--no-sandbox" {
		t.Errorf("full access_args = %v", full)
	}
}

// Isolation enforcement moved to Select/runner (Plan 3): a jailed engine
// whose bin resolves on PATH must PASS preflight, same as any other engine.
func TestPreflightAcceptsJailWithResolvableBin(t *testing.T) {
	engines := map[string]config.EngineConfig{"j": {Bin: "sh", Isolation: "jail"}}
	err := Preflight(engines, map[string]bool{"j": true})
	if err != nil {
		t.Fatalf("jailed engine with resolvable bin must pass preflight, got %v", err)
	}
}

func TestPreflightMissingBin(t *testing.T) {
	engines := map[string]config.EngineConfig{"x": {Bin: "definitely-not-a-real-binary-xyz"}}
	err := Preflight(engines, map[string]bool{"x": true})
	if err == nil || !strings.Contains(err.Error(), "definitely-not-a-real-binary-xyz") {
		t.Fatalf("missing bin must be reported, got %v", err)
	}
}

// TestParseTokens is the canonical coverage for ParseTokens, merged from the
// engine-side table (basic capture group, empty-regex sentinel, no-match
// sentinel) and the runner-side scrapeTokens it replaces (bad-regex
// sentinel, last-match-wins, last-capture-group vs whole-match fallback,
// TrimSpace on the matched text) after the reviewer adjudicated
// scrapeTokens' semantics as the correct ones to keep as the single
// canonical implementation.
func TestParseTokens(t *testing.T) {
	cases := []struct {
		name       string
		tokenRegex string
		output     string
		want       int64
	}{
		{"basic capture group", `"tokens":\{"total":([0-9]+)`, `blah "tokens":{"total":1234} blah`, 1234},
		{"empty regex sentinel", "", "anything", -1},
		{"bad regex sentinel (unclosed group)", `total=(unclosed`, "anything", -1},
		{"no match sentinel", `total=([0-9]+)`, "no match here", -1},
		{"last match wins over an earlier, smaller one", `total=([0-9]+)`, "total=100 ... total=200 done", 200},
		{"last capture group within a match is used, not the first", `a=([0-9]+) b=([0-9]+)`, "a=1 b=2", 2},
		{"whole-match fallback when the regex has no capture group", `[0-9]+`, "count 42", 42},
		{"leading whitespace inside the capture group is trimmed", `total=(\s*[0-9]+)`, "total=   42", 42},
		{"non-numeric match yields the sentinel", `total=(\w+)`, "total=abc", -1},
		{"grouping commas are stripped (codex 'tokens used\\n75,417')", `tokens\s+used\s*:?\s*([0-9][0-9,]*)`, "diff produced no output.\n\ntokens used\n75,417\ndone", 75417},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseTokens(tc.tokenRegex, tc.output); got != tc.want {
				t.Errorf("ParseTokens(%q, %q) = %d, want %d", tc.tokenRegex, tc.output, got, tc.want)
			}
		})
	}
}
