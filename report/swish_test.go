package report

import (
	"strings"
	"testing"
	"time"
)

// swishCSV builds a minimal Swish export: a comment line, a header line, then
// the given records. The real export is ISO-8859-1; ASCII test data is a
// subset, and non-ASCII is covered separately.
func swishCSV(records ...string) string {
	return "* Swishrapport\n" +
		"Radnr,Clnr,Kontonr,Produkt,Bokfdag,Transdag,Valutadag,Referens,Avsändare,Mottagare,Meddelande,Transaktionstyp,Belopp\n" +
		strings.Join(records, "\n") + "\n"
}

func swishRecord(date, message, amount string) string {
	return ",,,," + date + ",,,,,," + message + ",," + amount
}

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestParseSwishRoutesAndFilters(t *testing.T) {
	cfg := testConfig()
	csv := swishCSV(
		swishRecord("2025-01-10", "CA - Kaffe", "25.00"),
		swishRecord("2025-01-10", "no prefix here", "10.00"), // falls back to sk
		swishRecord("2025-02-01", "CA - Kaffe", "25.00"),     // outside range
	)

	reports, err := ParseSwish(cfg, strings.NewReader(csv),
		mustDate(t, "2025-01-01"), mustDate(t, "2025-01-31"))
	if err != nil {
		t.Fatal(err)
	}

	cafe, ok := reports["cafe-2025-01-10"]
	if !ok {
		t.Fatalf("missing cafe report, got %v", reports)
	}
	if cafe.Total != 2500 {
		t.Errorf("cafe total = %d öre, want 2500", cafe.Total)
	}
	sk, ok := reports["skatt-2025-01-10"]
	if !ok {
		t.Fatalf("missing fallback skatt report, got %v", reports)
	}
	if sk.Total != 1000 {
		t.Errorf("skatt total = %d öre, want 1000", sk.Total)
	}
	if len(reports) != 2 {
		t.Errorf("out-of-range record leaked in: %v", reports)
	}
}

func TestParseSwishGuessesProductByAmount(t *testing.T) {
	cfg := testConfig() // 2500 öre → "CA - Kaffe"
	csv := swishCSV(swishRecord("2025-01-10", "", "25.00"))

	reports, err := ParseSwish(cfg, strings.NewReader(csv),
		mustDate(t, "2025-01-01"), mustDate(t, "2025-01-31"))
	if err != nil {
		t.Fatal(err)
	}

	cafe, ok := reports["cafe-2025-01-10"]
	if !ok {
		t.Fatalf("amount fallback not applied, got %v", reports)
	}
	if cafe.Products[0].Name != "Kaffe" {
		t.Errorf("guessed entry name = %q, want %q", cafe.Products[0].Name, "Kaffe")
	}
}

func TestParseSwishAmount(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"25.00", 2500, false},
		{"0.05", 5, false},
		{"1234.99", 123499, false},
		{"25", 0, true},     // no decimals
		{"25.5", 0, true},   // one decimal digit
		{"abc.de", 0, true}, // not a number
	}
	for _, tc := range cases {
		got, err := parseSwishAmount(tc.in)
		if tc.wantErr != (err != nil) {
			t.Errorf("parseSwishAmount(%q) error = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("parseSwishAmount(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
