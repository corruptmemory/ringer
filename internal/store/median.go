// internal/store/median.go
package store

import (
	"database/sql/driver"
	"sort"

	sqlite "modernc.org/sqlite"
)

// medianAgg accumulates numeric argument values and returns their median.
// Used only as a plain aggregate (never an OVER(...) window), so
// WindowInverse is a guard, not a real sliding-window removal.
type medianAgg struct{ vals []float64 }

func (a *medianAgg) Step(_ *sqlite.FunctionContext, args []driver.Value) error {
	if len(args) == 0 || args[0] == nil {
		return nil // SQL semantics: aggregates skip NULL inputs
	}
	switch v := args[0].(type) {
	case int64:
		a.vals = append(a.vals, float64(v))
	case float64:
		a.vals = append(a.vals, v)
	}
	return nil
}

func (a *medianAgg) WindowInverse(_ *sqlite.FunctionContext, _ []driver.Value) error {
	return errMedianNotWindow
}

func (a *medianAgg) WindowValue(_ *sqlite.FunctionContext) (driver.Value, error) {
	n := len(a.vals)
	if n == 0 {
		return nil, nil // empty set -> NULL
	}
	s := append([]float64(nil), a.vals...)
	sort.Float64s(s)
	mid := n / 2
	if n%2 == 1 {
		return s[mid], nil
	}
	return (s[mid-1] + s[mid]) / 2.0, nil
}

func (a *medianAgg) Final(_ *sqlite.FunctionContext) {}

var errMedianNotWindow = driverError("median() is not supported as a window function")

type driverError string

func (e driverError) Error() string { return string(e) }

// registerMedian installs median() on every connection opened afterward.
// Idempotent-safe: guarded so repeated calls (test + prod Open) don't error.
func registerMedian() {
	if medianRegistered {
		return
	}
	medianRegistered = true
	err := sqlite.RegisterFunction("median", &sqlite.FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		MakeAggregate: func(_ sqlite.FunctionContext) (sqlite.AggregateFunction, error) {
			return &medianAgg{}, nil
		},
	})
	if err != nil {
		panic("store: register median(): " + err.Error())
	}
}

var medianRegistered bool
