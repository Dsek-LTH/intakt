package report

import (
	"testing"
	"time"
)

func TestLocationIsStockholm(t *testing.T) {
	if Location == time.UTC {
		t.Fatal("Location fell back to UTC — the embedded tzdata is not being used")
	}
	if got := Location.String(); got != "Europe/Stockholm" {
		t.Errorf("Location = %s, want Europe/Stockholm", got)
	}
}

// Zettle's timestamps are UTC instants, so the Swedish day they fall on is
// found by converting to Europe/Stockholm: a purchase late on a summer
// evening in UTC is already past midnight in Lund.
func TestParseZettleDateConvertsToSwedishDay(t *testing.T) {
	tests := []struct {
		name      string
		timestamp string
		want      string
	}{
		{
			name:      "a late summer evening in Sweden stays on its day",
			timestamp: "2026-07-01T21:30:00.000+0000", // 23:30 CEST
			want:      "2026-07-01",
		},
		{
			name:      "past Swedish midnight in summer is the next day",
			timestamp: "2026-07-01T22:30:00.000+0000", // 00:30 CEST
			want:      "2026-07-02",
		},
		{
			name:      "past Swedish midnight in winter is the next day",
			timestamp: "2026-01-01T23:30:00.000+0000", // 00:30 CET
			want:      "2026-01-02",
		},
		{
			name:      "ordinary daytime purchase",
			timestamp: "2026-03-15T14:22:31.000+0000",
			want:      "2026-03-15",
		},
		{
			name:      "an explicit Swedish offset is honoured",
			timestamp: "2026-01-02T00:30:00.000+0100",
			want:      "2026-01-02",
		},
		{
			// The finance API leaves the offset off; it is read as UTC.
			name:      "no offset at all",
			timestamp: "2026-07-01T22:30:00.000",
			want:      "2026-07-02",
		},
		{
			name:      "no offset and no fraction",
			timestamp: "2026-07-01T21:30:00",
			want:      "2026-07-01",
		},
		{
			name:      "a bare date",
			timestamp: "2026-03-15",
			want:      "2026-03-15",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseZettleDate(tt.timestamp); got != tt.want {
				t.Errorf("parseZettleDate(%q) = %q, want %q", tt.timestamp, got, tt.want)
			}
		})
	}
}

// An unparseable timestamp is passed through rather than silently becoming a
// wrong date. It will stand out as a nonsense report key, which is the point.
func TestParseZettleDatePassesThroughGarbage(t *testing.T) {
	const garbage = "not a timestamp"
	if got := parseZettleDate(garbage); got != garbage {
		t.Errorf("parseZettleDate(%q) = %q, want it returned unchanged", garbage, got)
	}
}

// ParseDay is what turns a date a person typed into a range boundary. Midnight
// in Sweden is 23:00 the previous day in UTC during winter — if this returned
// UTC midnight, the first hour of the start day would fall outside the window.
func TestParseDayIsSwedishMidnight(t *testing.T) {
	day, err := ParseDay("2026-01-01")
	if err != nil {
		t.Fatalf("ParseDay: %v", err)
	}

	if h, m, s := day.Clock(); h != 0 || m != 0 || s != 0 {
		t.Errorf("ParseDay gave %02d:%02d:%02d, want midnight", h, m, s)
	}
	if got := day.Format("2006-01-02T15:04:05-07:00"); got != "2026-01-01T00:00:00+01:00" {
		t.Errorf("ParseDay = %s, want 2026-01-01T00:00:00+01:00", got)
	}

	summer, err := ParseDay("2026-07-01")
	if err != nil {
		t.Fatalf("ParseDay: %v", err)
	}
	if got := summer.Format("2006-01-02T15:04:05-07:00"); got != "2026-07-01T00:00:00+02:00" {
		t.Errorf("ParseDay = %s, want 2026-07-01T00:00:00+02:00 (summer time)", got)
	}
}

// The round trip that has to hold for the requested window and the grouping to
// agree: every minute of a Swedish day, written as the UTC instant Zettle
// sends, must be grouped into that day and no other.
func TestEveryMinuteOfADayGroupsIntoIt(t *testing.T) {
	start, err := ParseDay("2026-07-01")
	if err != nil {
		t.Fatalf("ParseDay: %v", err)
	}

	for minutes := 0; minutes < 24*60; minutes += 17 {
		clock := start.Add(time.Duration(minutes) * time.Minute).UTC().Format("2006-01-02T15:04:05.000-0700")
		if got := parseZettleDate(clock); got != "2026-07-01" {
			t.Fatalf("a purchase %d minutes into 2026-07-01 (%s) was grouped as %s", minutes, clock, got)
		}
	}

	// And one minute past the end of the day is the next day, not the same one.
	closing := start.Add(24 * time.Hour).UTC().Format("2006-01-02T15:04:05.000-0700")
	if got := parseZettleDate(closing); got != "2026-07-02" {
		t.Errorf("midnight ending 2026-07-01 was grouped as %s, want 2026-07-02", got)
	}
}
