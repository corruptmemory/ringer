// internal/catalog/catalog.go
package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultSource     = "https://openrouter.ai/api/v1/models"
	FetchTimeout      = 30 * time.Second
	AutoRefreshMaxAge = 24 * time.Hour
)

type Model struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	ContextLength   int      `json:"context_length"`
	Modality        string   `json:"modality"`
	PromptPerM      *float64 `json:"prompt_per_m"`
	CompletionPerM  *float64 `json:"completion_per_m"`
	VariablePricing bool     `json:"variable_pricing"`
	PricingUnknown  bool     `json:"pricing_unknown"`
	Free            bool     `json:"free"`
	FetchedAt       string   `json:"fetched_at"`
}

// priceOrNil parses a raw price value ("" / nil / bad -> nil = unknown). A
// valid parse (including "0" and negatives) returns the float.
func priceOrNil(v any) *float64 {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", v))
	if s == "" {
		return nil
	}
	x, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &x
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func toMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// Normalize ports ringer.py:1268-1306.
func Normalize(raw map[string]any, fetchedAt string) Model {
	pricing := toMap(raw["pricing"])
	arch := toMap(raw["architecture"])
	id := str(raw["id"])
	prompt := priceOrNil(pricing["prompt"])
	completion := priceOrNil(pricing["completion"])
	pricingUnknown := prompt == nil || completion == nil
	variable := pricingUnknown || (prompt != nil && *prompt < 0) || (completion != nil && *completion < 0)
	var promptPerM, completionPerM *float64
	if !variable {
		p := *prompt * 1e6
		c := *completion * 1e6
		promptPerM, completionPerM = &p, &c
	}
	free := false
	if !variable {
		free = strings.HasSuffix(id, ":free") || (*prompt == 0 && *completion == 0)
	}
	if strings.HasSuffix(id, ":free") {
		free = true
	}
	ctx := 0
	if n, ok := asInt(raw["context_length"]); ok {
		ctx = n
	}
	name := str(raw["name"])
	if name == "" {
		name = id
	}
	return Model{
		ID: id, Name: name, ContextLength: ctx, Modality: str(arch["modality"]),
		PromptPerM: promptPerM, CompletionPerM: completionPerM,
		VariablePricing: variable, PricingUnknown: pricingUnknown, Free: free, FetchedAt: fetchedAt,
	}
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i), true
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i, true
		}
	}
	return 0, false
}

// NormalizePayload ports ringer.py:1309-1320: payload.data (list) -> sorted models.
func NormalizePayload(payload map[string]any, fetchedAt string) ([]Model, error) {
	data, ok := payload["data"].([]any)
	if !ok {
		return nil, fmt.Errorf("catalog source must be a JSON object with a data array")
	}
	models := make([]Model, 0, len(data))
	for _, item := range data {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		nm := Normalize(m, fetchedAt)
		if nm.ID != "" {
			models = append(models, nm)
		}
	}
	SortModels(models)
	return models, nil
}

// sumPrice mirrors catalog_sort_key: variable models sort last (Inf).
func sumPrice(m Model) float64 {
	if m.VariablePricing {
		return math.Inf(1)
	}
	var s float64
	if m.PromptPerM != nil {
		s += *m.PromptPerM
	}
	if m.CompletionPerM != nil {
		s += *m.CompletionPerM
	}
	return s
}

func SortModels(models []Model) {
	sort.SliceStable(models, func(i, j int) bool {
		vi, vj := models[i].VariablePricing, models[j].VariablePricing
		if vi != vj {
			return !vi // non-variable first
		}
		si, sj := sumPrice(models[i]), sumPrice(models[j])
		if si != sj {
			return si < sj
		}
		return models[i].ID < models[j].ID
	})
}

// Fetch ports ringer.py:1334-1344.
func Fetch(source string, timeout time.Duration) (map[string]any, error) {
	u, err := url.Parse(source)
	var body []byte
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		req, _ := http.NewRequest(http.MethodGet, source, nil)
		req.Header.Set("User-Agent", "ringer")
		client := &http.Client{Timeout: timeout}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("catalog fetch %s: %w", source, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("catalog fetch %s: HTTP %d", source, resp.StatusCode)
		}
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
	} else {
		body, err = os.ReadFile(expandUser(source))
		if err != nil {
			return nil, err
		}
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("catalog source must be a JSON object: %w", err)
	}
	return payload, nil
}

// LoadModelsFile tolerantly reads --file: raw payload, snapshot, or bare list.
func LoadModelsFile(path string) ([]Model, error) {
	body, err := os.ReadFile(expandUser(path))
	if err != nil {
		return nil, err
	}
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	switch v := doc.(type) {
	case map[string]any:
		if _, ok := v["data"].([]any); ok {
			return NormalizePayload(v, str(v["fetched_at"]))
		}
		if models, ok := v["models"].([]any); ok {
			return modelsFromList(models), nil
		}
	case []any:
		return modelsFromList(v), nil
	}
	return nil, fmt.Errorf("unrecognized catalog file shape: %s", path)
}

func modelsFromList(list []any) []Model {
	out := make([]Model, 0, len(list))
	for _, item := range list {
		b, err := json.Marshal(item)
		if err != nil {
			continue
		}
		var m Model
		if err := json.Unmarshal(b, &m); err == nil && m.ID != "" {
			out = append(out, m)
		}
	}
	SortModels(out)
	return out
}

func expandUser(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}
