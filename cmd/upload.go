package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"intakt/config"
	"intakt/fortnox"
	"intakt/report"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// costCenterSentinel is written into VoucherRows at compile time for rows that
// need a cost center. It is replaced with the actual cost center at upload time.
const costCenterSentinel = "__CC__"

type pendingVoucher struct {
	Request fortnox.VoucherCreateRequest `json:"request"`
	PDFPath string                       `json:"pdf_path"`
}

func savePendingVoucher(dir, key string, pv pendingVoucher) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating json dir: %w", err)
	}
	data, err := json.MarshalIndent(pv, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, key+".json"), data, 0644)
}

func loadPendingVoucher(dir, key string) (pendingVoucher, error) {
	var pv pendingVoucher
	data, err := os.ReadFile(filepath.Join(dir, key+".json"))
	if err != nil {
		return pv, fmt.Errorf("reading voucher data: %w", err)
	}
	if err := json.Unmarshal(data, &pv); err != nil {
		return pv, fmt.Errorf("parsing voucher data: %w", err)
	}
	return pv, nil
}

func applyCostCenter(req *fortnox.VoucherCreateRequest, cc string) {
	for i := range req.Voucher.VoucherRows {
		if req.Voucher.VoucherRows[i].CostCenter == costCenterSentinel {
			req.Voucher.VoucherRows[i].CostCenter = cc
		}
	}
}

func newFortnoxClient(cfg *config.Config, tokenFile string) (*fortnox.Client, error) {
	if !cfg.SIE4.Enabled {
		return nil, fmt.Errorf("--upload requires sie4 to be configured (sie4.enabled: true)")
	}

	clientID := os.Getenv("FORTNOX_CLIENT_ID")
	clientSecret := os.Getenv("FORTNOX_CLIENT_SECRET")

	if clientID == "" {
		return nil, fmt.Errorf("missing env var FORTNOX_CLIENT_ID")
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("missing env var FORTNOX_CLIENT_SECRET")
	}

	provider := &fortnox.RefreshingTokenProvider{
		Config: &fortnox.OAuthConfig{
			ClientID:     clientID,
			ClientSecret: clientSecret,
		},
		TokenFile: tokenFile,
	}

	return fortnox.NewClient(provider), nil
}

func parseAccount(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid account number %q: must be an integer", s)
	}
	return n, nil
}

func reportToVoucherRequest(cfg *config.Config, r *report.Report, paymentMethod, clearingAccount string) (fortnox.VoucherCreateRequest, error) {
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
			Description:     "IR " + paymentMethod + " - " + cfg.Committees[r.UtskottPrefix].Description,
			TransactionDate: r.Date,
			VoucherRows:     rows,
			VoucherSeries:   cfg.SIE4.VoucherSeries,
		},
	}, nil
}

func feeReportToVoucherRequest(cfg *config.Config, r *report.FeeReport) (fortnox.VoucherCreateRequest, error) {
	pa := cfg.FeePrefixAccount()

	clearing, err := parseAccount(cfg.SIE4.ZettleAccount)
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
			VoucherSeries:   cfg.SIE4.VoucherSeries,
		},
	}, nil
}

// attachPDF reads a PDF from disk, uploads it to the Fortnox inbox, and
// attaches it to the given voucher.
func attachPDF(ctx context.Context, fn *fortnox.Client, pdfPath string, voucher *fortnox.VoucherResult) error {
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		return fmt.Errorf("reading PDF: %w", err)
	}
	log.Printf("  read %s (%d bytes)", pdfPath, len(data))

	fileID, err := fn.UploadToInbox(ctx, filepath.Base(pdfPath), data)
	if err != nil {
		return fmt.Errorf("uploading PDF to inbox: %w", err)
	}
	log.Printf("  uploaded to inbox: file ID %s", fileID)

	log.Printf("  attaching to voucher %s%d (year %d)", voucher.VoucherSeries, voucher.VoucherNumber, voucher.Year)
	conn, err := fn.CreateVoucherFileConnection(ctx, voucher.Year, fortnox.VoucherFileConnectionCreateRequest{
		VoucherFileConnection: fortnox.VoucherFileConnectionCreate{
			FileID:        fileID,
			VoucherSeries: voucher.VoucherSeries,
			VoucherNumber: voucher.VoucherNumber,
		},
	})
	if err != nil {
		return fmt.Errorf("attaching PDF to voucher: %w", err)
	}
	log.Printf("  attached to voucher %s%d (connection file ID: %s)", voucher.VoucherSeries, voucher.VoucherNumber, conn.FileID)

	return nil
}

// jsonKeyFromEntry derives the JSON filename key from a manifest entry's PDF path.
// e.g. "output/reports/avgift-2026-05-22.pdf" → "avgift-2026-05-22"
func jsonKeyFromEntry(entry ManifestEntry) string {
	return strings.TrimSuffix(filepath.Base(entry.PDF), ".pdf")
}

func uploadWithManifest(ctx context.Context, fn *fortnox.Client, jsonDir string, manifest map[string]ManifestEntry) error {
	var errs []error
	for key, entry := range manifest {
		if entry.CostCenter == "" {
			log.Printf("upload %s: warning: no cost center set", key)
		}
		pv, err := loadPendingVoucher(jsonDir, jsonKeyFromEntry(entry))
		if err != nil {
			errs = append(errs, fmt.Errorf("upload %s: %w", key, err))
			continue
		}
		applyCostCenter(&pv.Request, entry.CostCenter)
		voucher, err := fn.CreateVoucher(ctx, pv.Request)
		if err != nil {
			errs = append(errs, fmt.Errorf("upload %s: creating voucher: %w", key, err))
			continue
		}
		if err := attachPDF(ctx, fn, pv.PDFPath, voucher); err != nil {
			errs = append(errs, fmt.Errorf("upload %s: %w", key, err))
		}
	}
	return errors.Join(errs...)
}
