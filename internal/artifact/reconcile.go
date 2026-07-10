package artifact

import "github.com/corruptmemory/ringer/internal/state"

// ReconcileDeadRuns flips library entries still marked state:"live" to
// "died" when their current_run_id is no longer in the pid-pruned
// active-runs registry (a run whose orchestrator exited without a clean
// finish). nowISO stamps flipped entries. Rewrites library.json only when
// something changed. Mirrors upstream reconcile_artifact_library_dead_runs.
func ReconcileDeadRuns(stateDir, nowISO string) (bool, error) {
	active, err := state.ReadActiveRuns(stateDir)
	if err != nil {
		return false, err
	}
	lib := ReadLibrary(stateDir)
	changed := false
	for name, entry := range lib.Artifacts {
		if entry.State != "live" {
			continue
		}
		if _, ok := active[entry.CurrentRunID]; entry.CurrentRunID != "" && ok {
			continue
		}
		entry.State = "died"
		entry.UpdatedAt = nowISO
		lib.Artifacts[name] = entry
		changed = true
	}
	if changed {
		if err := WriteLibrary(stateDir, lib); err != nil {
			return false, err
		}
	}
	return changed, nil
}
