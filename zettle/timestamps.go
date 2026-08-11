package zettle

import "time"

var timestampLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.000-0700",
	"2006-01-02T15:04:05-0700",
	// Both without an offset, which is how the finance API writes them — the
	// purchase API stamps a zone, the transactions behind the fees do not.
	"2006-01-02T15:04:05.000",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// ParseDate reads a Zettle timestamp and returns the calendar date it fell on.
//
// The wall clock is taken as written and never converted, which is why the
// result is assembled from t's own Y/M/D rather than shifted into a location.
// This looks wrong and is not: Zettle's documentation says its timestamps are
// UTC, and it stamps "+0000" on them, but the clock it sends is the merchant's
// own — Swedish time for a Swedish till. Converting to Europe/Stockholm
// therefore adds an hour or two to a time that is already Swedish, and an
// evening's last sales walk over into the next day. Selling until 23:00 in
// summer moved everything after 22:00 onto tomorrow's report.
//
// So the day boundary is Swedish because Zettle's clock is Swedish. Anything
// that genuinely carries a zone — a range a treasurer typed, a Swish export —
// goes through report.ParseDay instead; see report.Location.
func ParseDate(ts string) (time.Time, bool) {
	for _, layout := range timestampLayouts {
		if t, err := time.Parse(layout, ts); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), true
		}
	}
	return time.Time{}, false
}
