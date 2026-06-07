package main

import (
	"context"
	"errors"
	"fmt"
	"intakt/config"
	"intakt/report"
	"intakt/zettle"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Source is a data source that knows how to fetch, render, and upload its reports.
// Implement this interface to add a new payment source.
type Source interface {
	Fetch(ctx context.Context, start, end time.Time) error
	Compile(assetsDir string) error
	PendingEntries(outputDir string) []ManifestEntry
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
	default:
		return nil, fmt.Errorf("unknown source %q (available: zettle, fees, swish)", name)
	}
}

// zettleTimestamp formats a time.Time for the Zettle API.
// End dates are shifted forward by one day so the range is inclusive.
func zettleTimestamp(t time.Time) string {
	return t.Format("2006-01-02T15:04:05-07:00")
}

// -- zettle (purchases) --

type zettleSource struct {
	cfg     *config.Config
	reports map[string]*report.Report
}

func (s *zettleSource) Fetch(ctx context.Context, start, end time.Time) error {
	client, err := zettle.NewClientFromEnv(ctx)
	if err != nil {
		return err
	}

	purchases, err := client.GetPurchases(ctx, zettleTimestamp(start), zettleTimestamp(end.AddDate(0, 0, 1)))
	if err != nil {
		return err
	}

	s.reports = report.ParsePurchases(s.cfg, purchases)
	return nil
}

func (s *zettleSource) Compile(assetsDir string) error {
	if err := report.Compile(s.cfg, assetsDir, s.reports); err != nil {
		return err
	}
	var errs []error
	for key, r := range s.reports {
		req, err := reportToVoucherRequest(s.cfg, r, "Zettle", s.cfg.SIE4.ZettleAccount)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: building voucher: %w", key, err))
			continue
		}
		pv := pendingVoucher{
			Request: req,
			PDFPath: filepath.Join(s.cfg.OutPath.Reports, "purchases", key+".pdf"),
		}
		if err := savePendingVoucher(s.cfg.OutPath.JSON, key, pv); err != nil {
			errs = append(errs, fmt.Errorf("%s: saving voucher data: %w", key, err))
		}
	}
	errs = append(errs, report.CompileSIE4(s.cfg, s.reports))
	return errors.Join(errs...)
}

func (s *zettleSource) PendingEntries(outputDir string) []ManifestEntry {
	entries := make([]ManifestEntry, 0, len(s.reports))
	for key, r := range s.reports {
		entries = append(entries, ManifestEntry{
			Key:        key,
			Type:       "purchases",
			Date:       r.Date,
			PDF:        filepath.Join(outputDir, "purchases", key+".pdf"),
			Total:      float64(r.Total) / 100.0,
			CostCenter: s.cfg.Committees[r.UtskottPrefix].DefaultCostCenter,
		})
	}
	slices.SortFunc(entries, func(a, b ManifestEntry) int {
		return strings.Compare(a.Key, b.Key)
	})
	return entries
}

// -- fees --

type feesSource struct {
	cfg     *config.Config
	reports map[string]*report.FeeReport
}

func (s *feesSource) Fetch(ctx context.Context, start, end time.Time) error {
	client, err := zettle.NewClientFromEnv(ctx)
	if err != nil {
		return err
	}

	fees, err := client.GetFees(ctx, zettleTimestamp(start), zettleTimestamp(end.AddDate(0, 0, 1)))
	if err != nil {
		return err
	}

	s.reports = report.ParseFees(s.cfg, fees)
	return nil
}

func (s *feesSource) Compile(assetsDir string) error {
	if err := report.CompileFees(s.cfg, assetsDir, s.reports); err != nil {
		return err
	}
	var errs []error
	for key, r := range s.reports {
		req, err := feeReportToVoucherRequest(s.cfg, r)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: building voucher: %w", key, err))
			continue
		}
		pv := pendingVoucher{
			Request: req,
			PDFPath: filepath.Join(s.cfg.OutPath.Reports, "fees", "avgift-"+key+".pdf"),
		}
		if err := savePendingVoucher(s.cfg.OutPath.JSON, "avgift-"+key, pv); err != nil {
			errs = append(errs, fmt.Errorf("%s: saving voucher data: %w", key, err))
		}
	}
	return errors.Join(errs...)
}

func (s *feesSource) PendingEntries(outputDir string) []ManifestEntry {
	pa := s.cfg.FeePrefixAccount()
	defaultCC := s.cfg.Committees[pa.Name].DefaultCostCenter

	entries := make([]ManifestEntry, 0, len(s.reports))
	for key, r := range s.reports {
		entries = append(entries, ManifestEntry{
			Key:        key,
			Type:       "fees",
			Date:       r.Date,
			PDF:        filepath.Join(outputDir, "fees", "avgift-"+key+".pdf"),
			Total:      float64(r.Total) / 100.0,
			CostCenter: defaultCC,
		})
	}
	slices.SortFunc(entries, func(a, b ManifestEntry) int {
		return strings.Compare(a.Key, b.Key)
	})
	return entries
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

	reports, err := report.ParseSwish(s.cfg, f, start, end)
	if err != nil {
		return err
	}
	s.reports = reports
	return nil
}

func (s *swishSource) Compile(assetsDir string) error {
	if err := report.CompileSwish(s.cfg, assetsDir, s.reports); err != nil {
		return err
	}
	var errs []error
	for key, r := range s.reports {
		req, err := reportToVoucherRequest(s.cfg, r, "Swish", s.cfg.SIE4.SwishAccount)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: building voucher: %w", key, err))
			continue
		}
		pv := pendingVoucher{
			Request: req,
			PDFPath: filepath.Join(s.cfg.OutPath.Reports, "swish", key+".pdf"),
		}
		if err := savePendingVoucher(s.cfg.OutPath.JSON, key, pv); err != nil {
			errs = append(errs, fmt.Errorf("%s: saving voucher data: %w", key, err))
		}
	}
	errs = append(errs, report.CompileSIE4(s.cfg, s.reports))
	return errors.Join(errs...)
}

func (s *swishSource) PendingEntries(outputDir string) []ManifestEntry {
	entries := make([]ManifestEntry, 0, len(s.reports))
	for key, r := range s.reports {
		entries = append(entries, ManifestEntry{
			Key:        key,
			Type:       "swish",
			Date:       r.Date,
			PDF:        filepath.Join(outputDir, "swish", key+".pdf"),
			Total:      float64(r.Total) / 100.0,
			CostCenter: s.cfg.Committees[r.UtskottPrefix].DefaultCostCenter,
		})
	}
	slices.SortFunc(entries, func(a, b ManifestEntry) int {
		return strings.Compare(a.Key, b.Key)
	})
	return entries
}
