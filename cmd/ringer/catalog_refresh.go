// cmd/ringer/catalog_refresh.go
package main

import (
	"time"

	"github.com/corruptmemory/ringer/internal/catalog"
	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/store"
)

// catalogIsStale reports whether the newest catalog fetch is older than
// maxAge. An empty or unparseable timestamp counts as stale.
func catalogIsStale(newestFetchedAt, now string, maxAge time.Duration) bool {
	if newestFetchedAt == "" {
		return true
	}
	f, err := time.Parse(time.RFC3339, newestFetchedAt)
	if err != nil {
		return true
	}
	n, err := time.Parse(time.RFC3339, now)
	if err != nil {
		return true
	}
	return n.Sub(f) > maxAge
}

// maybeRefreshCatalog triggers a best-effort background catalog refresh if the
// stored catalog is stale. Never blocks the run; failures are logged, not fatal.
func maybeRefreshCatalog(s *store.Store, source string, lg logging.Logger, now string) {
	newest, err := s.NewestCatalogFetchedAt()
	if err != nil {
		lg.Warnf("catalog auto-refresh: freshness check: %v", err)
		return
	}
	if !catalogIsStale(newest, now, catalog.AutoRefreshMaxAge) {
		return
	}
	go func() {
		if _, err := catalog.Refresh(s, source, catalog.FetchTimeout, now); err != nil {
			lg.Warnf("catalog auto-refresh: %v", err)
		} else {
			lg.Infof("catalog auto-refreshed from %s", source)
		}
	}()
}

// catalogSourceOrDefault returns cfg's configured catalog source override, or
// catalog.DefaultSource when none is set.
func catalogSourceOrDefault(cfg *config.AppConfig) string {
	if src := cfg.CatalogSource(); src != "" {
		return src
	}
	return catalog.DefaultSource
}
