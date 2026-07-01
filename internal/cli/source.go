package cli

import (
	"context"
	"fmt"
	"intakt/config"
	"intakt/internal/cache"
	"intakt/internal/voucher"
	"intakt/report"
	"intakt/zettle"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Source is a data source that knows how to fetch, render, and upload its reports.
// Implement this interface to add a new payment source.
type Source interface {
	Fetch(ctx context.Context, start, end time.Time) error
	Compile(assetsDir string) error
}

func newSource(name string, cfg *config.Config, filePath string) (Source, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "zettle":
		return &zettleSource{cfg: cfg}, nil
	case "fees":
		return &feesSource{cfg: cfg}, nil
	case "swish":
		if filePath == "" {
			return nil, fmt.Errorf("swish source requires a CSV file path as the 4th argument")
		}
		return &swishSource{cfg: cfg, csvPath: filePath}, nil
	case "reconcile":
		return &reconcileSource{cfg: cfg}, nil
	default:
		return nil, fmt.Errorf("unknown source %q (available: zettle, fees, swish, reconcile)", name)
	}
}

func cacheRequiredErr(start, end time.Time) error {
	return fmt.Errorf("no local cache found — run first:\n  intakt fetch %s %s",
		start.Format("2006-01-02"), end.Format("2006-01-02"))
}

// -- zettle (purchases) --

type zettleSource struct {
	cfg     *config.Config
	reports map[string]*report.Report
}

func (s *zettleSource) Fetch(_ context.Context, start, end time.Time) error {
	cacheDir, err := cache.Dir()
	if err != nil {
		return err
	}
	path, ok := cache.FindCovering(cacheDir, "purchases", start, end)
	if !ok {
		return cacheRequiredErr(start, end)
	}
	var all []zettle.Purchase
	if err := dimSpinWhile("Reading cache...", func() error {
		var e error
		all, e = cache.ReadFile[zettle.Purchase](path)
		return e
	}); err != nil {
		return err
	}
	return dimSpinWhile("Aggregating purchases...", func() error {
		purchases := cache.FilterByDate(all, start, end, func(p zettle.Purchase) string { return p.Timestamp })
		s.reports = report.ParsePurchases(s.cfg, purchases)
		return nil
	})
}

func (s *zettleSource) Compile(assetsDir string) error {
	bar := newProgressBar(len(s.reports), "Compiling PDFs")
	if err := report.Compile(s.cfg, assetsDir, s.reports, func() { _ = bar.Add(1) }); err != nil {
		_ = bar.Finish()
		return err
	}
	_ = bar.Finish()
	return dimSpinWhile("Saving vouchers...", func() error {
		for key, r := range s.reports {
			req, err := voucher.ReportRequest(s.cfg, r, "Zettle", s.cfg.Fortnox.ZettleAccount)
			if err != nil {
				return fmt.Errorf("%s: building voucher: %w", key, err)
			}
			pv := voucher.PendingVoucher{
				Request:           req,
				PDFPath:           filepath.Join(s.cfg.OutPath.Reports, "purchases", key+".pdf"),
				DefaultCostCenter: s.cfg.Committees[r.UtskottPrefix].DefaultCostCenter,
			}
			if err := voucher.Save(s.cfg.OutPath.Vouchers, key, pv); err != nil {
				return fmt.Errorf("%s: saving voucher data: %w", key, err)
			}
		}
		return nil
	})
}

// -- fees --

type feesSource struct {
	cfg     *config.Config
	reports map[string]*report.FeeReport
}

func (s *feesSource) Fetch(_ context.Context, start, end time.Time) error {
	cacheDir, err := cache.Dir()
	if err != nil {
		return err
	}
	path, ok := cache.FindCovering(cacheDir, "finance", start, end)
	if !ok {
		return cacheRequiredErr(start, end)
	}
	var allTxs []zettle.TransactionFee
	if err := dimSpinWhile("Reading cache...", func() error {
		var e error
		allTxs, e = cache.ReadFile[zettle.TransactionFee](path)
		return e
	}); err != nil {
		return err
	}
	return dimSpinWhile("Aggregating fees...", func() error {
		allTxs = cache.FilterByDate(allTxs, start, end, func(tx zettle.TransactionFee) string { return tx.Timestamp })
		var fees []zettle.TransactionFee
		for _, tx := range allTxs {
			if tx.OriginatorTransactionType == "PAYMENT_FEE" {
				fees = append(fees, tx)
			}
		}
		s.reports = report.ParseFees(s.cfg, fees)
		return nil
	})
}

