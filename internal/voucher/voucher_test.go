package voucher

import (
	"intakt/config"
	"intakt/fortnox"
	"intakt/report"
	"testing"
)

func testConfig() *config.Config {
	return &config.Config{
		PrefixMappings: map[string]config.PrefixAccount{
			"sk": {Name: "skatt", Account: 3000},
		},
		Committees: map[string]config.CommitteeInfo{
			"skatt": {ProperName: "Skattmästeriet", Description: "Kiosken", DefaultCostCenter: "SK1"},
		},
		Fortnox: config.FortnoxConfig{
			ZettleAccount: "1580",
			SwishAccount:  "1930",
			VoucherSeries: "IR",
		},
	}
}

func TestReportRequestBuildsBalancedVoucher(t *testing.T) {
	cfg := testConfig()
	r := &report.Report{
		UtskottPrefix: "skatt",
		Date:          "2025-01-15",
		Total:         12345,
		AccountDistributions: []report.AccountDistribution{
			{Account: "3000", Total: 12000},
			{Account: "3001", Total: 345},
			{Account: "3002", Total: 0}, // zero rows are dropped
		},
	}

	req, err := ReportRequest(cfg, r, "Zettle", cfg.Fortnox.ZettleAccount)
	if err != nil {
		t.Fatal(err)
	}

	rows := req.Voucher.VoucherRows
	if len(rows) != 3 {
		t.Fatalf("rows = %+v, want 2 credit rows + 1 debit row", rows)
	}

	var credit, debit float64
	for _, row := range rows {
		credit += row.Credit
		debit += row.Debit
	}
	if credit != debit || debit != 123.45 {
		t.Errorf("voucher unbalanced: credit %.2f, debit %.2f, want both 123.45", credit, debit)
	}

	// Sales rows carry the sentinel; the clearing row does not.
	if rows[0].CostCenter != costCenterSentinel {
		t.Errorf("sales row cost center = %q, want sentinel", rows[0].CostCenter)
	}
	last := rows[len(rows)-1]
	if last.Account != 1580 || last.CostCenter != "" {
		t.Errorf("clearing row = %+v, want account 1580 with no cost center", last)
	}

	if req.Voucher.VoucherSeries != "IR" {
		t.Errorf("series = %q, want IR", req.Voucher.VoucherSeries)
	}
}

func TestReportRequestRejectsNonNumericAccount(t *testing.T) {
	cfg := testConfig()
	r := &report.Report{
		UtskottPrefix:        "skatt",
		Date:                 "2025-01-15",
		Total:                100,
		AccountDistributions: []report.AccountDistribution{{Account: "not-a-number", Total: 100}},
	}
	if _, err := ReportRequest(cfg, r, "Zettle", "1580"); err == nil {
		t.Error("expected an error for a non-numeric account")
	}
	if _, err := ReportRequest(cfg, r, "Zettle", "bad"); err == nil {
		t.Error("expected an error for a non-numeric clearing account")
	}
}

func TestFeeReportRequestDebitsFeesAndCreditsClearing(t *testing.T) {
	cfg := testConfig()
	req, err := FeeReportRequest(cfg, &report.FeeReport{Date: "2025-01-15", Total: 550})
	if err != nil {
		t.Fatal(err)
	}
	rows := req.Voucher.VoucherRows
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want fee debit + clearing credit", rows)
	}
	if rows[0].Debit != 5.50 || rows[0].CostCenter != costCenterSentinel {
		t.Errorf("fee row = %+v, want debit 5.50 with sentinel", rows[0])
	}
	if rows[1].Credit != 5.50 || rows[1].Account != 1580 {
		t.Errorf("clearing row = %+v, want credit 5.50 on 1580", rows[1])
	}
}

func TestApplyCostCenterReplacesOnlySentinelRows(t *testing.T) {
	req := fortnox.VoucherCreateRequest{
		Voucher: fortnox.VoucherCreate{
			VoucherRows: []fortnox.VoucherRow{
				{Account: 3000, CostCenter: costCenterSentinel},
				{Account: 1580, CostCenter: ""},
				{Account: 3001, CostCenter: "KEEP"},
			},
		},
	}
	ApplyCostCenter(&req, "SK1")

	rows := req.Voucher.VoucherRows
	if rows[0].CostCenter != "SK1" {
		t.Errorf("sentinel row not replaced: %+v", rows[0])
	}
	if rows[1].CostCenter != "" || rows[2].CostCenter != "KEEP" {
		t.Errorf("non-sentinel rows must be untouched: %+v", rows)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	pv := PendingVoucher{
		PDFPath:           "/tmp/x.pdf",
		DefaultCostCenter: "SK1",
	}
	pv.Request.Voucher.Description = "IR Zettle - Kiosken"

	if err := Save(dir, "skatt-2025-01-15", pv); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir, "skatt-2025-01-15")
	if err != nil {
		t.Fatal(err)
	}
	if got.PDFPath != pv.PDFPath || got.DefaultCostCenter != pv.DefaultCostCenter ||
		got.Request.Voucher.Description != pv.Request.Voucher.Description {
		t.Errorf("round trip mismatch: %+v", got)
	}
}
