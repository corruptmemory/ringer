package views

import (
	"fmt"

	"github.com/corruptmemory/ringer/internal/state"
)

// OutcomeText is the run's one-line result (mirrors ringside's outcome
// string, dashboard/ringside.html:885-889).
func OutcomeText(rs state.RunState) string {
	pass := PassCount(rs)
	if RunState(rs) == "died" {
		return fmt.Sprintf("died · %d passed", pass)
	}
	if RunState(rs) == "live" {
		return fmt.Sprintf("%d passed so far", pass)
	}
	if fail := FailCount(rs); fail > 0 {
		return fmt.Sprintf("%d passed · %d failed", pass, fail)
	}
	return fmt.Sprintf("all %d passed", pass)
}

// TaskStateText is the human label for a task bucket (ringside
// taskStateText, dashboard/ringside.html:708-712).
func TaskStateText(kind string) string {
	switch kind {
	case "pass":
		return "finished & checked"
	case "working":
		return "working"
	case "retry":
		return "sent back — redoing"
	case "fail":
		return "failed"
	default:
		return "waiting"
	}
}
