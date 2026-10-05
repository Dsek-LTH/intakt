package report

import (
	"fmt"
	"intakt/zettle"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type ReconciliationRow struct {
	Date   string
	Type   string
	Text   string
	Amount int // öre, positive = debit to Zettle account, negative = credit from Zettle account
}

// ParseReconciliation aggregates Zettle finance transactions by date and type.
func ParseReconciliation(txs []zettle.TransactionFee) []ReconciliationRow {
	type key struct{ date, txType string }
	totals := make(map[key]int)

	for _, tx := range txs {
		if tx.Amount == 0 {
			continue
		}
		date := parseZettleDate(tx.Timestamp)
		k := key{date, tx.OriginatorTransactionType}
		totals[k] += tx.Amount
	}

	rows := make([]ReconciliationRow, 0, len(totals))
	for k, amount := range totals {
		rows = append(rows, ReconciliationRow{
			Date:   k.date,
			Type:   k.txType,
			Text:   txTypeDescription(k.txType),
			Amount: amount,
		})
	}

	slices.SortFunc(rows, func(a, b ReconciliationRow) int {
		if c := strings.Compare(a.Date, b.Date); c != 0 {
			return c
		}
		return txTypeSortOrder(a.Type) - txTypeSortOrder(b.Type)
	})

	return rows
}

func txTypeDescription(t string) string {
	switch t {
	case zettle.TxTypePayment:
		return "Zettle köp"
	case zettle.TxTypePaymentFee:
		return "Zettle avgift"
	case zettle.TxTypePayout:
		return "Zettle utbetalning"
	default:
		return "Zettle " + strings.ToLower(t)
	}
}

func txTypeSortOrder(t string) int {
	switch t {
	case zettle.TxTypePayment:
		return 1
	case zettle.TxTypePaymentFee:
		return 2
	case zettle.TxTypePayout:
		return 3
	default:
		return 4
	}
}

// WriteReconciliationTSV generates a tab-separated bank statement file
// in the same format as Swedish bank exports, suitable for Fortnox import.
func WriteReconciliationTSV(rows []ReconciliationRow, start, end time.Time, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	var sb strings.Builder

	fmt.Fprintf(&sb, "* Transaktionsrapport Period %s – %s Skapad %s\n",
		start.Format("2006-01-02"),
		end.Format("2006-01-02"),
		time.Now().Format("2006-01-02 15:04"),
	)
	sb.WriteString("Radnr\tClnr\tKontonr\tProdukt\tValuta\tBokfdag\tTransdag\tValutadag\tReferens\tText\tBelopp\tSaldo\n")

	balance := 0
	for i, row := range rows {
		balance += row.Amount
		fmt.Fprintf(&sb, "%d\t\tZettle\tZettle\tSEK\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			i+1,
			row.Date, row.Date, row.Date,
			row.Type,
			row.Text,
			swedishAmount(row.Amount),
			swedishAmount(balance),
		)
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}

func swedishAmount(öre int) string {
	kr := float64(öre) / 100.0
	s := fmt.Sprintf("%.2f", kr)
	return strings.Replace(s, ".", ",", 1)
}
