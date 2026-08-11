package report

import (
	"log"
	"time"

	// The service runs in a scratch-ish container, and a container without
	// tzdata resolves every location to UTC silently — which is exactly the
	// failure this file exists to prevent. Embedding the database costs a few
	// hundred kilobytes and removes the dependency on the image.
	_ "time/tzdata"
)

// Location is the timezone the section's books are kept in: the day a report
// covers is a Swedish day, midnight to midnight in Lund.
//
// It is used to read dates, not to convert instants. Zettle's timestamps are
// already a Swedish wall clock however they are labelled — see parseZettleDate,
// which is where converting them did real damage — so the only things that pass
// through here are dates that carry no zone at all: a range a treasurer typed,
// a Transdag column in a Swish export.
var Location = mustLoad("Europe/Stockholm")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		// Falling back to UTC keeps the process alive but shifts every day
		// boundary by an hour or two, so it is worth saying out loud.
		log.Printf("warning: could not load timezone %s (%v); falling back to UTC", name, err)
		return time.UTC
	}
	return loc
}

// ParseDay reads a YYYY-MM-DD date as midnight in Location. Use it for anything
// a person typed: "2026-01-01" from a treasurer means midnight in Lund.
func ParseDay(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", s, Location)
}
