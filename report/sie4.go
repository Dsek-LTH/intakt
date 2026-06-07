package report

import (
	"bytes"
	"errors"
	"fmt"
	"intakt/config"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
)

func CompileSIE4(cfg *config.Config, reports map[string]*Report) error {
	if !cfg.SIE4.Enabled {
		return nil
	}

	if err := os.MkdirAll(cfg.OutPath.SIE, 0755); err != nil {
		return fmt.Errorf("creating SIE4 output directory: %w", err)
	}

	keys := make([]string, 0, len(reports))
	for key := range reports {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var errs []error
	for i, key := range keys {
		report := reports[key]

		path := filepath.Join(cfg.OutPath.SIE, key+".si")
		voucherNumber := ""

		if cfg.SIE4.VoucherNumberStart > 0 {
			voucherNumber = fmt.Sprintf("%d", cfg.SIE4.VoucherNumberStart+i)
		}

		if err := writeSIE4File(cfg, report, path, voucherNumber); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
		}
	}
	return errors.Join(errs...)
}

func writeSIE4File(cfg *config.Config, report *Report, path string, voucherNumber string) error {
	data, err := buildSIE4(cfg, report, voucherNumber)
	if err != nil {
		return err
	}

	encoded, err := charmap.CodePage437.NewEncoder().Bytes(data)
	if err != nil {
		return fmt.Errorf("encoding SIE4 as PC8/codepage 437: %w", err)
	}

	if err := os.WriteFile(path, encoded, 0644); err != nil {
		return fmt.Errorf("writing SIE4 file: %w", err)
	}

	return nil
}

func buildSIE4(cfg *config.Config, report *Report, voucherNumber string) ([]byte, error) {
	sieCfg := cfg.SIE4

	reportDate, err := sie4ReportDate(report.Date)
	if err != nil {
		return nil, err
	}

	fiscalYearStart, err := sie4ConfigDate(sieCfg.FiscalYearStart, "sie4.fiscal_year_start")
	if err != nil {
		return nil, err
	}

	fiscalYearEnd, err := sie4ConfigDate(sieCfg.FiscalYearEnd, "sie4.fiscal_year_end")
	if err != nil {
		return nil, err
	}

	committee := cfg.Committees[report.UtskottPrefix]

	voucherText := strings.TrimSpace(sieCfg.VoucherText)
	if voucherText == "" {
		voucherText = "Zettle"
	}

	if committee.ProperName != "" {
		voucherText += " " + committee.ProperName
	}

	var buf bytes.Buffer

	writeSIE4Header(&buf, cfg, fiscalYearStart, fiscalYearEnd)

	accountDescriptions, transactions, err := sie4Transactions(cfg, report, committee.Description)
	if err != nil {
		return nil, err
	}

	writeSIE4Accounts(&buf, accountDescriptions)

	fmt.Fprintf(
		&buf,
		"#VER %s %s %s %s\n",
		sie4Token(sieCfg.VoucherSeries),
		sie4Token(voucherNumber),
		reportDate,
		sie4Token(voucherText),
	)

	buf.WriteString("{\n")

	for _, tx := range transactions {
		fmt.Fprintf(
			&buf,
			"#TRANS %s {} %s %s %s\n",
			tx.Account,
			tx.Amount,
			reportDate,
			sie4Token(tx.Text),
		)
	}

	buf.WriteString("}\n")

	return buf.Bytes(), nil
}

func writeSIE4Header(w *bytes.Buffer, cfg *config.Config, fiscalYearStart, fiscalYearEnd string) {
	sieCfg := cfg.SIE4

	fmt.Fprintf(w, "#FLAGGA 0\n")
	fmt.Fprintf(w, "#PROGRAM %s %s\n", sie4Token(sieCfg.ProgramName), sie4Token(sieCfg.ProgramVersion))
	fmt.Fprintf(w, "#FORMAT PC8\n")
	fmt.Fprintf(w, "#GEN %s %s\n", time.Now().Format("20060102"), sie4Token(cfg.Attestation.Name))
	fmt.Fprintf(w, "#SIETYP 4\n")
	fmt.Fprintf(w, "#ORGNR %s\n", sie4Token(sieCfg.OrganizationID))
	fmt.Fprintf(w, "#FNAMN %s\n", sie4Token(sieCfg.CompanyName))
	fmt.Fprintf(w, "#RAR 0 %s %s\n", fiscalYearStart, fiscalYearEnd)
}

