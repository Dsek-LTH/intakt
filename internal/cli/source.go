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

// Names of the on-disk cache datasets written by fetch and read by compile.
const (
	cachePurchases = "purchases"
	cacheFinance   = "finance"
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

// readCachedRange loads the cached dataset covering [start, end] and filters it
// to that range, behind a "Reading cache..." spinner row. It fails with a
// fetch hint when no cache file covers the range.
func readCachedRange[T any](dataset string, start, end time.Time, timestamp func(T) string) ([]T, error) {
	cacheDir, err := cache.Dir()
	if err != nil {
		return nil, err
	}
	path, ok := cache.FindCovering(cacheDir, dataset, start, end)
	if !ok {
		return nil, cacheRequiredErr(start, end)
	}
	var all []T
	if err := dimSpinWhile("Reading cache...", func() error {
		var e error
		all, e = cache.ReadFile[T](path)
		return e
	}); err != nil {
		return nil, err
	}
	return cache.FilterByDate(all, start, end, timestamp), nil
}

// compileWithProgress runs compile behind a "Compiling PDFs" progress-bar row,
// advancing it once per rendered report.
func compileWithProgress(total int, compile func(onProgress func()) error) error {
	bar := newProgressBar(total, "Compiling PDFs")
	err := compile(func() { _ = bar.Add(1) })
	_ = bar.Finish()
	return err
}

// saveSalesVouchers persists one pending voucher per sales report, pointing at
// the PDF that the compile step rendered into subdir under out_path.reports.
func saveSalesVouchers(cfg *config.Config, reports map[string]*report.Report, subdir, paymentMethod, clearingAccount string) error {
	for key, r := range reports {
		req, err := voucher.ReportRequest(cfg, r, paymentMethod, clearingAccount)
		if err != nil {
			return fmt.Errorf("%s: building voucher: %w", key, err)
		}
		pv := voucher.PendingVoucher{
			Request:           req,
			PDFPath:           filepath.Join(cfg.OutPath.Reports, subdir, key+".pdf"),
			DefaultCostCenter: cfg.Committees[r.UtskottPrefix].DefaultCostCenter,
		}
		if err := voucher.Save(cfg.OutPath.Vouchers, key, pv); err != nil {
			return fmt.Errorf("%s: saving voucher data: %w", key, err)
		}
	}
	return nil
}

// -- zettle (purchases) --

type zettleSource struct {
	cfg     *config.Config
	reports map[string]*report.Report
}

func (s *zettleSource) Fetch(_ context.Context, start, end time.Time) error {
	purchases, err := readCachedRange(cachePurchases, start, end,
		func(p zettle.Purchase) string { return p.Timestamp })
	if err != nil {
		return err
	}
	return dimSpinWhile("Aggregating purchases...", func() error {
		s.reports = report.ParsePurchases(s.cfg, purchases)
		return nil
	})
}

func (s *zettleSource) Compile(assetsDir string) error {
	err := compileWithProgress(len(s.reports), func(onProgress func()) error {
		return report.Compile(s.cfg, assetsDir, s.reports, onProgress)
	})
	if err != nil {
		return err
	}
	return dimSpinWhile("Saving vouchers...", func() error {
		return saveSalesVouchers(s.cfg, s.reports, report.SubdirPurchases, "Zettle", s.cfg.Fortnox.ZettleAccount)
	})
}

// -- fees --

type feesSource struct {
	cfg     *config.Config
	reports map[string]*report.FeeReport
}

func (s *feesSource) Fetch(_ context.Context, start, end time.Time) error {
	txs, err := readCachedRange(cacheFinance, start, end,
		func(tx zettle.TransactionFee) string { return tx.Timestamp })
	if err != nil {
		return err
	}
	return dimSpinWhile("Aggregating fees...", func() error {
		var fees []zettle.TransactionFee
		for _, tx := range txs {
			if tx.OriginatorTransactionType == zettle.TxTypePaymentFee {
				fees = append(fees, tx)
			}
		}
		s.reports = report.ParseFees(s.cfg, fees)
		return nil
	})
}

func (s *feesSource) Compile(assetsDir string) error {
	err := compileWithProgress(len(s.reports), func(onProgress func()) error {
		return report.CompileFees(s.cfg, assetsDir, s.reports, onProgress)
	})
	if err != nil {
		return err
	}
	return dimSpinWhile("Saving vouchers...", func() error {
		pa := s.cfg.FeePrefixAccount()
		defaultCC := s.cfg.Committees[pa.Name].DefaultCostCenter
		for key, r := range s.reports {
			req, err := voucher.FeeReportRequest(s.cfg, r)
			if err != nil {
				return fmt.Errorf("%s: building voucher: %w", key, err)
			}
			fileKey := report.FeeFilePrefix + key
			pv := voucher.PendingVoucher{
				Request:           req,
				PDFPath:           filepath.Join(s.cfg.OutPath.Reports, report.SubdirFees, fileKey+".pdf"),
				DefaultCostCenter: defaultCC,
			}
			if err := voucher.Save(s.cfg.OutPath.Vouchers, fileKey, pv); err != nil {
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
	txs, err := readCachedRange(cacheFinance, start, end,
		func(tx zettle.TransactionFee) string { return tx.Timestamp })
	if err != nil {
		return err
	}
	return dimSpinWhile("Aggregating transactions...", func() error {
		s.rows = report.ParseReconciliation(txs)
		return nil
	})
}

func (s *reconcileSource) Compile(_ string) error {
	outDir := filepath.Join(s.cfg.OutPath.Reports, report.SubdirReconciliation)
	filename := fmt.Sprintf("zettle-%s--%s.txt", s.start.Format("2006-01-02"), s.end.Format("2006-01-02"))
	return report.WriteReconciliationTSV(s.rows, s.start, s.end, filepath.Join(outDir, filename))
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
	err := compileWithProgress(len(s.reports), func(onProgress func()) error {
		return report.CompileSwish(s.cfg, assetsDir, s.reports, onProgress)
	})
	if err != nil {
		return err
	}
	return dimSpinWhile("Saving vouchers...", func() error {
		return saveSalesVouchers(s.cfg, s.reports, report.SubdirSwish, "Swish", s.cfg.Fortnox.SwishAccount)
	})
}
