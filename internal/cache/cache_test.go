package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type item struct {
	ID string `json:"id"`
	TS string `json:"ts"`
}

func itemKey(i item) string { return i.ID }
func itemTS(i item) string  { return i.TS }
func d(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestMergeAndWriteDeduplicatesAndMergesRanges(t *testing.T) {
	dir := t.TempDir()

	// Seed an existing cache file for January 1–10.
	first := []item{{ID: "a", TS: "2025-01-05"}, {ID: "b", TS: "2025-01-06"}}
	if _, err := MergeAndWrite(dir, "purchases", d("2025-01-01"), d("2025-01-10"), first, itemKey); err != nil {
		t.Fatal(err)
	}

	// Fetch an overlapping range with one duplicate ("b") and one new item.
	second := []item{{ID: "b", TS: "2025-01-06"}, {ID: "c", TS: "2025-01-15"}}
	path, err := MergeAndWrite(dir, "purchases", d("2025-01-05"), d("2025-01-20"), second, itemKey)
	if err != nil {
		t.Fatal(err)
	}

	// The merged file covers the union of both ranges.
	if want := "purchases-2025-01-01--2025-01-20.json"; filepath.Base(path) != want {
		t.Errorf("merged file = %s, want %s", filepath.Base(path), want)
	}

	items, err := ReadFile[item](path)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Errorf("merged items = %+v, want 3 deduplicated items", items)
	}

	// The superseded file is deleted.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected old cache file to be removed, dir has %d files", len(entries))
	}
}

func TestFindCoveringMatchesOnlyFullCoverage(t *testing.T) {
	dir := t.TempDir()
	if _, err := MergeAndWrite(dir, "finance", d("2025-01-01"), d("2025-01-31"), []item{}, itemKey); err != nil {
		t.Fatal(err)
	}

	if _, ok := FindCovering(dir, "finance", d("2025-01-10"), d("2025-01-20")); !ok {
		t.Error("sub-range of the cached span should be covered")
	}
	if _, ok := FindCovering(dir, "finance", d("2025-01-10"), d("2025-02-05")); ok {
		t.Error("range extending past the cached span must not be covered")
	}
	if _, ok := FindCovering(dir, "purchases", d("2025-01-10"), d("2025-01-20")); ok {
		t.Error("a different prefix must not match")
	}
}

func TestFilterByDateKeepsInclusiveRange(t *testing.T) {
	// Timestamps are UTC but items are bucketed by Swedish calendar day
	// (UTC+1 in January): 23:00Z belongs to the next Swedish day.
	items := []item{
		{ID: "before", TS: "2025-01-09T22:00:00Z"},
		{ID: "start", TS: "2025-01-09T23:00:00Z"},
		{ID: "end", TS: "2025-01-20T22:59:00Z"},
		{ID: "after", TS: "2025-01-20T23:00:00Z"},
		{ID: "bad", TS: "garbage"},
	}
	got := FilterByDate(items, d("2025-01-10"), d("2025-01-20"), itemTS)
	if len(got) != 2 || got[0].ID != "start" || got[1].ID != "end" {
		t.Errorf("FilterByDate = %+v, want [start end]", got)
	}
}
