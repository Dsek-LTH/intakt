package main

import (
	"context"
	"flag"
	"fmt"
	"intakt/config"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
)

const helpText = `intakt — automated bookkeeping for Zettle and Swish

Usage:
  intakt compile [flags] <source> <start> <end> [<file>]
  intakt upload  [flags]
  intakt login   [flags]

Sources:  zettle, fees, swish
Dates:    YYYY-MM-DD (inclusive)
File:     path to CSV export (required for swish)
`

func main() {
	log.SetFlags(0)
	_ = godotenv.Load()

	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, helpText)
		os.Exit(1)
	}

	ctx := context.Background()

	var err error
	switch os.Args[1] {
	case "login":
		err = loginCmd(ctx, os.Args[2:])
	case "compile":
		err = compileCmd(ctx, os.Args[2:])
	case "upload":
		err = uploadCmd(ctx, os.Args[2:])
	case "help", "--help", "-h":
		fmt.Fprint(os.Stdout, helpText)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], helpText)
		os.Exit(1)
	}

	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func loginCmd(ctx context.Context, argv []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	tokenFile := fs.String("token-file", defaultTokenPath(), "path to token file")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: intakt login [--token-file path]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(argv); err != nil {
		return err
	}
	return doLogin(ctx, *tokenFile)
}

func compileCmd(ctx context.Context, argv []string) error {
	fs := flag.NewFlagSet("compile", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "path to config file")
	assetsDir := fs.String("assets", "assets", "assets directory")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: intakt compile [flags] <source> <start> <end> [<file>]\n\n")
		fmt.Fprintf(os.Stderr, "Sources: zettle, fees, swish\nDates:   YYYY-MM-DD\nFile:    CSV export path (required for swish)\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(argv); err != nil {
		return err
	}

	pos := fs.Args()
	if len(pos) < 3 {
		fs.Usage()
		return fmt.Errorf("expected <source> <start> <end>, got %d argument(s)", len(pos))
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

	src, err := fetchAndCompile(ctx, cfg, sourceName, *assetsDir, filePath, start, end)
	if err != nil {
		return err
	}

	manifestFile := manifestFilePath()
	if err := writeManifest(manifestFile, src.PendingEntries(cfg.OutPath.Reports)); err != nil {
		log.Printf("warning: could not write manifest: %v", err)
	} else {
		log.Printf("wrote %s", manifestFile)
		log.Printf("fill in cost_center, remove entries to skip, then run:")
		log.Printf("  intakt upload")
	}

	return nil
}

func uploadCmd(ctx context.Context, argv []string) error {
	fs := flag.NewFlagSet("upload", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "path to config file")
	tokenFile := fs.String("token-file", defaultTokenPath(), "path to token file")
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

	manifest, err := readManifest(manifestFilePath())
	if err != nil {
		return err
	}

	fn, err := newFortnoxClient(cfg, *tokenFile)
	if err != nil {
		return err
	}

	return uploadWithManifest(ctx, fn, cfg.OutPath.JSON, manifest)
}

// fetchAndCompile fetches data from the source and compiles PDFs and SIE4 files.
func fetchAndCompile(ctx context.Context, cfg *config.Config, sourceName, assetsDir, filePath string, start, end time.Time) (Source, error) {
	src, err := newSource(sourceName, cfg, filePath)
	if err != nil {
		return nil, err
	}
	if err := src.Fetch(ctx, start, end); err != nil {
		return nil, err
	}
	if err := src.Compile(assetsDir); err != nil {
		return nil, err
	}
	return src, nil
}

func parseDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: expected YYYY-MM-DD", s)
	}
	return t, nil
}
