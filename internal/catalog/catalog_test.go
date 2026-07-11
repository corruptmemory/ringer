package catalog

import "testing"

func f(v float64) *float64 { return &v }

func TestNormalize(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]any
		want Model
	}{
		{"paid", map[string]any{"id": "a/b", "name": "A B", "context_length": 128000,
			"pricing":      map[string]any{"prompt": "0.0000005", "completion": "0.0000015"},
			"architecture": map[string]any{"modality": "text"}},
			Model{ID: "a/b", Name: "A B", ContextLength: 128000, Modality: "text",
				PromptPerM: f(0.5), CompletionPerM: f(1.5), Free: false}},
		{"free-slug", map[string]any{"id": "x/y:free",
			"pricing": map[string]any{"prompt": "0", "completion": "0"}},
			Model{ID: "x/y:free", Name: "x/y:free", PromptPerM: f(0), CompletionPerM: f(0), Free: true}},
		{"unknown-pricing", map[string]any{"id": "u/v", "pricing": map[string]any{}},
			Model{ID: "u/v", Name: "u/v", PricingUnknown: true, VariablePricing: true}},
		{"negative-variable", map[string]any{"id": "n/g",
			"pricing": map[string]any{"prompt": "-1", "completion": "0.0000001"}},
			Model{ID: "n/g", Name: "n/g", VariablePricing: true}},
	}
	for _, c := range cases {
		got := Normalize(c.raw, "")
		if got.ID != c.want.ID || got.Name != c.want.Name || got.Free != c.want.Free ||
			got.VariablePricing != c.want.VariablePricing || got.PricingUnknown != c.want.PricingUnknown ||
			!eqp(got.PromptPerM, c.want.PromptPerM) || !eqp(got.CompletionPerM, c.want.CompletionPerM) {
			t.Errorf("%s: Normalize=%+v want %+v", c.name, got, c.want)
		}
	}
}

func eqp(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
