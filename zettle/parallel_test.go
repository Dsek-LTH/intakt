package zettle

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestShardRangeCoversRangeContiguously(t *testing.T) {
	cases := []struct {
		start, end string
		n          int
		wantShards int
	}{
		{"2025-01-01", "2025-01-10", 4, 4}, // 10 days over 4 shards
		{"2025-01-01", "2025-01-02", 4, 2}, // never more shards than days
		{"2025-01-01", "2025-01-01", 4, 1}, // single day
		{"2025-01-01", "2025-01-10", 0, 1}, // n clamps to 1
	}
	for _, tc := range cases {
		got := shardRange(day(tc.start), day(tc.end), tc.n)
		if len(got) != tc.wantShards {
			t.Errorf("shardRange(%s, %s, %d) returned %d shards, want %d",
				tc.start, tc.end, tc.n, len(got), tc.wantShards)
			continue
		}
		if !got[0].start.Equal(day(tc.start)) {
			t.Errorf("first shard starts %v, want %s", got[0].start, tc.start)
		}
		if !got[len(got)-1].end.Equal(day(tc.end)) {
			t.Errorf("last shard ends %v, want %s", got[len(got)-1].end, tc.end)
		}
		for i := 1; i < len(got); i++ {
			if !got[i].start.Equal(got[i-1].end.AddDate(0, 0, 1)) {
				t.Errorf("shard %d starts %v, not adjacent to previous end %v",
					i, got[i].start, got[i-1].end)
			}
		}
	}
}

func TestShardRangePutsRemainderOnEarliestShards(t *testing.T) {
	got := shardRange(day("2025-01-01"), day("2025-01-10"), 4) // 10 = 3+3+2+2
	sizes := make([]int, len(got))
	for i, r := range got {
		sizes[i] = int(r.end.Sub(r.start).Hours()/24) + 1
	}
	want := []int{3, 3, 2, 2}
	for i := range want {
		if sizes[i] != want[i] {
			t.Fatalf("shard sizes = %v, want %v", sizes, want)
		}
	}
}

func TestFetchShardedConcatenatesInRangeOrder(t *testing.T) {
	var mu sync.Mutex
	var calls []string

	fetch := func(_ context.Context, startDate, _ string) ([]string, error) {
		mu.Lock()
		calls = append(calls, startDate)
		mu.Unlock()
		return []string{startDate}, nil
	}

	got, err := fetchSharded(context.Background(), day("2025-01-01"), day("2025-01-08"), fetch)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != maxFetchShards {
		t.Fatalf("expected %d shard calls, got %d", maxFetchShards, len(calls))
	}
	// Results must be in range order regardless of goroutine completion order.
	for i := 1; i < len(got); i++ {
		if strings.Compare(got[i-1], got[i]) >= 0 {
			t.Fatalf("results out of range order: %v", got)
		}
	}
}

func TestFetchShardedReturnsFirstShardError(t *testing.T) {
	sentinel := errors.New("shard failed")
	fetch := func(_ context.Context, startDate, _ string) ([]int, error) {
		if strings.HasPrefix(startDate, "2025-01-01") {
			return nil, fmt.Errorf("first: %w", sentinel)
		}
		return []int{1}, nil
	}

	_, err := fetchSharded(context.Background(), day("2025-01-01"), day("2025-01-08"), fetch)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the failing shard's error", err)
	}
}
