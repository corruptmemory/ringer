package config

import (
	"os"
	"path/filepath"
	"strings"
)

func ResolveIdentity(flagValue string, cfg *AppConfig, startDir string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv("FLEET_IDENTITY"); v != "" {
		return v
	}
	if v := os.Getenv("RINGER_IDENTITY"); v != "" {
		return v
	}
	for dir := startDir; ; dir = filepath.Dir(dir) {
		b, err := os.ReadFile(filepath.Join(dir, ".fleet-agent"))
		if err == nil {
			if id := strings.TrimSpace(string(b)); id != "" {
				return id
			}
		}
		if dir == filepath.Dir(dir) { // reached filesystem root
			break
		}
	}
	if cfg != nil && cfg.IdentityDefault != "" {
		return cfg.IdentityDefault
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "ringer"
	}
	return strings.Split(host, ".")[0] // short hostname
}
