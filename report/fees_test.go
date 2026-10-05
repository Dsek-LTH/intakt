package report

import (
	"intakt/zettle"
	"testing"
)

func TestParseFeesAggregatesPerDay(t *testing.T) {
	cfg := testConfig() // no "z" mapping, so fees use the "sk" prefix account
	fees := []zettle.TransactionFee{
		{Timestamp: "2025-01-10T09:00:00.000+0000", Amount: -150},
		{Timestamp: "2025-01-10T15:00:00.000+0000", Amount: -150},
		{Timestamp: "2025-01-11T09:00:00.000+0000", Amount: -200},
		{Timestamp: "2025-01-11T10:00:00.000+0000", Amount: 0}, // ignored
	}

	reports := ParseFees(cfg, fees)

	if len(reports) != 2 {
		t.Fatalf("expected 2 daily reports, got %d", len(reports))
	}
	day1 := reports["2025-01-10"]
	if day1.Total != 300 {
		t.Errorf("2025-01-10 total = %d, want 300 (fees stored positive)", day1.Total)
	}
	if len(day1.Fees) != 1 || day1.Fees[0].Quantity != 2 {
		t.Errorf("identical fee lines should merge: %+v", day1.Fees)
	}
	if day1.Fees[0].Account != "3000" {
		t.Errorf("fee account = %s, want 3000 (sk fallback)", day1.Fees[0].Account)
	}
}

func TestFeeAccountDistributionsSortedByAccount(t *testing.T) {
	cfg := testConfig()
	dists := feeAccountDistributions(cfg, []FeeEntry{
		{Account: "3001", Total: 100},
		{Account: "3000", Total: 200},
		{Account: "3001", Total: 50},
	})

	if len(dists) != 2 {
		t.Fatalf("expected 2 distributions, got %+v", dists)
	}
	if dists[0].Account != "3000" || dists[1].Account != "3001" {
		t.Errorf("distributions not sorted by account: %+v", dists)
	}
	if dists[1].Total != 150 {
		t.Errorf("3001 total = %d, want 150", dists[1].Total)
	}
}
