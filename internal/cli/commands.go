package cli

import (
	"context"
	"flag"
	"fmt"
	"intakt/config"
	"intakt/fortnox"
	"os"
	"time"
)

// configPath is the fixed location intakt reads its bookkeeping config from.
const configPath = "config.yaml"

func LoginCmd(ctx context.Context, argv []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: intakt login\n")
	}
	if err := fs.Parse(argv); err != nil {
		return err
	}
	return doLogin(ctx, fortnox.DefaultTokenPath())
}

func CompileCmd(ctx context.Context, argv []string) error {
	fs := flag.NewFlagSet("compile", flag.ExitOnError)
	assetsDir := fs.String("assets", "assets", "assets directory")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: intakt compile [flags] <source> <start> <end> [<file>]\n\n")
		fmt.Fprintf(os.Stderr, "Sources: zettle, fees, swish, reconcile\nDates:   YYYY-MM-DD\nFile:    path to CSV export (required for swish)\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(argv); err != nil {
		return err
	}

	pos := fs.Args()
	if len(pos) < 3 {
		return usage(fs, fmt.Errorf("expected <source> <start> <end>, got %d argument(s)", len(pos)))
	}

	sourceName := pos[0]

	start, end, err := parseDateRange(pos[1], pos[2])
	if err != nil {
		return usage(fs, err)
	}

	var filePath string
	if len(pos) > 3 {
		filePath = pos[3]
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	// Validate the source before the box so an unknown source/missing file is a
	// plain usage error rather than an empty box.
	src, err := newSource(sourceName, cfg, filePath)
	if err != nil {
		return usage(fs, err)
	}

	if err := compileInBox(ctx, src, sourceName, *assetsDir, start, end); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\n  Run \033[1mintakt upload\033[0m to review and upload vouchers.\n\n")
	return nil
}

// compileInBox drives the fetch/compile pipeline for src inside a live box.
// Errors already rendered on a failed box row come back marked as reported;
// errors that escape before any step (e.g. a cache miss) are left for main.
func compileInBox(ctx context.Context, src Source, sourceName, assetsDir string, start, end time.Time) error {
	box := startBox("Compiling " + sourceName)
	activeBox = box
	err := fetchAndCompile(ctx, src, assetsDir, start, end)
	activeBox = nil
	box.finish()
	if err != nil && box.hasFailure() {
		return reported(err)
	}
	return err
}

func UploadCmd(ctx context.Context, argv []string) error {
	fs := flag.NewFlagSet("upload", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "review vouchers without uploading to Fortnox")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: intakt upload [flags]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(argv); err != nil {
		return err
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	entries, err := scanVouchers(cfg.OutPath.Vouchers)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("no pending vouchers in %s — run intakt compile first", cfg.OutPath.Vouchers)
	}
	return runUpload(ctx, cfg, entries, *dryRun)
}

// runUpload drives the review TUI over the pending vouchers and reports the
// outcome. Failures the summary already displayed come back marked as reported
// so main only sets the exit code.
func runUpload(ctx context.Context, cfg *config.Config, entries []reviewEntry, dryRun bool) error {
	// A dry run never touches Fortnox, so don't require credentials for it.
	var fn *fortnox.Client
	if !dryRun {
		var err error
		fn, err = fortnox.NewClientFromEnv(fortnox.DefaultTokenPath())
		if err != nil {
			return err
		}
	}

	summary, err := runTUI(ctx, fn, cfg, cfg.OutPath.Vouchers, entries, dryRun)
	if err != nil {
		return err
	}
	if !summary.confirmed {
		return nil
	}

	printUploadSummary(summary)
	if len(summary.failures) > 0 {
		return reported(fmt.Errorf("%d of %d voucher(s) failed to upload",
			len(summary.failures), summary.total))
	}
	return nil
}

// fetchAndCompile loads the source's data and compiles its PDFs and vouchers.
func fetchAndCompile(ctx context.Context, src Source, assetsDir string, start, end time.Time) error {
	if err := src.Fetch(ctx, start, end); err != nil {
		return err
	}
	return src.Compile(assetsDir)
}

func parseDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: expected YYYY-MM-DD", s)
	}
	return t, nil
}

// parseDateRange parses the <start> <end> positional arguments shared by fetch
// and compile, rejecting reversed ranges.
func parseDateRange(startArg, endArg string) (start, end time.Time, err error) {
	start, err = parseDate(startArg)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("<start>: %w", err)
	}
	end, err = parseDate(endArg)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("<end>: %w", err)
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("end date %s is before start date %s", endArg, startArg)
	}
	return start, end, nil
}
