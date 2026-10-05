# intakt

Automated bookkeeping for a student-union kiosk: it turns **Zettle** card sales,
Zettle fees and **Swish** payments into attested voucher PDFs and posts them to
**Fortnox**.

`intakt` fetches transactions from Zettle, groups them per committee and day,
renders a signed PDF voucher for each group with `pdflatex`, then lets you review
everything in a terminal UI before uploading the vouchers (with their PDF
receipts attached) to Fortnox.

---

## How it works

```
        login                fetch                 compile                 upload
  ┌───────────────┐   ┌────────────────┐   ┌──────────────────┐   ┌──────────────────┐
  │ Fortnox OAuth │   │  Zettle API →  │   │  cache → parse → │   │  review TUI →    │
  │ token on disk │   │  local cache   │   │  PDF + voucher   │   │  Fortnox upload  │
  └───────────────┘   └────────────────┘   └──────────────────┘   └──────────────────┘
```

1. **login** — one-time Fortnox OAuth; the token is stored and auto-refreshed.
2. **fetch** — download purchases and finance transactions from Zettle into a
   local cache (so later steps are offline and repeatable).
3. **compile** — read the cache, group transactions, render a voucher PDF per
   group, and write a *pending voucher* JSON for each.
4. **upload** — review pending vouchers in a terminal UI, assign cost centers,
   then create the vouchers in Fortnox and attach their PDFs.

Vouchers are only created in Fortnox during `upload`, after you confirm. Each
uploaded voucher's JSON is deleted locally on success, so re-running `upload`
only retries what didn't go through.

---

## Prerequisites

- **Go 1.25+**
- **A LaTeX toolchain** providing `pdflatex` on your `PATH` (e.g. TeX Live or
  MacTeX). Required by `compile`.
- **Zettle API credentials** — a client ID and an API key (JWT assertion).
- **A Fortnox OAuth app** — client ID and client secret.

## Install

```sh
git clone <this-repo> intakt
cd intakt
go build -o intakt .
```

This produces a single `intakt` binary. (`go run .` also works during
development.)

---

## Configuration

### Credentials — `.env`

`intakt` loads a `.env` file from the working directory (or you can export these
in your shell):

```sh
ZETTLE_CLIENT_ID=...
ZETTLE_API_KEY=...            # Zettle JWT assertion
FORTNOX_CLIENT_ID=...
FORTNOX_CLIENT_SECRET=...
```

`.env` is gitignored — never commit real credentials.

### Bookkeeping rules — `config.yaml`

Copy the template and edit it for your organisation:

```sh
cp config.example.yaml config.yaml
```

`config.yaml` is gitignored. Its keys:

| Key | Purpose |
| --- | --- |
| `out_path.reports` | directory for generated voucher PDFs |
| `out_path.vouchers` | directory for pending-voucher JSON |
| `attestation.name` / `attestation.stil_id` | who signs the vouchers (printed on the PDF) |
| `account_descriptions` | human-readable names for account numbers, shown on PDFs |
| `prefix_mappings` | maps a product-name prefix to a committee and booking account |
| `committees` | per-committee display name, description and default cost center |
| `swish_amount_fallbacks` | for Swish payments with no label, guess the product by amount (öre) |
| `fortnox.zettle_account` / `fortnox.swish_account` | clearing accounts |
| `fortnox.voucher_series` | Fortnox voucher series to post into (e.g. `IR`) |

**Prefix routing.** Product names are matched against `prefix_mappings`: a
product called `SK - Coffee` routes to the `sk` mapping (committee `skatt`,
account `3000`). Unmatched products fall back to the `sk` mapping.

---

## Usage

### 1. Authenticate with Fortnox

```sh
intakt login
```

Opens a browser for the OAuth consent, then stores a refreshable token at
`<user-config-dir>/intakt/fortnox_token.json`. Run once (re-run if the refresh
token is ever invalidated).

### 2. Fetch Zettle data

```sh
intakt fetch 2025-01-01 2025-01-31
```

Dates are inclusive (`YYYY-MM-DD`). Data is cached under
`<user-cache-dir>/intakt/`; overlapping fetches are merged and de-duplicated, so
you can widen a range without re-downloading everything.

### 3. Compile vouchers

```sh
intakt compile <source> <start> <end> [<file>]
```

| Source | What it produces |
| --- | --- |
| `zettle` | card purchases → one sales voucher per committee and day |
| `fees` | Zettle transaction fees → fee vouchers |
| `swish` | reads a Swish CSV export (ISO-8859-1) — pass its path as `<file>` |
| `reconcile` | a Zettle bank-statement `.txt` (TSV) for **import into Fortnox**; not uploaded by `intakt` |

Examples:

```sh
intakt compile zettle 2025-01-01 2025-01-31
intakt compile fees   2025-01-01 2025-01-31
intakt compile swish  2025-01-01 2025-01-31 ./swish-export.csv
intakt compile reconcile 2025-01-01 2025-01-31
```

PDFs land in `out_path.reports/<source>/` and pending vouchers in
`out_path.vouchers/`.

### 4. Review and upload

```sh
intakt upload            # review, then create vouchers in Fortnox
intakt upload --dry-run  # run the whole review flow without contacting Fortnox
```

`upload` opens a terminal UI. For each pending voucher you confirm the cost
center (pre-filled from the committee's default), then upload. Failed vouchers
stay on disk so you can re-run `upload` to retry only those.

#### Review UI keys

**Review a voucher**

| Key | Action |
| --- | --- |
| type | edit the cost center |
| `tab` / `shift+tab` | next / previous voucher |
| `ctrl+s` | skip this voucher and move to the next |
| `ctrl+d` | jump to the summary |
| `esc` | quit without uploading |

**Summary**

| Key | Action |
| --- | --- |
| `↑`/`↓` or `j`/`k` | move the highlight |
| `enter` | open the highlighted voucher for review |
| `u` | proceed to upload (asks for confirmation) |
| `esc` | back to reviewing |
| `q` | quit |

---

## Command reference

Run `intakt help`, or `intakt <command> -h` for a command's flags.

| Command | Key flags |
| --- | --- |
| `login` | — |
| `fetch` | — |
| `compile` | `--assets` |
| `upload` | `--dry-run` |

`compile` and `upload` read bookkeeping rules from `config.yaml` in the working
directory. The Fortnox token is stored at
`<user-config-dir>/intakt/fortnox_token.json`. Default: `--assets assets`.

---

## Project layout

```
main.go                 CLI entrypoint and command dispatch
internal/
  cli/                  command implementations, sources, and the review TUI
  cache/                on-disk cache of fetched Zettle data (merge/dedup/filter)
  voucher/              voucher construction and cost-center mapping
config/                 config.yaml loading and validation
zettle/                 Zettle API client (purchases, transactions, auth)
fortnox/                Fortnox API client (vouchers, inbox, OAuth token store)
report/                 parsing → reports → LaTeX/PDF and reconciliation TSV
assets/                 LaTeX skeletons (intakt.tex, avgift.tex) and signature art
```

## Development

```sh
go test ./...   # unit tests (parsing, money arithmetic, cache merging, API clients)
go vet ./...
```

The tests need no credentials or network access; API clients are tested
against local `httptest` stubs.

## Notes

- **Offline after fetch.** `compile` only reads the local cache. If it reports a missing cache, run `fetch` for that range first.
- **Rate limits.** The Fortnox client is rate-limited and uploads run with a
  small worker pool, keeping well within Fortnox's request budget.
- **Amounts** are handled in integer öre internally and only formatted to kronor for display and on the PDFs.
