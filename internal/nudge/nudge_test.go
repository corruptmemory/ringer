package nudge

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func payloadJSON(t *testing.T, m map[string]any) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestProviderHarnessRegexes(t *testing.T) {
	hits := []string{
		"curl https://api.anthropic.com/v1/messages",
		"python3 run_probe.py --n 5",
		"node persona_sim.mjs",
		"bun smoke_grader.ts",
	}
	for _, c := range hits {
		if !(providerRE.MatchString(c) || harnessRE.MatchString(c)) {
			t.Errorf("expected match for %q", c)
		}
	}
	misses := []string{"ls -la", "git status", "go test ./...", "python3 app.py"}
	for _, c := range misses {
		if providerRE.MatchString(c) || harnessRE.MatchString(c) {
			t.Errorf("unexpected match for %q", c)
		}
	}
}

func TestCommandInvokesRinger(t *testing.T) {
	yes := []string{"ringer run m.toml", "/usr/bin/ringer demo", "./ringer nudge-hook pre-bash", "python3 ringer.py run"}
	for _, c := range yes {
		if !commandInvokesRinger(c) {
			t.Errorf("expected ringer-invocation for %q", c)
		}
	}
	no := []string{"python3 ringer_probe.py", "grep ringer notes.txt", "node harness.mjs"}
	for _, c := range no {
		if commandInvokesRinger(c) {
			t.Errorf("did not expect ringer-invocation for %q", c)
		}
	}
}

func TestSafeSessionID(t *testing.T) {
	cases := map[any]string{
		"abc-123":   "abc-123",
		"a/b c":     "a_b_c",
		"":          "unknown-session",
		nil:         "unknown-session",
		"__weird__": "weird",
	}
	for in, want := range cases {
		if got := safeSessionID(in); got != want {
			t.Errorf("safeSessionID(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestRunPreBashTriggersAndDedupes(t *testing.T) {
	home := t.TempDir()
	pl := payloadJSON(t, map[string]any{
		"session_id": "s1",
		"tool_input": map[string]any{"command": "curl https://api.anthropic.com/v1/messages"},
	})

	var out1 bytes.Buffer
	if err := Run("pre-bash", strings.NewReader(pl), &out1, home); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out1.String(), NudgeText) {
		t.Fatalf("expected nudge, got %q", out1.String())
	}
	if !strings.Contains(out1.String(), `"hookEventName":"PreToolUse"`) {
		t.Fatalf("expected PreToolUse event, got %q", out1.String())
	}

	// Second identical invocation is deduped by the marker.
	var out2 bytes.Buffer
	if err := Run("pre-bash", strings.NewReader(pl), &out2, home); err != nil {
		t.Fatal(err)
	}
	if out2.Len() != 0 {
		t.Fatalf("expected deduped (empty) output, got %q", out2.String())
	}
}

func TestRunPreBashSuppressedByLiveRun(t *testing.T) {
	home := t.TempDir()
	// A live run: this test's own PID is alive, so the entry survives pruning.
	writeActiveRunsForTest(t, home, os.Getpid())
	pl := payloadJSON(t, map[string]any{
		"session_id": "s2",
		"tool_input": map[string]any{"command": "curl https://api.openai.com/v1/chat/completions"},
	})
	var out bytes.Buffer
	if err := Run("pre-bash", strings.NewReader(pl), &out, home); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected suppression during a live run, got %q", out.String())
	}
}

func TestRunPostEditThreshold(t *testing.T) {
	home := t.TempDir()
	mk := func(i int) string {
		return payloadJSON(t, map[string]any{
			"session_id": "sedit",
			"tool_input": map[string]any{"file_path": filepathN(i)},
		})
	}
	// 7 edits across 3 files: below the count>=8 threshold → no nudge yet.
	for i := 0; i < 7; i++ {
		var out bytes.Buffer
		if err := Run("post-edit", strings.NewReader(mk(i%3)), &out, home); err != nil {
			t.Fatal(err)
		}
		if out.Len() != 0 {
			t.Fatalf("edit %d: unexpected early nudge %q", i, out.String())
		}
	}
	// 8th edit (still 3 distinct files) crosses count>=8 AND distinct>=3.
	var out bytes.Buffer
	if err := Run("post-edit", strings.NewReader(mk(0)), &out, home); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"hookEventName":"PostToolUse"`) {
		t.Fatalf("expected PostToolUse nudge at the 8th edit, got %q", out.String())
	}
}

func TestRunUnknownModeAndMalformed(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	if err := Run("bogus", strings.NewReader(`{}`), &out, home); err != nil || out.Len() != 0 {
		t.Fatalf("unknown mode should no-op: err=%v out=%q", err, out.String())
	}
	out.Reset()
	if err := Run("pre-bash", strings.NewReader("not json"), &out, home); err != nil || out.Len() != 0 {
		t.Fatalf("malformed stdin should no-op: err=%v out=%q", err, out.String())
	}
	out.Reset()
	if err := Run("pre-bash", strings.NewReader(""), &out, home); err != nil || out.Len() != 0 {
		t.Fatalf("empty stdin should no-op: err=%v out=%q", err, out.String())
	}
}

// writeActiveRunsForTest writes an active-runs.json with one entry at pid.
func writeActiveRunsForTest(t *testing.T, home string, pid int) {
	t.Helper()
	entry := map[string]any{
		"r1": map[string]any{"pid": pid, "run_name": "x", "identity": "y", "workdir": "/tmp/x", "started_at": "t"},
	}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "active-runs.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func filepathN(i int) string {
	return filepath.Join("/work", "file"+string(rune('a'+i))+".go")
}
