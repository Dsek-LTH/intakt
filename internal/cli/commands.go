package cli

import (
	"context"
	"flag"
	"fmt"
	"intakt/config"
	"intakt/fortnox"
	"intakt/report"
	"os"
	"time"
)

func LoginCmd(ctx context.Context, argv []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	tokenFile := fs.String("token-file", fortnox.DefaultTokenPath(), "path to token file")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: intakt login [--token-file path]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(argv); err != nil {
		return err
	}
	return doLogin(ctx, *tokenFile)
}

func CompileCmd(ctx context.Context, argv []string) error {
	fs := flag.NewFlagSet("compile", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "path to config file")
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
		fs.Usage()
		return fmt.Errorf("expected <source> <start> <end>, got %d argument(s)\n\nRun 'intakt fetch <start> <end>' first to download data from Zettle.", len(pos))
	}

	sourceName := pos[0]

	start, err := parseDate(pos[1])
	if err != nil {
		return fmt.Errorf("<start>: %w", err)
	}
	end, err := parseDate(pos[2])
	if err != nil {
		return fmt.Errorf("<end>: %w", err)
	}
	if end.Before(start) {
		return fmt.Errorf("end date %s is before start date %s", pos[2], pos[1])
	}

	var filePath string
	if len(pos) > 3 {
		filePath = pos[3]
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	box := startBox("Compiling " + sourceName)
	activeBox = box
	err = fetchAndCompile(ctx, cfg, sourceName, *assetsDir, filePath, start, end)
	activeBox = nil
	box.finish()
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\n  Run \033[1mintakt upload\033[0m to review and upload vouchers.\n\n")
	return nil
}

func UploadCmd(ctx context.Context, argv []string) error {
	fs := flag.NewFlagSet("upload", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "path to config file")
	tokenFile := fs.String("token-file", fortnox.DefaultTokenPath(), "path to token file")
	dryRun := fs.Bool("dry-run", false, "review vouchers without uploading to Fortnox")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: intakt upload [flags]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(argv); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
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

	// A dry run never touches Fortnox, so don't require credentials for it.
	var fn *fortnox.Client
	if !*dryRun {
		fn, err = fortnox.NewClientFromEnv(*tokenFile)
		if err != nil {
			return err
		}
	}

	summary, err := runTUI(ctx, fn, cfg, cfg.OutPath.Vouchers, entries, *dryRun)
	if err != nil {
		return err
	}
	if !summary.confirmed {
		return nil
	}

	printUploadSummary(summary)
	if len(summary.failures) > 0 {
		return fmt.Errorf("%d of %d voucher(s) failed to upload — rerun 'intakt upload' to retry",
			len(summary.failures), summary.total)
	}
	return nil
}

// fetchAndCompile fetches data from the source and compiles PDFs.
func fetchAndCompile(ctx context.Context, cfg *config.Config, sourceName, assetsDir, filePath string, start, end time.Time) error {
	src, err := newSource(sourceName, cfg, filePath)
	if err != nil {
		return err
	}
	if err := src.Fetch(ctx, start, end); err != nil {
		return err
	}
	return src.Compile(assetsDir)
}

// parseDate reads a date a person typed. Through report.ParseDay, so a range
// boundary means Swedish midnight — the same day the CSV rows and the Zettle
// timestamps are grouped into. Two definitions of "a day" in one program is how
// the last day of a Swish range went missing.
func parseDate(s string) (time.Time, error) {
	t, err := report.ParseDay(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: expected YYYY-MM-DD", s)
	}
	return t, nil
}
