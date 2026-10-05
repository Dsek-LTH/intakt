package report

import (
	"encoding/csv"
	"fmt"
	"intakt/config"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
)

// Column layout of the Swish CSV export (see the bank's "Transaktionsrapport").
const (
	swishColTransDate = 3  // Transdag
	swishColMessage   = 10 // Meddelande
	swishColAmount    = 12 // Belopp
	swishMinColumns   = 13
)

// swishPayment is one accepted payment row of the export.
type swishPayment struct {
	date    string // YYYY-MM-DD transaction date (Transdag)
	message string // free-text message, usually "<prefix> - <product>"
	amount  int    // öre
}

// parseSwishRecord extracts a payment from one CSV record. ok=false marks rows
// to skip silently: summary/short rows, unparsable dates or amounts, zero
// amounts, and dates outside [start, end] — all expected in a raw export.
func parseSwishRecord(record []string, start, end time.Time) (swishPayment, bool) {
	if len(record) < swishMinColumns {
		return swishPayment{}, false
	}
	date := strings.TrimSpace(record[swishColTransDate])
	t, err := time.Parse("2006-01-02", date)
	if err != nil || t.Before(start) || t.After(end) {
		return swishPayment{}, false
	}
	amount, err := parseSwishAmount(strings.TrimSpace(record[swishColAmount]))
	if err != nil || amount == 0 {
		return swishPayment{}, false
	}
	return swishPayment{
		date:    date,
		message: strings.TrimSpace(record[swishColMessage]),
		amount:  amount,
	}, true
}

// routeSwishPayment resolves the mapping a payment books to: first by message
// prefix, then by the configured per-amount product guess, finally the
// fallback mapping with the raw message as the product name.
func routeSwishPayment(cfg *config.Config, re *regexp.Regexp, p swishPayment) (config.PrefixAccount, string) {
	prefix, name := deconstruct(re, p.message)
	if pa, ok := cfg.PrefixMappings[prefix]; prefix != "" && ok {
		return pa, name
	}
	if guess, found := cfg.SwishAmountFallbacks[p.amount]; found {
		prefix, name = deconstruct(re, guess)
		if pa, ok := cfg.PrefixMappings[prefix]; prefix != "" && ok {
			return pa, name
		}
	}
	return cfg.FallbackPrefixAccount(), p.message
}

// ParseSwish reads an ISO-8859-1 encoded Swish CSV export and returns one
// Report per (committee, date) pair, filtered to [start, end] inclusive.
func ParseSwish(cfg *config.Config, r io.Reader, start, end time.Time) (map[string]*Report, error) {
	decoded := charmap.ISO8859_1.NewDecoder().Reader(r)
	cr := csv.NewReader(decoded)
	cr.Comment = '*'
	cr.LazyQuotes = true

	re := regexp.MustCompile(buildRegexFromValidPrefixes(cfg))
	reports := make(map[string]*Report)

	header := true
	for {
		record, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading swish CSV: %w", err)
		}
		if header {
			header = false
			continue
		}

		p, ok := parseSwishRecord(record, start, end)
		if !ok {
			continue
		}
		pa, name := routeSwishPayment(cfg, re, p)
		addEntry(cfg, getReport(reports, pa, p.date), Entry{
			Name:      name,
			Account:   strconv.Itoa(pa.Account),
			Quantity:  1,
			UnitPrice: p.amount,
			Total:     p.amount,
		})
	}

	return reports, nil
}

func parseSwishAmount(s string) (int, error) {
	parts := strings.SplitN(s, ".", 2)
	if len(parts) != 2 || len(parts[1]) != 2 {
		return 0, fmt.Errorf("invalid swish amount %q", s)
	}
	whole, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("invalid swish amount %q", s)
	}
	cents, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("invalid swish amount %q", s)
	}
	return whole*100 + cents, nil
}
