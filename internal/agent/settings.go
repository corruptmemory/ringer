package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// hookMarker identifies a ringer-installed hook by a substring of its command.
// The Go install registers `<binary> nudge-hook <action>`, so "nudge-hook" is
// the stable, binary-path-independent marker (the analog of ringer_nudge.py's
// "ringer_nudge.py"). Legacy `python3 …/ringer_nudge.py` hooks are intentionally
// NOT matched — cleaning those up belongs to the 5d cutover / a README
// migration note, not this port.
const hookMarker = "nudge-hook"

func hookCommandContains(handler any) bool {
	m, ok := handler.(map[string]any)
	if !ok {
		return false
	}
	cmd, _ := m["command"].(string)
	return strings.Contains(cmd, hookMarker)
}

func eventHasRingerHook(groups []any) bool {
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		handlers, ok := gm["hooks"].([]any)
		if !ok {
			continue
		}
		for _, h := range handlers {
			if hookCommandContains(h) {
				return true
			}
		}
	}
	return false
}

// mergeRingerHook adds a ringer hook group for (event, matcher, command) unless
// one is already present (idempotent). Returns true if it modified settings.
// Type mismatches on the hooks tree are loud errors (Python raised ValueError).
func mergeRingerHook(settings map[string]any, event, matcher, command string) (bool, error) {
	hooksAny, ok := settings["hooks"]
	if !ok {
		hooksAny = map[string]any{}
		settings["hooks"] = hooksAny
	}
	hooks, ok := hooksAny.(map[string]any)
	if !ok {
		return false, fmt.Errorf("settings hooks field must be a JSON object")
	}
	groups, _ := hooks[event].([]any) // absent or wrong-type → treat as empty, then re-validate
	if raw, present := hooks[event]; present {
		if _, ok := raw.([]any); !ok {
			return false, fmt.Errorf("settings hooks.%s field must be a JSON array", event)
		}
	}
	if eventHasRingerHook(groups) {
		hooks[event] = groups
		return false, nil
	}
	groups = append(groups, map[string]any{
		"matcher": matcher,
		"hooks": []any{
			map[string]any{"type": "command", "command": command},
		},
	})
	hooks[event] = groups
	return true, nil
}

// removeRingerHooks strips every handler whose command contains hookMarker,
// pruning emptied groups and emptied events. Returns the count removed.
func removeRingerHooks(settings map[string]any) int {
	hooksAny, ok := settings["hooks"]
	if !ok {
		return 0
	}
	hooks, ok := hooksAny.(map[string]any)
	if !ok {
		return 0
	}
	removed := 0
	events := make([]string, 0, len(hooks))
	for e := range hooks {
		events = append(events, e)
	}
	sort.Strings(events)
	for _, event := range events {
		groups, ok := hooks[event].([]any)
		if !ok {
			continue
		}
		kept := []any{}
		for _, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok {
				kept = append(kept, g)
				continue
			}
			handlers, ok := gm["hooks"].([]any)
			if !ok {
				kept = append(kept, g)
				continue
			}
			keptHandlers := []any{}
			for _, h := range handlers {
				if hookCommandContains(h) {
					removed++
				} else {
					keptHandlers = append(keptHandlers, h)
				}
			}
			if len(keptHandlers) > 0 {
				ng := make(map[string]any, len(gm))
				for k, v := range gm {
					ng[k] = v
				}
				ng["hooks"] = keptHandlers
				kept = append(kept, ng)
			}
		}
		if len(kept) > 0 {
			hooks[event] = kept
		} else {
			delete(hooks, event)
		}
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	}
	return removed
}

// loadSettings reads settings.json into a map, distinguishing "absent" (→ {})
// from "invalid JSON" and "not a JSON object" (both loud errors).
func loadSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("settings file is not valid JSON: %s", path)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("settings file must contain a JSON object: %s", path)
	}
	return m, nil
}

// backupFile copies an existing file to `<name>.bak-<UTCstamp>` and returns the
// backup path ("" if the source did not exist). Mirrors ringer.py backup_file.
func backupFile(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000") + "Z"
	backup := path + ".bak-" + stamp
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(backup, data, 0o644); err != nil {
		return "", err
	}
	return backup, nil
}

// writeSettings backs up any existing file, then atomically writes the map as
// 2-space-indented, sorted-key JSON with a trailing newline and NO HTML
// escaping (matches Python json.dumps(indent=2, sort_keys=True)+"\n").
func writeSettings(path string, settings map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := backupFile(path); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(settings); err != nil { // Encode appends the trailing "\n"
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), fmt.Sprintf(".%s.%d.tmp", filepath.Base(path), os.Getpid()))
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
