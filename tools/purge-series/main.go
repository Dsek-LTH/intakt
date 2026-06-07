package main

import (
	"bufio"
	"context"
	"fmt"
	"intakt/fortnox"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: purge-series <series> [--token-file path] [--debug]")
		os.Exit(1)
	}

	series := strings.ToUpper(strings.TrimSpace(os.Args[1]))
	tokenFile := defaultTokenPath()
	debug := false
	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--token-file":
			if i+1 < len(os.Args) {
				tokenFile = os.Args[i+1]
				i++
			}
		case "--debug":
			debug = true
		}
	}

	ctx := context.Background()

	clientID := os.Getenv("FORTNOX_CLIENT_ID")
	clientSecret := os.Getenv("FORTNOX_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		fmt.Fprintln(os.Stderr, "error: FORTNOX_CLIENT_ID and FORTNOX_CLIENT_SECRET must be set")
		os.Exit(1)
	}

	provider := &fortnox.RefreshingTokenProvider{
		Config: &fortnox.OAuthConfig{
			ClientID:     clientID,
			ClientSecret: clientSecret,
		},
		TokenFile: tokenFile,
	}
	client := fortnox.NewClient(provider)

	if debug {
		today := time.Now().Format("2006-01-02")
		fyID, err := client.FinancialYearForDate(ctx, today)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error finding financial year: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Financial year ID for %s: %d\n\n", today, fyID)
		raw, err := client.GetRaw(ctx, fmt.Sprintf("/voucherseries?financialyear=%d", fyID))
		if err != nil {
			fmt.Fprintf(os.Stderr, "debug failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("--- voucher series for current year ---")
		fmt.Println(string(raw))
		return
	}

	today := time.Now().Format("2006-01-02")
	fmt.Printf("Looking up financial year for %s...\n", today)
	fyID, err := client.FinancialYearForDate(ctx, today)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error finding financial year: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Using financial year ID %d\n", fyID)

	fmt.Printf("Listing vouchers in series %s...\n", series)
	vouchers, err := client.ListVouchersBySeries(ctx, series, fyID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error listing vouchers: %v\n", err)
		os.Exit(1)
	}

	if len(vouchers) == 0 {
		fmt.Printf("No vouchers found in series %s for financial year %d.\n", series, fyID)
		fmt.Println("Tip: run with --debug to list all voucher series.")
		return
	}

	fmt.Printf("Found %d voucher(s) in series %s:\n\n", len(vouchers), series)
	for _, v := range vouchers {
		fmt.Printf("  %s%d  %s  %s\n", v.VoucherSeries, v.VoucherNumber, v.TransactionDate, v.Description)
	}

	fmt.Printf("\nThis will permanently delete all %d vouchers above.\n", len(vouchers))
	fmt.Print("Type YES to confirm: ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	if strings.TrimSpace(scanner.Text()) != "YES" {
		fmt.Println("Aborted.")
		return
	}

	for i, j := 0, len(vouchers)-1; i < j; i, j = i+1, j-1 {
		vouchers[i], vouchers[j] = vouchers[j], vouchers[i]
	}

	failed := 0
	for i, v := range vouchers {
		fmt.Printf("[%d/%d] Deleting %s%d... ", i+1, len(vouchers), v.VoucherSeries, v.VoucherNumber)
		if err := client.DeleteVoucher(ctx, v.VoucherSeries, v.VoucherNumber, fyID); err != nil {
			fmt.Printf("ERROR: %v\n", err)
			failed++
		} else {
			fmt.Println("ok")
		}
	}

	if failed > 0 {
		fmt.Fprintf(os.Stderr, "\n%d deletion(s) failed.\n", failed)
		os.Exit(1)
	}
	fmt.Printf("\nDeleted %d voucher(s).\n", len(vouchers))
}

func defaultTokenPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ".fortnox_token.json"
	}
	return filepath.Join(dir, "intakt", "fortnox_token.json")
}
