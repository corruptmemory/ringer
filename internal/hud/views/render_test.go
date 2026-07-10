package views

import (
	"testing"

	"github.com/corruptmemory/ringer/internal/state"
)

func TestRunDerivations(t *testing.T) {
	live := state.RunState{Done: false, StartedAt: "2026-07-09T00:00:00Z", UpdatedAt: "2026-07-09T00:00:05Z",
		Tasks: []state.TaskView{{Status: "running"}}}
	if RunState(live) != "live" {
		t.Fatalf("running run → %q, want live", RunState(live))
	}
	if RunElapsed(live) != 5 {
		t.Fatalf("elapsed = %v, want 5", RunElapsed(live))
	}
	finFail := state.RunState{Done: true, Tasks: []state.TaskView{{Status: "passed"}, {Status: "failed"}}}
	if RunState(finFail) != "fail" || PassCount(finFail) != 1 || FailCount(finFail) != 1 {
		t.Fatalf("finished-with-fail wrong: state=%q pass=%d fail=%d", RunState(finFail), PassCount(finFail), FailCount(finFail))
	}
	finPass := state.RunState{Done: true, Tasks: []state.TaskView{{Status: "passed"}}}
	if RunState(finPass) != "pass" {
		t.Fatalf("all-pass finished → %q, want pass", RunState(finPass))
	}
}

func TestTaskKind(t *testing.T) {
	cases := []struct {
		status  string
		attempt int
		want    string
	}{
		{"passed", 1, "pass"}, {"running", 1, "working"}, {"running", 2, "retry"},
		{"failed", 1, "fail"}, {"timeout", 1, "fail"}, {"pending", 0, "waiting"},
	}
	for _, c := range cases {
		if got := TaskKind(state.TaskView{Status: c.status, Attempt: c.attempt}); got != c.want {
			t.Errorf("TaskKind(%q, a%d) = %q, want %q", c.status, c.attempt, got, c.want)
		}
	}
}

func TestTaskElapsedAndFormat(t *testing.T) {
	tv := state.TaskView{StartedAt: "2026-07-09T00:00:00Z", EndedAt: "2026-07-09T00:01:03Z"}
	if TaskElapsed(tv, "2026-07-09T00:01:03Z") != 63 {
		t.Fatalf("task elapsed = %v, want 63", TaskElapsed(tv, "2026-07-09T00:01:03Z"))
	}
	// A still-running task (no end) measures against nowISO, not 0.
	if TaskElapsed(state.TaskView{StartedAt: "2026-07-09T00:00:00Z"}, "2026-07-09T00:00:00Z") != 0 {
		t.Fatal("unfinished task elapsed at nowISO==StartedAt must be 0")
	}
	// A never-started-but-failed task (EndedAt set, StartedAt "") reads 0 too,
	// not a huge/negative number (runner early-exit failures stamp EndedAt only).
	if TaskElapsed(state.TaskView{EndedAt: "2026-07-09T00:00:00Z"}, "2026-07-09T00:00:05Z") != 0 {
		t.Fatal("never-started (empty StartedAt) task elapsed must be 0")
	}
	if got := FormatDuration(63); got != "1m 03s" {
		t.Fatalf("FormatDuration(63) = %q, want \"1m 03s\"", got)
	}
	if got := FormatDuration(9); got != "9s" {
		t.Fatalf("FormatDuration(9) = %q, want \"9s\"", got)
	}
}

func TestTaskElapsedRunningTaskTicks(t *testing.T) {
	// A running task (no ended_at) must measure started_at -> nowISO, not 0.
	running := state.TaskView{Status: "running", StartedAt: "2026-07-10T10:00:00Z", EndedAt: ""}
	if got := TaskElapsed(running, "2026-07-10T10:00:13Z"); got != 13 {
		t.Errorf("running TaskElapsed = %v, want 13", got)
	}
	// A finished task ignores nowISO and uses ended_at.
	done := state.TaskView{Status: "passed", StartedAt: "2026-07-10T10:00:00Z", EndedAt: "2026-07-10T10:00:05Z"}
	if got := TaskElapsed(done, "2026-07-10T10:00:13Z"); got != 5 {
		t.Errorf("finished TaskElapsed = %v, want 5", got)
	}
	// Unparseable/empty start still yields 0.
	if got := TaskElapsed(state.TaskView{}, "2026-07-10T10:00:13Z"); got != 0 {
		t.Errorf("empty TaskElapsed = %v, want 0", got)
	}
}
