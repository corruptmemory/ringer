// internal/scoreboard/identity.go
package scoreboard

import (
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	ringer "github.com/corruptmemory/ringer"
)

type ModelIdentity struct {
	ModelDisplay string
	Harness      string
	Access       string
	Confidence   string
	Source       string
}

type Registry struct {
	identities map[[2]string]ModelIdentity // key: {engine, modelKey}
	defaults   map[string]string           // engine -> default model key
	engineMeta map[string]ModelIdentity    // engine -> meta identity
}

func txt(s string) string { return strings.TrimSpace(s) }

// Resolve is a verbatim port of ringer.py:4749-4780.
func (r Registry) Resolve(engine, modelKey string) ModelIdentity {
	engineKey := txt(engine)
	rawKey := txt(modelKey)
	lookup := rawKey
	if lookup == "" {
		lookup = r.defaults[engineKey]
	}
	if id, ok := r.identities[[2]string{engineKey, lookup}]; ok {
		return id
	}
	meta, hasMeta := r.engineMeta[engineKey]
	if engineKey == "opencode" && strings.HasPrefix(rawKey, "openrouter/") {
		harness, access := "OpenCode", "OpenRouter API"
		if hasMeta {
			harness, access = meta.Harness, meta.Access
		}
		return ModelIdentity{
			ModelDisplay: strings.TrimPrefix(rawKey, "openrouter/"),
			Harness:      harness, Access: access,
			Confidence: "fallback", Source: "unlisted OpenRouter slug",
		}
	}
	if hasMeta && lookup != "" {
		return ModelIdentity{
			ModelDisplay: lookup, Harness: meta.Harness, Access: meta.Access,
			Confidence: "fallback", Source: "engine default model key",
		}
	}
	unknown := engineKey
	if unknown == "" {
		unknown = "unknown"
	}
	return ModelIdentity{ModelDisplay: unknown, Harness: unknown, Access: "unknown", Confidence: "unknown"}
}

type registryTOML struct {
	Engines map[string]struct {
		Harness         string `toml:"harness"`
		Access          string `toml:"access"`
		DefaultModelKey string `toml:"default_model_key"`
		Models          map[string]struct {
			Display    string `toml:"display"`
			Confidence string `toml:"confidence"`
			Source     string `toml:"source"`
		} `toml:"models"`
	} `toml:"engines"`
}

// ParseRegistry ports ringer.py:4786-4833. Malformed input -> empty registry, nil error only for a decode failure that Python swallowed; we surface decode errors for tests but LoadRegistry ignores them.
func ParseRegistry(data []byte) (Registry, error) {
	reg := Registry{
		identities: map[[2]string]ModelIdentity{},
		defaults:   map[string]string{},
		engineMeta: map[string]ModelIdentity{},
	}
	var doc registryTOML
	if err := toml.Unmarshal(data, &doc); err != nil {
		return reg, err
	}
	for engineName, raw := range doc.Engines {
		engine := txt(engineName)
		if engine == "" {
			continue
		}
		harness := txt(raw.Harness)
		if harness == "" {
			harness = engine
		}
		access := txt(raw.Access)
		if access == "" {
			access = "unknown"
		}
		if dk := txt(raw.DefaultModelKey); dk != "" {
			reg.defaults[engine] = dk
		}
		reg.engineMeta[engine] = ModelIdentity{ModelDisplay: engine, Harness: harness, Access: access, Confidence: "engine"}
		for keyRaw, m := range raw.Models {
			key := txt(keyRaw)
			if key == "" {
				continue
			}
			display := txt(m.Display)
			if display == "" {
				display = key
			}
			reg.identities[[2]string{engine, key}] = ModelIdentity{
				ModelDisplay: display, Harness: harness, Access: access,
				Confidence: txt(m.Confidence), Source: txt(m.Source),
			}
		}
	}
	return reg, nil
}

// LoadRegistry loads the override file at overridePath, or the embedded
// registry when overridePath == "". A read/parse failure yields an empty
// registry (Python parity: analytics degrades to raw model keys, never
// crashes). A non-empty override that can't be read returns empty rather than
// silently masquerading the embedded default as the override — a misconfigured
// override degrades visibly (raw slugs), not invisibly (built-in identities).
func LoadRegistry(overridePath string) Registry {
	data := ringer.ModelIdentityTOML
	if overridePath != "" {
		b, err := os.ReadFile(overridePath)
		if err != nil {
			empty, _ := ParseRegistry(nil) // empty-but-non-nil registry; override read failed, do not fall back to embedded
			return empty
		}
		data = b
	}
	reg, _ := ParseRegistry(data) // ParseRegistry already returns an empty-non-nil Registry on its error path
	return reg
}
