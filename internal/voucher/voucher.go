package voucher

import (
	"encoding/json"
	"fmt"
	"intakt/config"
	"intakt/fortnox"
	"intakt/report"
	"os"
	"path/filepath"
	"strconv"
)

// costCenterSentinel is written into VoucherRows at compile time for rows that
// need a cost center. It is replaced with the actual cost center at upload time.
const costCenterSentinel = "__CC__"

// PendingVoucher is a voucher request awaiting review and upload, persisted to
// disk between the compile and upload steps.
type PendingVoucher struct {
	Request           fortnox.VoucherCreateRequest `json:"request"`
	PDFPath           string                       `json:"pdf_path"`
	DefaultCostCenter string                       `json:"default_cost_center,omitempty"`
}

func Save(dir, key string, pv PendingVoucher) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating vouchers dir: %w", err)
	}
	data, err := json.MarshalIndent(pv, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, key+".json"), data, 0644)
}

func Load(dir, key string) (PendingVoucher, error) {
	var pv PendingVoucher
	data, err := os.ReadFile(filepath.Join(dir, key+".json"))
	if err != nil {
		return pv, fmt.Errorf("reading voucher data: %w", err)
	}
	if err := json.Unmarshal(data, &pv); err != nil {
		return pv, fmt.Errorf("parsing voucher data: %w", err)
	}
	return pv, nil
}

// ApplyCostCenter replaces the cost-center sentinel on every row with cc.
func ApplyCostCenter(req *fortnox.VoucherCreateRequest, cc string) {
	for i := range req.Voucher.VoucherRows {
		if req.Voucher.VoucherRows[i].CostCenter == costCenterSentinel {
			req.Voucher.VoucherRows[i].CostCenter = cc
		}
	}
}

func parseAccount(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid account number %q: must be an integer", s)
	}
	return n, nil
}

// ReportRequest builds a Fortnox voucher request from a sales report. Rows that
// need a cost center carry the sentinel until ApplyCostCenter fills it in.
func ReportRequest(cfg *config.Config, r *report.Report, paymentMethod, clearingAccount string) (fortnox.VoucherCreateRequest, error) {
	clearing, err := parseAccount(clearingAccount)
	if err != nil {
		return fortnox.VoucherCreateRequest{}, fmt.Errorf("clearing account: %w", err)
	}

	rows := make([]fortnox.VoucherRow, 0, len(r.AccountDistributions)+1)
	for _, d := range r.AccountDistributions {
		if d.Total == 0 {
			continue
		}
		account, err := parseAccount(d.Account)
		if err != nil {
			return fortnox.VoucherCreateRequest{}, fmt.Errorf("account distribution %q: %w", d.Account, err)
		}
		rows = append(rows, fortnox.VoucherRow{
			Account:    account,
			Credit:     float64(d.Total) / 100.0,
			CostCenter: costCenterSentinel,
		})
	}

	rows = append(rows, fortnox.VoucherRow{
		Account: clearing,
		Debit:   float64(r.Total) / 100.0,
	})

	return fortnox.VoucherCreateRequest{
		Voucher: fortnox.VoucherCreate{
			// No series prefix: this goes into VoucherSeries below, which
			// already says what it is, and the description has only ~30
			// characters for the committee's name to fit in. utlagg writes the
			// same text from incomeVoucherDescription and folds a leading "IR"
			// away when it compares, so vouchers uploaded either way and in
			// either wording still match its duplicate check.
			Description:     paymentMethod + " - " + cfg.Committees[r.UtskottPrefix].Description,
			TransactionDate: r.Date,
			VoucherRows:     rows,
			VoucherSeries:   cfg.Fortnox.VoucherSeries,
		},
	}, nil
}

// FeeReportRequest builds a Fortnox voucher request for a Zettle fee report.
func FeeReportRequest(cfg *config.Config, r *report.FeeReport) (fortnox.VoucherCreateRequest, error) {
	pa := cfg.FeePrefixAccount()

	clearing, err := parseAccount(cfg.Fortnox.ZettleAccount)
	if err != nil {
		return fortnox.VoucherCreateRequest{}, fmt.Errorf("clearing account: %w", err)
	}

	rows := []fortnox.VoucherRow{
		{
			Account:    pa.Account,
			Debit:      float64(r.Total) / 100.0,
			CostCenter: costCenterSentinel,
		},
		{
			Account: clearing,
			Credit:  float64(r.Total) / 100.0,
		},
	}

	return fortnox.VoucherCreateRequest{
		Voucher: fortnox.VoucherCreate{
			Description:     "Zettleavgifter",
			TransactionDate: r.Date,
			VoucherRows:     rows,
			VoucherSeries:   cfg.Fortnox.VoucherSeries,
		},
	}, nil
}
