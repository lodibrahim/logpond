package search

import (
	"fmt"
	"testing"
	"time"

	"github.com/lodibrahim/logpond/internal/parser"
	"github.com/lodibrahim/logpond/internal/store"
)

func populatedStore() *store.Store {
	s := store.New(100)
	entries := []struct {
		i    int
		sev  string
		body string
		sym  string
	}{
		{0, "INFO", "PM: Flat → Entering", "NVDA"},
		{1, "INFO", "Paper fill", "NVDA"},
		{2, "WARN", "Entry signal rejected", "SOXL"},
		{3, "DEBUG", "Entry fill applied", "NVDA"},
		{4, "INFO", "PM: Exiting → Flat", "SOXL"},
	}
	for _, e := range entries {
		s.Append(&parser.Entry{
			Timestamp: time.Date(2026, 2, 19, 14, 14, e.i, 0, time.UTC),
			Severity:  e.sev,
			Body:      e.body,
			Fields:    map[string]string{"symbol": e.sym, "component": "strategy"},
			Raw:       fmt.Sprintf(`{"i":%d}`, e.i),
		})
	}
	return s
}

func TestSearchByText(t *testing.T) {
	s := populatedStore()
	results, err := Run(s, Query{Text: "Paper"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len = %d, want 1", len(results))
	}
	if results[0].Body != "Paper fill" {
		t.Errorf("Body = %q, want %q", results[0].Body, "Paper fill")
	}
}

func TestSearchByRegex(t *testing.T) {
	s := populatedStore()
	results, err := Run(s, Query{Text: "PM:.*Flat"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}
}

func TestSearchByLevel(t *testing.T) {
	s := populatedStore()
	results, _ := Run(s, Query{Level: "WARN"})
	if len(results) != 1 {
		t.Fatalf("len = %d, want 1", len(results))
	}
	if results[0].Body != "Entry signal rejected" {
		t.Errorf("Body = %q", results[0].Body)
	}
}

func TestSearchByField(t *testing.T) {
	s := populatedStore()
	results, _ := Run(s, Query{Fields: map[string]string{"symbol": "SOXL"}})
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}
}

func TestSearchCombined(t *testing.T) {
	s := populatedStore()
	results, _ := Run(s, Query{
		Level:  "INFO",
		Fields: map[string]string{"symbol": "NVDA"},
	})
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2 (PM:Flat + Paper fill)", len(results))
	}
}

func TestSearchWithLimit(t *testing.T) {
	s := populatedStore()
	results, _ := Run(s, Query{Limit: 2})
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}
	// Limit returns the newest N matches
	if results[1].Body != "PM: Exiting → Flat" {
		t.Errorf("Last result = %q, want newest entry", results[1].Body)
	}
}

func TestSearchByTimeRange(t *testing.T) {
	s := populatedStore()
	// After is strictly greater-than, so second=1 means entries at 2,3,4 match
	after := time.Date(2026, 2, 19, 14, 14, 1, 0, time.UTC)
	results, _ := Run(s, Query{After: after})
	if len(results) != 3 {
		t.Fatalf("len = %d, want 3", len(results))
	}
}

func TestSearchByBeforeTime(t *testing.T) {
	s := populatedStore()
	// Before is strictly less-than, so second=3 means entries at 0,1,2 match
	before := time.Date(2026, 2, 19, 14, 14, 3, 0, time.UTC)
	results, _ := Run(s, Query{Before: before})
	if len(results) != 3 {
		t.Fatalf("len = %d, want 3", len(results))
	}
}

func TestSearchInvalidRegex(t *testing.T) {
	s := populatedStore()
	_, err := Run(s, Query{Text: "["})
	if err == nil {
		t.Error("Should fail for invalid regex")
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	s := populatedStore()
	results, _ := Run(s, Query{})
	if len(results) != 5 {
		t.Fatalf("len = %d, want 5 (all entries)", len(results))
	}
}
