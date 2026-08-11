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

// ParseSwish reads an ISO-8859-1 encoded Swish CSV export and returns one
// Report per (committee, date) pair, filtered to [start, end] inclusive.
func ParseSwish(cfg *config.Config, r io.Reader, start, end time.Time) (map[string]*Report, error) {
	decoded := charmap.ISO8859_1.NewDecoder().Reader(r)
	cr := csv.NewReader(decoded)
	cr.Comment = '*'
	cr.LazyQuotes = true

	re := regexp.MustCompile(buildRegexFromValidPrefixes(cfg))
	fallback := cfg.PrefixMappings["sk"]
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
		if len(record) < 13 {
			continue
		}

		dateStr := strings.TrimSpace(record[4]) // Bokfdag
		// Read in Location, because that is where the range boundaries come
		// from too (see ParseDay). Parsed as UTC while the caller's `end` was
		// Swedish midnight, the two sat two hours apart and the last day of
		// every range was silently dropped — a term's final Friday, every time.
		t, err := time.ParseInLocation("2006-01-02", dateStr, Location)
		if err != nil {
			continue
		}
		if t.Before(start) || t.After(end) {
			continue
		}

		message := strings.TrimSpace(record[10]) // Meddelande
		amountStr := strings.TrimSpace(record[12])
		amount, err := parseSwishAmount(amountStr)
		if err != nil || amount == 0 {
			continue
		}

		prefix, name := deconstruct(re, message)
		pa, ok := cfg.PrefixMappings[prefix]
		if prefix == "" || !ok {
			if guess, found := cfg.SwishAmountFallbacks[amount]; found {
				prefix, name = deconstruct(re, guess)
				pa, ok = cfg.PrefixMappings[prefix]
			}
			if prefix == "" || !ok {
				pa = fallback
				name = message
			}
		}

		report := getReport(reports, pa, dateStr)
		entry := Entry{
			Name:      name,
			Account:   strconv.Itoa(pa.Account),
			Quantity:  1,
			UnitPrice: amount,
			Total:     amount,
		}
		addEntry(cfg, report, entry)
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
