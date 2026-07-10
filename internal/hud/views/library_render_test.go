package views

import (
	"testing"

	"github.com/corruptmemory/ringer/internal/artifact"
)

// TestSortedLibraryEntriesStableNewestFirst locks the stable ordering that
// stops the polled library panel from flipping rows: newest UpdatedAt first,
// run_name as tiebreaker, identical across repeated calls (a raw map range is
// randomized per access).
func TestSortedLibraryEntriesStableNewestFirst(t *testing.T) {
	lib := artifact.Library{Artifacts: map[string]artifact.Entry{
		"beta":  {UpdatedAt: "2026-07-10T10:00:00Z"},
		"alpha": {UpdatedAt: "2026-07-10T11:00:00Z"},
		"gamma": {UpdatedAt: "2026-07-10T11:00:00Z"}, // ties alpha on time -> name tiebreak
	}}
	want := []string{"alpha", "gamma", "beta"} // 11:00 (alpha<gamma), then 10:00
	for i := 0; i < 20; i++ {
		got := SortedLibraryEntries(lib)
		if len(got) != 3 {
			t.Fatalf("got %d entries", len(got))
		}
		for k := range want {
			if got[k].Name != want[k] {
				t.Fatalf("iteration %d: got %s at %d, want %s (order unstable/wrong)", i, got[k].Name, k, want[k])
			}
		}
	}
}
