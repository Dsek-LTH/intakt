package main

import (
	"context"
	"fmt"
	"intakt/internal/cli"
	"os"

	"github.com/joho/godotenv"
)

const helpText = `intakt — automated bookkeeping for Zettle and Swish → Fortnox

USAGE
  intakt <command> [flags] [arguments]

COMMANDS
  login                                     authenticate with Fortnox (opens a browser)
  fetch    <start> <end>                    download and cache Zettle data for a date range
  compile  <source> <start> <end> [<file>]  build voucher PDFs and pending vouchers
  upload                                    review pending vouchers and upload them to Fortnox
  help                                      show this help

WORKFLOW
  1. intakt login                                 once, to store a Fortnox token
  2. intakt fetch 2025-01-01 2025-01-31           pull a month of Zettle data
  3. intakt compile zettle 2025-01-01 2025-01-31  render PDFs + pending vouchers
  4. intakt upload                                review in the terminal, then push

SOURCES (for compile)
  zettle     card purchases  -> sales vouchers, one per committee and day
  fees       Zettle fees     -> fee vouchers
  swish      a Swish CSV export (pass its path as <file>) -> sales vouchers
  reconcile  a Zettle bank-statement TSV for import into Fortnox (not uploaded)

ARGUMENTS
  <start> <end>  inclusive dates, formatted YYYY-MM-DD
  <file>         path to a Swish CSV export (required only for the swish source)

ENVIRONMENT (loaded from .env or the shell)
  ZETTLE_CLIENT_ID, ZETTLE_API_KEY          Zettle API credentials (used by fetch)
  FORTNOX_CLIENT_ID, FORTNOX_CLIENT_SECRET  Fortnox OAuth credentials (login, upload)

FILES
  config.yaml    accounts, committees and prefix routing (see README.md)
  Fortnox token  <user-config-dir>/intakt/fortnox_token.json
  Zettle cache   <user-cache-dir>/intakt/

Run 'intakt <command> -h' for command-specific flags.
`

func main() {
	_ = godotenv.Load()

	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, helpText)
		os.Exit(1)
	}

	ctx := context.Background()

	var err error
	switch os.Args[1] {
	case "login":
		err = cli.LoginCmd(ctx, os.Args[2:])
	case "fetch":
		err = cli.FetchCmd(ctx, os.Args[2:])
	case "compile":
		err = cli.CompileCmd(ctx, os.Args[2:])
	case "upload":
		err = cli.UploadCmd(ctx, os.Args[2:])
	case "help", "--help", "-h":
		fmt.Fprint(os.Stdout, helpText)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], helpText)
		os.Exit(1)
	}

	if err != nil {
		cli.HandleError(err)
		os.Exit(1)
	}
}
