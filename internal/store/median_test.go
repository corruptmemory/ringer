package store

import (
	"path/filepath"
	"testing"
)

func TestMedianAggregate(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`CREATE TABLE t(g TEXT, v REAL)`); err != nil {
		t.Fatal(err)
	}
	rows := [][2]any{{"odd", 1.0}, {"odd", 5.0}, {"odd", 3.0}, // median 3
		{"even", 10.0}, {"even", 20.0}, {"even", 40.0}, {"even", 30.0}, // median 25
		{"one", 7.0}} // median 7
	for _, r := range rows {
		if _, err := s.db.Exec(`INSERT INTO t(g,v) VALUES (?,?)`, r[0], r[1]); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]float64{"odd": 3, "even": 25, "one": 7}
	got := map[string]float64{}
	res, err := s.db.Query(`SELECT g, median(v) FROM t GROUP BY g`)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	for res.Next() {
		var g string
		var m float64
		if err := res.Scan(&g, &m); err != nil {
			t.Fatal(err)
		}
		got[g] = m
	}
	for g, w := range want {
		if got[g] != w {
			t.Fatalf("median[%s]=%v want %v", g, got[g], w)
		}
	}
	// Empty set -> NULL (not 0).
	var n any
	if err := s.db.QueryRow(`SELECT median(v) FROM t WHERE g='none'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != nil {
		t.Fatalf("median over empty set = %v, want NULL", n)
	}
}
