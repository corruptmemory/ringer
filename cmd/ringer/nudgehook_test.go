// cmd/ringer/nudgehook_test.go
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/nudge"
)

func TestRunNudgeHookPreBashEmits(t *testing.T) {
	t.Setenv("RINGER_HOME", t.TempDir())
	in := strings.NewReader(`{"session_id":"cli1","tool_input":{"command":"curl https://api.anthropic.com/v1/messages"}}`)
	var out bytes.Buffer
	if err := runNudgeHook("pre-bash", in, &out); err != nil {
		t.Fatalf("nudge-hook must never error, got %v", err)
	}
	if !strings.Contains(out.String(), nudge.NudgeText) {
		t.Fatalf("expected nudge output, got %q", out.String())
	}
}

func TestRunNudgeHookBogusModeIsSilentExitZero(t *testing.T) {
	t.Setenv("RINGER_HOME", t.TempDir())
	var out bytes.Buffer
	if err := runNudgeHook("", strings.NewReader(`{}`), &out); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no output, got %q", out.String())
	}
}
