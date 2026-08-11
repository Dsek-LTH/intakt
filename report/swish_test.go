package report

import (
	"bufio"
	"fmt"
	"intakt/config"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// The golden file is the manifest the CLI wrote from this same CSV before any
// of the service refactoring — one line per report, "<key>\t<date>\t<total>".
// It is the whole point of this test: the parsing, the prefix routing, the
// per-day grouping and the amount fallbacks all have to keep producing exactly
// what the treasurer already booked.
const (
	goldenPath = "testdata/swish_reports.golden"
	csvPath    = "testdata/swish.csv"
	configPath = "testdata/config.yaml"
)

// The range the manifest was compiled with. The fixture CSV runs a few days
// past it, which is deliberate — it also exercises the date filter.
//
// Built with ParseDay rather than time.Date, because ParseDay is what every
// real caller uses and the two are not the same instant. Constructing these at
// UTC midnight made the test agree with a bug instead of with the service: the
// parser read CSV dates as UTC too, so the pair were consistent with each other
// and wrong together, and the last day of every range was silently dropped once
// the boundaries became Swedish.
var (
	goldenStart = mustParseDay("2026-01-01")
	goldenEnd   = mustParseDay("2026-05-31")
)

func mustParseDay(s string) time.Time {
	t, err := ParseDay(s)
	if err != nil {
		panic(err)
	}
	return t
}

func loadTestConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("loading test config: %v", err)
	}
	return cfg
}

func parseGoldenCSV(t *testing.T, cfg *config.Config, start, end time.Time) map[string]*Report {
	t.Helper()
	f, err := os.Open(csvPath)
	if err != nil {
		t.Fatalf("opening fixture CSV: %v", err)
	}
	defer f.Close()

	reports, err := ParseSwish(cfg, f, start, end)
	if err != nil {
		t.Fatalf("ParseSwish: %v", err)
	}
	return reports
}

// reportLine renders a report the same way the golden file records it, so the
// two are compared as plain strings and a mismatch reads as a diff rather than
// as a struct dump.
func reportLine(key string, r *Report) string {
	return fmt.Sprintf("%s\t%s\t%s", key, r.Date, crowns(r.Total))
}

func readGolden(t *testing.T) []string {
	t.Helper()
	f, err := os.Open(goldenPath)
	if err != nil {
		t.Fatalf("opening golden file: %v", err)
	}
	defer f.Close()

	var lines []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		if line := strings.TrimRight(s.Text(), "\r"); line != "" {
			lines = append(lines, line)
		}
	}
	if err := s.Err(); err != nil {
		t.Fatalf("reading golden file: %v", err)
	}
	return lines
}

// TestParseSwishMatchesGolden is the regression net for the whole Swish path.
func TestParseSwishMatchesGolden(t *testing.T) {
	cfg := loadTestConfig(t)
	reports := parseGoldenCSV(t, cfg, goldenStart, goldenEnd)

	got := make([]string, 0, len(reports))
	for key, r := range reports {
		got = append(got, reportLine(key, r))
	}
	slices.Sort(got)

	want := readGolden(t)
	slices.Sort(want)

	if len(got) != len(want) {
		t.Errorf("report count = %d, want %d", len(got), len(want))
	}

	// Compare as sets so a missing report and a spurious one are reported
	// separately — with 146 rows, a positional diff after one insertion is
	// unreadable.
	inWant := make(map[string]bool, len(want))
	for _, line := range want {
		inWant[line] = true
	}
	inGot := make(map[string]bool, len(got))
	for _, line := range got {
		inGot[line] = true
	}

	for _, line := range got {
		if !inWant[line] {
			t.Errorf("unexpected report: %s", line)
		}
	}
	for _, line := range want {
		if !inGot[line] {
			t.Errorf("missing report:    %s", line)
		}
	}
}

// The sum over every report is what actually reaches the ledger, so it is
// worth asserting on its own: a routing bug that moved money between
// committees would leave the total untouched, but a parsing bug that dropped
// or double-counted a payment would not.
func TestParseSwishTotal(t *testing.T) {
	cfg := loadTestConfig(t)
	reports := parseGoldenCSV(t, cfg, goldenStart, goldenEnd)

	total := 0
	for _, r := range reports {
		total += r.Total
	}

	const want = 2808100 // öre — the sum of the golden file's totals
	if total != want {
		t.Errorf("total = %s kr, want %s kr", crowns(total), crowns(want))
	}
}

// Every report's own total has to equal the sum of its product lines and the
// sum of its account distributions. The distributions are what become voucher
// rows, so a report where these three disagree is one that would push an
// unbalanced verifikation to Fortnox.
func TestParseSwishReportsAreInternallyConsistent(t *testing.T) {
	cfg := loadTestConfig(t)
	reports := parseGoldenCSV(t, cfg, goldenStart, goldenEnd)

	if len(reports) == 0 {
		t.Fatal("no reports parsed")
	}

	for key, r := range reports {
		products := 0
		for _, p := range r.Products {
			products += p.Total
		}
		if products != r.Total {
			t.Errorf("%s: product lines sum to %s, report total is %s",
				key, crowns(products), crowns(r.Total))
		}

		distributed := 0
		for _, d := range r.AccountDistributions {
			distributed += d.Total
		}
		if distributed != r.Total {
			t.Errorf("%s: account distributions sum to %s, report total is %s",
				key, crowns(distributed), crowns(r.Total))
		}
	}
}

// The date filter is inclusive at both ends. The fixture runs to 2026-06-06,
// so widening the range past the golden end date has to pull in more.
func TestParseSwishDateRangeIsInclusive(t *testing.T) {
	cfg := loadTestConfig(t)

	narrow := parseGoldenCSV(t, cfg, goldenStart, goldenEnd)
	wide := parseGoldenCSV(t, cfg, goldenStart, mustParseDay("2026-06-30"))

	if len(wide) <= len(narrow) {
		t.Errorf("widening the range gave %d reports, not more than %d", len(wide), len(narrow))
	}

	// A single day, taken at the boundary, must still produce that day's report.
	oneDay := parseGoldenCSV(t, cfg, goldenEnd, goldenEnd)
	if len(oneDay) == 0 {
		t.Errorf("no reports for the single day %s, which the golden file covers",
			goldenEnd.Format("2006-01-02"))
	}
	for key, r := range oneDay {
		if r.Date != goldenEnd.Format("2006-01-02") {
			t.Errorf("%s: date %s outside the single-day range", key, r.Date)
		}
	}
}

// Payments whose message carries no usable prefix are routed by exact amount
// through swish_amount_fallbacks. The fixture contains bare "Monster" and
// "Kaffe" messages, plus three empty ones, so this path is live here.
func TestParseSwishAmountFallback(t *testing.T) {
	cfg := loadTestConfig(t)
	if len(cfg.SwishAmountFallbacks) == 0 {
		t.Skip("test config defines no amount fallbacks")
	}

	reports := parseGoldenCSV(t, cfg, goldenStart, goldenEnd)

	// Every product line has to have landed on an account the config knows
	// about — the fallback routing included. An unmapped account reaching a
	// voucher row is what Fortnox rejects.
	for key, r := range reports {
		for _, p := range r.Products {
			if _, ok := cfg.AccountDescriptions[p.Account]; !ok {
				t.Errorf("%s: product %q booked to unknown account %s", key, p.Name, p.Account)
			}
		}
	}
}
