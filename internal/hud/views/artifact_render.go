package views

import (
	"fmt"
	"strings"

	"github.com/corruptmemory/ringer/internal/state"
)

// BriefingLive is the status page's plain-language heading (port of
// live_briefing_html, ringer.py:3013-3034): e.g. "Ringer is working on 2
// tasks — 1 finished and checked and 1 working, started 20 seconds ago."
func BriefingLive(rs state.RunState) string {
	total := len(rs.Tasks)
	ago := formatAgo(RunElapsed(rs))
	if total == 0 {
		return fmt.Sprintf("Ringer has no tasks. Started %s ago.", ago)
	}
	pass, fail := PassCount(rs), FailCount(rs)
	working, retry, waiting := bucketCounts(rs)
	var parts []string
	if pass > 0 {
		parts = append(parts, passedPhrase(pass))
	}
	if working > 0 {
		parts = append(parts, runningPhrase(working))
	}
	if retry > 0 {
		parts = append(parts, retryPhrase(retry))
	}
	if waiting > 0 {
		parts = append(parts, waitingPhrase(waiting))
	}
	if fail > 0 {
		parts = append(parts, failedPhrase(fail))
	}
	statusSentence := joinPlainParts(parts)
	return fmt.Sprintf("Ringer is working on %d %s — %s, started %s ago.", total, taskWord(total), statusSentence, ago)
}

// BriefingFinal is the final report heading (port of final_briefing_html,
// ringer.py:3041-3053): e.g. "Ringer finished 3 tasks in 1m 04s. All 3
// finished and checked." / "...2 finished and checked, 1 failed after
// retry."
func BriefingFinal(rs state.RunState) string {
	total := len(rs.Tasks)
	pass, fail := PassCount(rs), FailCount(rs)
	elapsed := FormatDuration(RunElapsed(rs))
	first := fmt.Sprintf("Ringer finished %d %s in %s.", total, taskWord(total), elapsed)
	if fail == 0 {
		return fmt.Sprintf("%s All %d finished and checked.", first, total)
	}
	return fmt.Sprintf("%s %d finished and checked, %d failed after retry.", first, pass, fail)
}

// progressLegend is the progress bar's summary line (port of
// render_progress_bar's legend, ringer.py:3169-3180): e.g. "1 finished · 1
// working" or "No tasks".
func progressLegend(rs state.RunState) string {
	pass, fail := PassCount(rs), FailCount(rs)
	working, retry, waiting := bucketCounts(rs)
	var parts []string
	if pass > 0 {
		parts = append(parts, fmt.Sprintf("%d finished", pass))
	}
	if working > 0 {
		parts = append(parts, fmt.Sprintf("%d working", working))
	}
	if retry > 0 {
		parts = append(parts, fmt.Sprintf("%d sent back", retry))
	}
	if fail > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", fail))
	}
	if waiting > 0 {
		parts = append(parts, fmt.Sprintf("%d waiting", waiting))
	}
	if len(parts) == 0 {
		return "No tasks"
	}
	return strings.Join(parts, " · ")
}

// bucketCounts tallies the "working"/"retry"/"waiting" TaskKind buckets not
// already covered by the existing PassCount/FailCount helpers (port of the
// non-pass/fail slices of task_status_counts, ringer.py:2957-2972).
func bucketCounts(rs state.RunState) (working, retry, waiting int) {
	for _, t := range rs.Tasks {
		switch TaskKind(t) {
		case "working":
			working++
		case "retry":
			retry++
		case "waiting":
			waiting++
		}
	}
	return working, retry, waiting
}

func taskWord(n int) string {
	if n == 1 {
		return "task"
	}
	return "tasks"
}

// passedPhrase/failedPhrase/runningPhrase/retryPhrase/waitingPhrase port the
// eponymous Python phrase helpers (ringer.py:2979-3006).
func passedPhrase(n int) string  { return fmt.Sprintf("%d finished and checked", n) }
func failedPhrase(n int) string  { return fmt.Sprintf("%d failed", n) }
func runningPhrase(n int) string { return fmt.Sprintf("%d working", n) }
func retryPhrase(n int) string   { return fmt.Sprintf("%d sent back", n) }
func waitingPhrase(n int) string {
	if n == 1 {
		return "1 is waiting"
	}
	return fmt.Sprintf("%d are waiting", n)
}

// joinPlainParts ports join_plain_html_parts (ringer.py:3056-3063): "a", "a
// and b", or "a, b, and c".
func joinPlainParts(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
	}
}

// formatAgo renders seconds as a word-based "ago" duration (port of
// fmt_plain_ago, ringer.py:2174-2195): "9 seconds", "1 minute 5 seconds", "2
// hours". Distinct from FormatDuration's compact "9s"/"1m 03s" shape, which
// live_briefing_html does not use for its "started ... ago" clause.
func formatAgo(sec float64) string {
	total := int(sec)
	if total < 0 {
		total = 0
	}
	if total < 60 {
		return pluralUnit(total, "second")
	}
	minutes, secondsLeft := total/60, total%60
	if minutes < 60 {
		if secondsLeft == 0 {
			return pluralUnit(minutes, "minute")
		}
		return pluralUnit(minutes, "minute") + " " + pluralUnit(secondsLeft, "second")
	}
	hours, minutesLeft := minutes/60, minutes%60
	if minutesLeft == 0 {
		return pluralUnit(hours, "hour")
	}
	return pluralUnit(hours, "hour") + " " + pluralUnit(minutesLeft, "minute")
}

func pluralUnit(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
