package store

import (
	"path/filepath"
	"testing"
)

func TestSchemaV2TablesExist(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tbl := range []string{"attempts", "catalog_models", "catalog_events", "schema_version"} {
		if _, err := s.db.Exec(`SELECT COUNT(*) FROM ` + tbl); err != nil {
			t.Fatalf("table %s not queryable: %v", tbl, err)
		}
	}
}
