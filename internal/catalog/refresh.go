// internal/catalog/refresh.go
package catalog

import "time"

// CatalogStore is the store surface Refresh needs (satisfied by *store.Store).
type CatalogStore interface {
	CatalogModels() ([]Model, error)
	ReplaceCatalog([]Model) error
	AppendCatalogEvents([]Event) error
}

type Result struct {
	Models []Model
	Events []Event
}

// Refresh fetches the catalog from source, diffs it against the current
// table, appends change events, and replaces the table. The DB is the
// snapshot; there is no JSON file. Events are appended before the table is
// replaced so a crash duplicates (recoverable) rather than loses events.
func Refresh(s CatalogStore, source string, timeout time.Duration, now string) (Result, error) {
	old, err := s.CatalogModels()
	if err != nil {
		return Result{}, err
	}
	payload, err := Fetch(source, timeout)
	if err != nil {
		return Result{}, err
	}
	models, err := NormalizePayload(payload, now)
	if err != nil {
		return Result{}, err
	}
	events := Diff(old, models, now)
	if err := s.AppendCatalogEvents(events); err != nil {
		return Result{}, err
	}
	if err := s.ReplaceCatalog(models); err != nil {
		return Result{}, err
	}
	return Result{Models: models, Events: events}, nil
}
