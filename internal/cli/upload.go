package cli

import (
	"context"
	"fmt"
	"intakt/fortnox"
	"intakt/internal/voucher"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// attachPDF reads a PDF from disk, uploads it to the Fortnox inbox, and
// attaches it to the given voucher.
func attachPDF(ctx context.Context, fn *fortnox.Client, pdfPath string, voucher *fortnox.VoucherResult) error {
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		return fmt.Errorf("reading PDF: %w", err)
	}

	fileID, err := fn.UploadToInbox(ctx, filepath.Base(pdfPath), data)
	if err != nil {
		return fmt.Errorf("uploading PDF to inbox: %w", err)
	}

	_, err = fn.CreateVoucherFileConnection(ctx, voucher.Year, fortnox.VoucherFileConnectionCreateRequest{
		VoucherFileConnection: fortnox.VoucherFileConnectionCreate{
			FileID:        fileID,
			VoucherSeries: voucher.VoucherSeries,
			VoucherNumber: voucher.VoucherNumber,
		},
	})
	if err != nil {
		return fmt.Errorf("attaching PDF to voucher: %w", err)
	}

	return nil
}

// scanVouchers reads all *.json files in dir and returns them as reviewEntries,
// sorted by transaction date then key.
func scanVouchers(dir string) ([]reviewEntry, error) {
	dirEntries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading vouchers directory: %w", err)
	}

	var result []reviewEntry
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		key := strings.TrimSuffix(de.Name(), ".json")
		pv, err := voucher.Load(dir, key)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		result = append(result, reviewEntry{key: key, pv: pv, costCenter: pv.DefaultCostCenter})
	}

	slices.SortFunc(result, func(a, b reviewEntry) int {
		if c := strings.Compare(
			a.pv.Request.Voucher.TransactionDate,
			b.pv.Request.Voucher.TransactionDate,
		); c != 0 {
			return c
		}
		return strings.Compare(a.key, b.key)
	})

	return result, nil
}

// uploadResult reports the outcome of uploading a single voucher.
type uploadResult struct {
	key string
	err error
}

// uploadSummary aggregates the outcome of an upload run for reporting to the user.
type uploadSummary struct {
	dryRun    bool
	confirmed bool           // user chose to proceed past the review
	total     int            // number of vouchers that were (or would be) uploaded
	uploaded  int            // successfully uploaded
	failures  []uploadResult // per-voucher failures, left on disk for retry
}

// uploadOne creates the voucher, attaches its PDF, and removes the local JSON
// on success so it will not reappear on the next run.
func uploadOne(ctx context.Context, fn *fortnox.Client, vouchersDir string, e reviewEntry) error {
	pv := e.pv
	voucher.ApplyCostCenter(&pv.Request, e.costCenter)

	voucher, err := fn.CreateVoucher(ctx, pv.Request)
	if err != nil {
		return fmt.Errorf("creating voucher: %w", err)
	}
	if pv.PDFPath != "" {
		if err := attachPDF(ctx, fn, pv.PDFPath, voucher); err != nil {
			return err
		}
	}

	_ = os.Remove(filepath.Join(vouchersDir, e.key+".json"))
	return nil
}

// startUpload uploads non-skipped entries with a bounded worker pool, streaming
// one result per entry onto the returned channel and closing it when done.
func startUpload(ctx context.Context, fn *fortnox.Client, vouchersDir string, entries []reviewEntry) <-chan uploadResult {
	jobs := make(chan reviewEntry, len(entries))
	for _, e := range entries {
		jobs <- e
	}
	close(jobs)

	// 4 workers: each voucher uses up to 3 sequential Fortnox requests,
	// keeping peak load well within the 300/min rate limit.
	results := make(chan uploadResult, len(entries))
	var wg sync.WaitGroup
	for range min(4, len(entries)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range jobs {
				results <- uploadResult{key: e.key, err: uploadOne(ctx, fn, vouchersDir, e)}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	return results
}

// printUploadSummary writes a persistent plain-text summary to stdout. The TUI
// shows the same information, but the alt-screen is torn down on exit, so this
// keeps a record in the terminal scrollback.
func printUploadSummary(s uploadSummary) {
	if s.dryRun {
		fmt.Printf("\nDry run — no vouchers were sent. %d voucher(s) would be uploaded.\n\n", s.total)
		return
	}
	fmt.Printf("\n%d of %d voucher(s) uploaded.\n", s.uploaded, s.total)
	if len(s.failures) > 0 {
		fmt.Printf("%d failed (left on disk for retry):\n", len(s.failures))
		for _, r := range s.failures {
			fmt.Printf("  %s: %v\n", r.key, r.err)
		}
	}
	fmt.Println()
}
