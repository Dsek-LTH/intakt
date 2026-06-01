package main

import (
	"errors"
	"flag"
	"fmt"
	"intakt/config"
	"intakt/report"
	"intakt/zettle"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type args struct {
	Source     string
	StartDate  string
	EndDate    string
	ConfigPath string
	AssetsDir  string
}

func parseArgs() args {
	var a args

	flag.StringVar(&a.Source, "source", "zettle", "data source: zettle")
	flag.StringVar(&a.Source, "s", "zettle", "alias for --source")

	flag.StringVar(&a.AssetsDir, "assets", "assets", "assets directory: assets")
	flag.StringVar(&a.AssetsDir, "a", "assets", "alias for --assets")

	flag.StringVar(&a.StartDate, "start-date", "", "start date, format: YYYY-MM-DD")
	flag.StringVar(&a.StartDate, "sd", "", "alias for --start-date")

	flag.StringVar(&a.EndDate, "end-date", "", "end date, format: YYYY-MM-DD")
	flag.StringVar(&a.EndDate, "ed", "", "alias for --end-date")

	flag.StringVar(&a.ConfigPath, "config", "config/config.yaml", "path to config file")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  %s --start-date YYYY-MM-DD --end-date YYYY-MM-DD [--source zettle] [--assets path/to/assets_dir] [--config path/to/config.yaml]\n\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()

	return a
}

func parseDateArg(name, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("missing required argument: %s", name)
	}

	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s %q: expected YYYY-MM-DD", name, value)
	}

	return t, nil
}

func zettleTimestamp(t time.Time) string {
	return t.Format("2006-01-02T15:04:05-07:00")
}

func run(a args) error {
	cfg, err := config.Load(a.ConfigPath)
	if err != nil {
		return err
	}

	source := strings.ToLower(strings.TrimSpace(a.Source))

	startDate, err := parseDateArg("--start-date", a.StartDate)
	if err != nil {
		return err
	}

	endDate, err := parseDateArg("--end-date", a.EndDate)
	if err != nil {
		return err
	}

	if endDate.Before(startDate) {
		return errors.New("--end-date cannot be before --start-date")
	}

	switch source {
	case "zettle":
		client, err := zettle.NewClientFromEnv()
		if err != nil {
			return err
		}

		// Treat CLI dates as whole days.
		// Example:
		//   --start-date 2026-05-01 --end-date 2026-05-01
		// fetches from 2026-05-01 00:00 through 2026-05-02 00:00.
		start := zettleTimestamp(startDate)
		end := zettleTimestamp(endDate.AddDate(0, 0, 1))

		purchases, err := client.GetPurchases(start, end)
		if err != nil {
			return err
		}

		reports := report.ParsePurchases(cfg, purchases)
		report.Compile(cfg, a.AssetsDir, reports)

		return nil
	case "fees":
		client, err := zettle.NewClientFromEnv()
		if err != nil {
			return err
		}
		start := zettleTimestamp(startDate)
		end := zettleTimestamp(endDate.AddDate(0, 0, 1))

		fees, err := client.GetFees(start, end)
		if err != nil {
			return err
		}

		reports := report.ParseFees(cfg, fees)
		report.CompileFees(cfg, a.AssetsDir, reports)

		return nil
	default:
		return fmt.Errorf("unknown source: %s", a.Source)
	}
}

func main() {
	log.SetFlags(0)
	_ = godotenv.Load()

	if err := run(parseArgs()); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
