package views

import (
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/state"
)

func TestBriefingLiveAndFinal(t *testing.T) {
	live := state.RunState{RunName: "run", StartedAt: "2026-07-10T10:00:00Z", UpdatedAt: "2026-07-10T10:00:20Z",
		Tasks: []state.TaskView{{Status: "running"}, {Status: "passed"}}}
	if b := BriefingLive(live); !strings.Contains(b, "2") { // "working on N tasks"
		t.Errorf("live briefing missing task count: %q", b)
	}
	final := state.RunState{RunName: "run", Done: true, StartedAt: "2026-07-10T10:00:00Z", UpdatedAt: "2026-07-10T10:01:04Z",
		Tasks: []state.TaskView{{Status: "passed"}, {Status: "passed"}, {Status: "passed"}}}
	if b := BriefingFinal(final); !strings.Contains(b, "3") {
		t.Errorf("final briefing missing task count: %q", b)
	}
}