func writeSIE4Accounts(w *bytes.Buffer, accountDescriptions map[string]string) {
	accounts := make([]string, 0, len(accountDescriptions))
	for account := range accountDescriptions {
		accounts = append(accounts, account)
	}
	slices.Sort(accounts)

	for _, account := range accounts {
		fmt.Fprintf(w, "#KONTO %s %s\n", account, sie4Token(accountDescriptions[account]))
	}
}

type sie4Transaction struct {
	Account string
	Amount  string
	Text    string
}

func sie4Transactions(cfg *config.Config, report *Report, fallbackDescription string) (map[string]string, []sie4Transaction, error) {
	sieCfg := cfg.SIE4

	accountDescriptions := make(map[string]string)
	transactions := make([]sie4Transaction, 0, len(report.AccountDistributions)+1)

	totalCredit := 0

	distributions := append([]AccountDistribution(nil), report.AccountDistributions...)
	slices.SortFunc(distributions, func(a, b AccountDistribution) int {
		return strings.Compare(a.Account, b.Account)
	})

	for _, d := range distributions {
		account := strings.TrimSpace(d.Account)
		if account == "" {
			return nil, nil, fmt.Errorf("account distribution contains empty account")
		}

		if d.Total == 0 {
			continue
		}

		description := strings.TrimSpace(d.Description)
		if description == "" {
			description = fallbackDescription
		}

		if configuredDescription := strings.TrimSpace(cfg.AccountDescriptions[account]); configuredDescription != "" {
			description = configuredDescription
		}

		// Försäljning/intäkt bokas normalt i kredit.
		amount := -d.Total
		totalCredit += d.Total

		transactions = append(transactions, sie4Transaction{
			Account: account,
			Amount:  crowns(amount),
			Text:    description,
		})

		accountDescriptions[account] = description
	}

	if totalCredit != report.Total {
		return nil, nil, fmt.Errorf(
			"account distributions do not match report total: distributions=%s kr, report total=%s kr",
			crowns(totalCredit),
			crowns(report.Total),
		)
	}

	clearingDescription := strings.TrimSpace(sieCfg.ZettleAccountName)
	if configuredDescription := strings.TrimSpace(cfg.AccountDescriptions[sieCfg.ZettleAccount]); configuredDescription != "" {
		clearingDescription = configuredDescription
	}

	transactions = append(transactions, sie4Transaction{
		Account: sieCfg.ZettleAccount,
		Amount:  crowns(report.Total),
		Text:    "Zettle",
	})

	accountDescriptions[sieCfg.ZettleAccount] = clearingDescription

	if err := validateSIE4Balanced(transactions); err != nil {
		return nil, nil, err
	}

	return accountDescriptions, transactions, nil
}

func validateSIE4Balanced(transactions []sie4Transaction) error {
	var sum int

	for _, tx := range transactions {
		cents, err := parseCrownsToCents(tx.Amount)
		if err != nil {
			return err
		}

		sum += cents
	}

	if sum != 0 {
		return fmt.Errorf("voucher is not balanced: diff=%s kr", crowns(sum))
	}

	return nil
}

func sie4Token(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")

	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
	)

	return `"` + replacer.Replace(s) + `"`
}

var sie4DatePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

func sie4ReportDate(s string) (string, error) {
	matches := sie4DatePattern.FindAllString(s, -1)
	if len(matches) == 0 {
		return "", fmt.Errorf("report date must contain YYYY-MM-DD, got %q", s)
	}

	date := matches[len(matches)-1]
	return sie4ConfigDate(date, "report.Date")
}

func sie4ConfigDate(s string, field string) (string, error) {
	parsed, err := time.Parse("2006-01-02", s)
	if err != nil {
		return "", fmt.Errorf("%s must be YYYY-MM-DD, got %q: %w", field, s, err)
	}

	return parsed.Format("20060102"), nil
}

func parseCrownsToCents(s string) (int, error) {
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")

	parts := strings.Split(s, ".")
	if len(parts) != 2 || len(parts[1]) != 2 {
		return 0, fmt.Errorf("invalid SIE amount: %q", s)
	}

	whole := 0
	for _, r := range parts[0] {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid SIE amount: %q", s)
		}
		whole = whole*10 + int(r-'0')
	}

	decimals := 0
	for _, r := range parts[1] {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid SIE amount: %q", s)
		}
		decimals = decimals*10 + int(r-'0')
	}

	cents := whole*100 + decimals
	if negative {
		cents = -cents
	}

	return cents, nil
}
