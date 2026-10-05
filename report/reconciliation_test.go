package report

import (
	"intakt/zettle"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseReconciliationAggregatesAndSorts(t *testing.T) {
	txs := []zettle.TransactionFee{
		{Timestamp: "2025-01-11T10:00:00.000+0000", Amount: -5000, OriginatorTransactionType: "PAYOUT"},
		{Timestamp: "2025-01-10T10:00:00.000+0000", Amount: 2000, OriginatorTransactionType: "PAYMENT"},
		{Timestamp: "2025-01-10T12:00:00.000+0000", Amount: 3000, OriginatorTransactionType: "PAYMENT"},
		{Timestamp: "2025-01-10T12:00:00.000+0000", Amount: -50, OriginatorTransactionType: "PAYMENT_FEE"},
		{Timestamp: "2025-01-10T13:00:00.000+0000", Amount: 0, OriginatorTransactionType: "PAYMENT"}, // ignored
	}

	rows := ParseReconciliation(txs)

	if len(rows) != 3 {
		t.Fatalf("expected 3 aggregated rows, got %+v", rows)
	}
	// Sorted by date, then PAYMENT < PAYMENT_FEE < PAYOUT.
	if rows[0].Type != "PAYMENT" || rows[0].Amount != 5000 {
		t.Errorf("row 0 = %+v, want aggregated PAYMENT of 5000", rows[0])
	}
	if rows[1].Type != "PAYMENT_FEE" || rows[1].Amount != -50 {
		t.Errorf("row 1 = %+v, want PAYMENT_FEE of -50", rows[1])
	}
	if rows[2].Date != "2025-01-11" || rows[2].Type != "PAYOUT" {
		t.Errorf("row 2 = %+v, want 2025-01-11 PAYOUT", rows[2])
	}
}

func TestSwedishAmountUsesDecimalComma(t *testing.T) {
	cases := []struct {
		öre  int
		want string
	}{
		{12345, "123,45"},
		{-50, "-0,50"},
		{0, "0,00"},
	}
	for _, tc := range cases {
		if got := swedishAmount(tc.öre); got != tc.want {
			t.Errorf("swedishAmount(%d) = %q, want %q", tc.öre, got, tc.want)
		}
	}
}

func TestWriteReconciliationTSVComputesRunningBalance(t *testing.T) {
	rows := []ReconciliationRow{
		{Date: "2025-01-10", Type: "PAYMENT", Text: "Zettle köp", Amount: 5000},
		{Date: "2025-01-10", Type: "PAYMENT_FEE", Text: "Zettle avgift", Amount: -50},
	}
	path := filepath.Join(t.TempDir(), "out", "recon.txt")

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)
	if err := WriteReconciliationTSV(rows, start, end, path); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 4 { // comment, header, 2 rows
		t.Fatalf("expected 4 lines, got %d:\n%s", len(lines), data)
	}
	if !strings.HasSuffix(lines[2], "50,00\t50,00") {
		t.Errorf("first row should end with amount and balance 50,00: %q", lines[2])
	}
	if !strings.HasSuffix(lines[3], "-0,50\t49,50") {
		t.Errorf("second row should carry running balance 49,50: %q", lines[3])
	}
}