func (s *feesSource) Compile(assetsDir string) error {
	bar := newProgressBar(len(s.reports), "Compiling PDFs")
	if err := report.CompileFees(s.cfg, assetsDir, s.reports, func() { _ = bar.Add(1) }); err != nil {
		_ = bar.Finish()
		return err
	}
	_ = bar.Finish()
	return dimSpinWhile("Saving vouchers...", func() error {
		pa := s.cfg.FeePrefixAccount()
		defaultCC := s.cfg.Committees[pa.Name].DefaultCostCenter
		for key, r := range s.reports {
			req, err := voucher.FeeReportRequest(s.cfg, r)
			if err != nil {
				return fmt.Errorf("%s: building voucher: %w", key, err)
			}
			pv := voucher.PendingVoucher{
				Request:           req,
				PDFPath:           filepath.Join(s.cfg.OutPath.Reports, "fees", "avgift-"+key+".pdf"),
				DefaultCostCenter: defaultCC,
			}
			if err := voucher.Save(s.cfg.OutPath.Vouchers, "avgift-"+key, pv); err != nil {
				return fmt.Errorf("%s: saving voucher data: %w", key, err)
			}
		}
		return nil
	})
}

// -- reconcile --

type reconcileSource struct {
	cfg   *config.Config
	rows  []report.ReconciliationRow
	start time.Time
	end   time.Time
}

func (s *reconcileSource) Fetch(_ context.Context, start, end time.Time) error {
	s.start = start
	s.end = end
	cacheDir, err := cache.Dir()
	if err != nil {
		return err
	}
	path, ok := cache.FindCovering(cacheDir, "finance", start, end)
	if !ok {
		return cacheRequiredErr(start, end)
	}
	var allTxs []zettle.TransactionFee
	if err := dimSpinWhile("Reading cache...", func() error {
		var e error
		allTxs, e = cache.ReadFile[zettle.TransactionFee](path)
		return e
	}); err != nil {
		return err
	}
	return dimSpinWhile("Aggregating transactions...", func() error {
		allTxs = cache.FilterByDate(allTxs, start, end, func(tx zettle.TransactionFee) string { return tx.Timestamp })
		s.rows = report.ParseReconciliation(allTxs)
		return nil
	})
}

func (s *reconcileSource) Compile(_ string) error {
	outDir := filepath.Join(s.cfg.OutPath.Reports, "reconciliation")
	filename := fmt.Sprintf("zettle-%s--%s.txt", s.start.Format("2006-01-02"), s.end.Format("2006-01-02"))
	path := filepath.Join(outDir, filename)
	return report.WriteReconciliationTSV(s.rows, s.start, s.end, path)
}

// -- swish --

type swishSource struct {
	cfg     *config.Config
	csvPath string
	reports map[string]*report.Report
}

func (s *swishSource) Fetch(_ context.Context, start, end time.Time) error {
	f, err := os.Open(s.csvPath)
	if err != nil {
		return fmt.Errorf("opening swish CSV: %w", err)
	}
	defer f.Close()

	return dimSpinWhile("Aggregating payments...", func() error {
		var e error
		s.reports, e = report.ParseSwish(s.cfg, f, start, end)
		return e
	})
}

func (s *swishSource) Compile(assetsDir string) error {
	bar := newProgressBar(len(s.reports), "Compiling PDFs")
	if err := report.CompileSwish(s.cfg, assetsDir, s.reports, func() { _ = bar.Add(1) }); err != nil {
		_ = bar.Finish()
		return err
	}
	_ = bar.Finish()
	return dimSpinWhile("Saving vouchers...", func() error {
		for key, r := range s.reports {
			req, err := voucher.ReportRequest(s.cfg, r, "Swish", s.cfg.Fortnox.SwishAccount)
			if err != nil {
				return fmt.Errorf("%s: building voucher: %w", key, err)
			}
			pv := voucher.PendingVoucher{
				Request:           req,
				PDFPath:           filepath.Join(s.cfg.OutPath.Reports, "swish", key+".pdf"),
				DefaultCostCenter: s.cfg.Committees[r.UtskottPrefix].DefaultCostCenter,
			}
			if err := voucher.Save(s.cfg.OutPath.Vouchers, key, pv); err != nil {
				return fmt.Errorf("%s: saving voucher data: %w", key, err)
			}
		}
		return nil
	})
}
