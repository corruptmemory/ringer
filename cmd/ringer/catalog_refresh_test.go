package main

import (
	"testing"
	"time"
)

func TestCatalogIsStale(t *testing.T) {
	now := "2026-07-10T12:00:00Z"
	fresh := "2026-07-10T00:00:00Z" // 12h ago
	stale := "2026-07-08T00:00:00Z" // 60h ago
	if catalogIsStale(fresh, now, 24*time.Hour) {
		t.Fatal("12h-old catalog should be fresh")
	}
	if !catalogIsStale(stale, now, 24*time.Hour) {
		t.Fatal("60h-old catalog should be stale")
	}
	if !catalogIsStale("", now, 24*time.Hour) {
		t.Fatal("empty (never fetched) should be stale")
	}
}
