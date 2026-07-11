// internal/catalog/diff.go
package catalog

import "sort"

type Event struct {
	TS      string
	Kind    string
	ModelID string
	Payload map[string]any
}

func perM(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func modelDetails(m Model) map[string]any {
	return map[string]any{
		"name": m.Name, "prompt_per_m": perM(m.PromptPerM), "completion_per_m": perM(m.CompletionPerM),
		"variable_pricing": m.VariablePricing, "pricing_unknown": m.PricingUnknown,
		"free": m.Free, "context_length": m.ContextLength, "modality": m.Modality,
	}
}

// Diff ports ringer.py:1374-1478. Events carry old/new per-M prices in Payload.
func Diff(old, nw []Model, ts string) []Event {
	oldByID := map[string]Model{}
	newByID := map[string]Model{}
	for _, m := range old {
		if m.ID != "" {
			oldByID[m.ID] = m
		}
	}
	for _, m := range nw {
		if m.ID != "" {
			newByID[m.ID] = m
		}
	}
	var events []Event
	added, removed, common := keyDiff(oldByID, newByID)
	for _, id := range added {
		m := newByID[id]
		p := modelDetails(m)
		events = append(events, Event{TS: ts, Kind: "added", ModelID: id, Payload: p})
	}
	for _, id := range removed {
		m := oldByID[id]
		p := modelDetails(m)
		events = append(events, Event{TS: ts, Kind: "removed", ModelID: id, Payload: p})
	}
	for _, id := range common {
		o, n := oldByID[id], newByID[id]
		oldPrompt, newPrompt := perM(o.PromptPerM), perM(n.PromptPerM)
		oldComp, newComp := perM(o.CompletionPerM), perM(n.CompletionPerM)
		payload := func() map[string]any {
			return map[string]any{
				"name":             pick(n.Name, o.Name),
				"old_prompt_per_m": oldPrompt, "new_prompt_per_m": newPrompt,
				"old_completion_per_m": oldComp, "new_completion_per_m": newComp,
				"old_free": o.Free, "new_free": n.Free,
			}
		}
		if n.VariablePricing {
			if !o.VariablePricing {
				events = append(events, Event{TS: ts, Kind: "pricing_variable", ModelID: id, Payload: payload()})
			}
			continue
		}
		if o.VariablePricing {
			events = append(events, Event{TS: ts, Kind: "pricing_fixed", ModelID: id, Payload: payload()})
			if n.Free {
				events = append(events, Event{TS: ts, Kind: "went_free", ModelID: id, Payload: payload()})
			}
			continue
		}
		if oldPrompt != newPrompt || oldComp != newComp {
			events = append(events, Event{TS: ts, Kind: "price_change", ModelID: id, Payload: payload()})
		}
		if o.Free != n.Free {
			kind := "went_paid"
			if n.Free {
				kind = "went_free"
			}
			events = append(events, Event{TS: ts, Kind: kind, ModelID: id, Payload: payload()})
		}
	}
	return events
}

func keyDiff(oldByID, newByID map[string]Model) (added, removed, common []string) {
	for id := range newByID {
		if _, ok := oldByID[id]; !ok {
			added = append(added, id)
		} else {
			common = append(common, id)
		}
	}
	for id := range oldByID {
		if _, ok := newByID[id]; !ok {
			removed = append(removed, id)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(common)
	return
}

func pick(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
