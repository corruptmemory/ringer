package catalog

import "testing"

func TestDiff(t *testing.T) {
	old := []Model{
		{ID: "keep", PromptPerM: f(1), CompletionPerM: f(2)},
		{ID: "goes", PromptPerM: f(1), CompletionPerM: f(1)},
		{ID: "reprice", PromptPerM: f(1), CompletionPerM: f(1)},
		{ID: "tofree", PromptPerM: f(1), CompletionPerM: f(1)},
	}
	nw := []Model{
		{ID: "keep", PromptPerM: f(1), CompletionPerM: f(2)},               // unchanged -> no event
		{ID: "reprice", PromptPerM: f(3), CompletionPerM: f(1)},            // price_change
		{ID: "tofree", PromptPerM: f(0), CompletionPerM: f(0), Free: true}, // price_change + went_free
		{ID: "brand", PromptPerM: f(5), CompletionPerM: f(5)},              // added
	}
	events := Diff(old, nw, "T")
	kinds := map[string]int{}
	for _, e := range events {
		kinds[e.Kind+":"+e.ModelID]++
	}
	for _, want := range []string{"added:brand", "removed:goes", "price_change:reprice", "price_change:tofree", "went_free:tofree"} {
		if kinds[want] == 0 {
			t.Errorf("missing event %s; got %v", want, kinds)
		}
	}
	if kinds["price_change:keep"] != 0 {
		t.Errorf("unchanged model emitted an event")
	}
}
