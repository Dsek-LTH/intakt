package cli

import (
	"context"
	"flag"
	"fmt"
	"intakt/internal/cache"
	"intakt/zettle"
	"os"
	"time"
)

func FetchCmd(ctx context.Context, argv []string) error {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: intakt fetch <start> <end>\n\n")
		fmt.Fprintf(os.Stderr, "Dates: YYYY-MM-DD\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(argv); err != nil {
		return err
	}

	pos := fs.Args()
	if len(pos) < 2 {
		fs.Usage()
		return fmt.Errorf("expected <start> <end>, got %d argument(s)", len(pos))
	}

	start, err := parseDate(pos[0])
	if err != nil {
		return fmt.Errorf("<start>: %w", err)
	}
	end, err := parseDate(pos[1])
	if err != nil {
		return fmt.Errorf("<end>: %w", err)
	}
	if end.Before(start) {
		return fmt.Errorf("end date %s is before start date %s", pos[1], pos[0])
	}

	client, err := zettle.NewClientFromEnv(ctx)
	if err != nil {
		return err
	}

	startStr := zettleTimestamp(start)
	endStr := zettleTimestamp(end.AddDate(0, 0, 1))
	dir, err := cache.Dir()
	if err != nil {
		return err
	}

	var (
		purchases []zettle.Purchase
		txs       []zettle.TransactionFee
	)
	box := startBox("Fetching from Zettle")
	ferr := box.runConcurrent(
		boxTask{"Fetching purchases", func() (string, error) {
			var err error
			purchases, err = client.GetPurchases(ctx, startStr, endStr)
			return fmt.Sprintf("%d", len(purchases)), err
		}},
		boxTask{"Fetching transactions", func() (string, error) {
			var err error
			txs, err = client.GetAllTransactions(ctx, startStr, endStr)
			return fmt.Sprintf("%d", len(txs)), err
		}},
	)
	if ferr == nil {
		ferr = box.run("Saving purchases cache", false, func() (string, error) {
			_, e := cache.MergeAndWrite(dir, "purchases", start, end, purchases, func(p zettle.Purchase) string {
				return p.PurchaseUUID
			})
			return "", e
		})
	}
	if ferr == nil {
		ferr = box.run("Saving finance cache", false, func() (string, error) {
			_, e := cache.MergeAndWrite(dir, "finance", start, end, txs, func(t zettle.TransactionFee) string {
				return t.OriginatingTransactionUUID + ":" + t.OriginatorTransactionType
			})
			return "", e
		})
	}
	box.finish()
	if ferr != nil {
		return fmt.Errorf("fetching from Zettle: %w", ferr)
	}
	fmt.Fprintf(os.Stderr, "\n\033[2mfetched %d purchases, %d transactions\033[0m\n\n", len(purchases), len(txs))
	return nil
}

// zettleTimestamp formats a time.Time for the Zettle API.
// End dates are shifted forward by one day so the range is inclusive.
func zettleTimestamp(t time.Time) string {
	return t.Format("2006-01-02T15:04:05-07:00")
}
