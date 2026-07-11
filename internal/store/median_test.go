package store

import (
	"fmt"
	"path/filepath"
	"sync"
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

// TestMedianConcurrentRegistration exercises registerMedian() from many
// goroutines at once, the way net/http serves one goroutine per connection
// and the HUD opens a store inside each request handler (handleModels ->
// store.Open -> registerMedian). Existing tests never Open concurrently, so
// this is the only test that puts registerMedian's guard under -race.
func TestMedianConcurrentRegistration(t *testing.T) {
	const n = 16
	var wg sync.WaitGroup
	var ready sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, n)
	ready.Add(n)
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready.Done()
			<-start // all n goroutines hit registerMedian() at nearly the same instant
			s, err := Open(filepath.Join(t.TempDir(), fmt.Sprintf("m%d.db", i)))
			if err != nil {
				errs <- fmt.Errorf("worker %d: Open: %w", i, err)
				return
			}
			defer s.Close()
			if _, err := s.db.Exec(`CREATE TABLE t(v REAL)`); err != nil {
				errs <- fmt.Errorf("worker %d: create table: %w", i, err)
				return
			}
			for _, v := range []float64{1, 2, 3} {
				if _, err := s.db.Exec(`INSERT INTO t(v) VALUES (?)`, v); err != nil {
					errs <- fmt.Errorf("worker %d: insert: %w", i, err)
					return
				}
			}
			var got float64
			if err := s.db.QueryRow(`SELECT median(v) FROM t`).Scan(&got); err != nil {
				errs <- fmt.Errorf("worker %d: select median: %w", i, err)
				return
			}
			if got != 2 {
				errs <- fmt.Errorf("worker %d: median = %v, want 2", i, got)
			}
		}()
	}
	ready.Wait() // wait for every goroutine to be parked on <-start
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
