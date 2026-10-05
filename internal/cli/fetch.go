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
		return usage(fs, fmt.Errorf("expected <start> <end>, got %d argument(s)", len(pos)))
	}

	start, end, err := parseDateRange(pos[0], pos[1])
	if err != nil {
		return usage(fs, err)
	}
	return runFetch(ctx, start, end)
}

// runFetch downloads purchases and finance transactions concurrently and
// merges them into the local cache, rendering progress as box rows.
func runFetch(ctx context.Context, start, end time.Time) error {
	client, err := zettle.NewClientFromEnv(ctx)
	if err != nil {
		return err
	}

	dir, err := cache.Dir()
	if err != nil {
		return err
	}

	// The API filters on UTC timestamps, but vouchers group by Swedish
	// calendar days, which begin at 22:00/23:00 UTC the evening before.
	// Fetch one extra UTC day so the range's first local day is complete.
	fetchStart := start.AddDate(0, 0, -1)

	var (
		purchases []zettle.Purchase
		txs       []zettle.TransactionFee
	)
	box := startBox("Fetching from Zettle")
	ferr := box.runConcurrent(
		boxTask{"Fetching purchases", func() (string, error) {
			var err error
			purchases, err = client.GetPurchasesRange(ctx, fetchStart, end)
			return fmt.Sprintf("%d", len(purchases)), err
		}},
		boxTask{"Fetching transactions", func() (string, error) {
			var err error
			txs, err = client.GetAllTransactionsRange(ctx, fetchStart, end)
			return fmt.Sprintf("%d", len(txs)), err
		}},
	)
	if ferr == nil {
		ferr = box.run("Saving purchases cache", false, func() (string, error) {
			_, e := cache.MergeAndWrite(dir, cachePurchases, start, end, purchases, func(p zettle.Purchase) string {
				return p.PurchaseUUID
			})
			return "", e
		})
	}
	if ferr == nil {
		ferr = box.run("Saving finance cache", false, func() (string, error) {
			_, e := cache.MergeAndWrite(dir, cacheFinance, start, end, txs, func(t zettle.TransactionFee) string {
				return t.OriginatingTransactionUUID + ":" + t.OriginatorTransactionType
			})
			return "", e
		})
	}
	box.finish()
	if ferr != nil {
		if box.hasFailure() {
			return reported(ferr) // the failed step is already shown in the box
		}
		return ferr
	}
	fmt.Fprintf(os.Stderr, "\n\033[2mfetched %d purchases, %d transactions\033[0m\n\n", len(purchases), len(txs))
	return nil
}
