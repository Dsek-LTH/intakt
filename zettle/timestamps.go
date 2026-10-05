package zettle

import (
	"time"
	_ "time/tzdata" // fallback zoneinfo for systems without a tz database
)

var timestampLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.000-0700",
	"2006-01-02T15:04:05-0700",
	"2006-01-02",
}

var stockholm = mustLoadStockholm()

func mustLoadStockholm() *time.Location {
	loc, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		panic("zettle: loading Europe/Stockholm timezone: " + err.Error())
	}
	return loc
}

// ParseDate parses a Zettle timestamp and returns its Swedish calendar date
// (as UTC midnight). Zettle reports timestamps in UTC, so purchases made
// between midnight and 01:00/02:00 local time belong to the day after their
// UTC date.
func ParseDate(ts string) (time.Time, bool) {
	for _, layout := range timestampLayouts {
		if t, err := time.Parse(layout, ts); err == nil {
			t = t.In(stockholm)
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), true
		}
	}
	return time.Time{}, false
}
