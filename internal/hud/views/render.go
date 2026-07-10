package views

import (
	"fmt"
	"time"

	"github.com/corruptmemory/ringer/internal/state"
)

// RunState derives the ringside run bucket from the Go run-state: "live"
// while running, else "fail" if any task failed/timed out, else "pass".
func RunState(rs state.RunState) string {
	if !rs.Done {
		return "live"
	}
	if FailCount(rs) > 0 {
		return "fail"
	}
	return "pass"
}

// PassCount / FailCount count terminal task outcomes.
func PassCount(rs state.RunState) int {
	n := 0
	for _, t := range rs.Tasks {
		if t.Status == "passed" {
			n++
		}
	}
	return n
}

func FailCount(rs state.RunState) int {
	n := 0
	for _, t := range rs.Tasks {
		if t.Status == "failed" || t.Status == "timeout" {
			n++
		}
	}
	return n
}

// RunElapsed is updated-started in seconds (0 if either is unparseable).
func RunElapsed(rs state.RunState) float64 { return elapsed(rs.StartedAt, rs.UpdatedAt) }

// TaskElapsed is a task's ended-started in seconds; a still-running task
// (no ended_at) reads as 0.
func TaskElapsed(t state.TaskView) float64 { return elapsed(t.StartedAt, t.EndedAt) }

// TaskKind maps a Go task status to the ringside bucket the lifted CSS
// styles: passed→pass, running→working (retry on a 2nd attempt),
// failed/timeout→fail, else waiting.
func TaskKind(t state.TaskView) string {
	switch t.Status {
	case "passed":
		return "pass"
	case "running":
		if t.Attempt > 1 {
			return "retry"
		}
		return "working"
	case "failed", "timeout":
		return "fail"
	default:
		return "waiting"
	}
}

// FormatDuration renders seconds as "9s" or "1m 03s" (mirrors ringside
// formatDuration's minute:zero-padded-second shape).
func FormatDuration(sec float64) string {
	s := int(sec + 0.5)
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm %02ds", s/60, s%60)
}

func elapsed(startISO, endISO string) float64 {
	start, err1 := time.Parse(time.RFC3339, startISO)
	end, err2 := time.Parse(time.RFC3339, endISO)
	if err1 != nil || err2 != nil {
		return 0
	}
	if d := end.Sub(start).Seconds(); d > 0 {
		return d
	}
	return 0
}
