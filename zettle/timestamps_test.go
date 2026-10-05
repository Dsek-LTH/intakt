package zettle

import (
	"testing"
	"time"
)

func TestParseDateAcceptsZettleLayouts(t *testing.T) {
	want := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	cases := []string{
		"2025-01-15T10:30:00Z",           // RFC3339
		"2025-01-15T10:30:00.123456789Z", // RFC3339Nano
		"2025-01-15T10:30:00.000+0000",   // Zettle purchase timestamp
		"2025-01-15T10:30:00+0000",       // without millis
		"2025-01-15",                     // plain date
	}
	for _, in := range cases {
		got, ok := ParseDate(in)
		if !ok {
			t.Errorf("ParseDate(%q) failed", in)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("ParseDate(%q) = %v, want %v (UTC midnight)", in, got, want)
		}
	}
}

func TestParseDateUsesSwedishCalendarDay(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
	}{
		// 23:30 UTC in winter (CET, UTC+1) is 00:30 the next day in Sweden.
		{"2025-01-15T23:30:00.000+0000", time.Date(2025, 1, 16, 0, 0, 0, 0, time.UTC)},
		// 22:30 UTC in summer (CEST, UTC+2) is 00:30 the next day in Sweden.
		{"2025-09-10T22:30:00.000+0000", time.Date(2025, 9, 11, 0, 0, 0, 0, time.UTC)},
		// 21:30 UTC in summer is still 23:30 the same day in Sweden.
		{"2025-09-10T21:30:00.000+0000", time.Date(2025, 9, 10, 0, 0, 0, 0, time.UTC)},
		// A timestamp already carrying a local offset keeps its local date.
		{"2025-09-11T00:30:00+0200", time.Date(2025, 9, 11, 0, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		got, ok := ParseDate(c.in)
		if !ok {
			t.Errorf("ParseDate(%q) failed", c.in)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("ParseDate(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseDateRejectsGarbage(t *testing.T) {
	if _, ok := ParseDate("not a date"); ok {
		t.Error("ParseDate accepted garbage input")
	}
}
