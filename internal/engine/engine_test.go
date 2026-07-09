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

func TestPreflightRejectsJail(t *testing.T) {
	engines := map[string]config.EngineConfig{"j": {Bin: "sh", Isolation: "jail"}}
	err := Preflight(engines, map[string]bool{"j": true})
	if err == nil || !strings.Contains(err.Error(), "Plan 3") {
		t.Fatalf("jail isolation must be rejected in Plan 2, got %v", err)
	}
}

func TestPreflightMissingBin(t *testing.T) {
	engines := map[string]config.EngineConfig{"x": {Bin: "definitely-not-a-real-binary-xyz"}}
	err := Preflight(engines, map[string]bool{"x": true})
	if err == nil || !strings.Contains(err.Error(), "definitely-not-a-real-binary-xyz") {
		t.Fatalf("missing bin must be reported, got %v", err)
	}
}

func TestParseTokens(t *testing.T) {
	got := ParseTokens(`"tokens":\{"total":([0-9]+)`, `blah "tokens":{"total":1234} blah`)
	if got != 1234 {
		t.Errorf("ParseTokens = %d, want 1234", got)
	}
	if ParseTokens("", "anything") != -1 {
		t.Errorf("empty regex must yield -1")
	}
	if ParseTokens(`total=([0-9]+)`, "no match here") != -1 {
		t.Errorf("no match must yield -1")
	}
}
