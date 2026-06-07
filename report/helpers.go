package report

import (
	"intakt/config"
	"regexp"
	"strings"
)

func accountDescription(cfg *config.Config, account string) string {
	if description, ok := cfg.AccountDescriptions[account]; ok {
		return description
	}
	return "???"
}

// Builds this each time it is called but whatever
func buildRegexFromValidPrefixes(cfg *config.Config) string {
	prefixes := make([]string, len(cfg.PrefixMappings))
	i := 0
	for k := range cfg.PrefixMappings {
		prefixes[i] = k
		i++
	}
	var sb strings.Builder
	sb.WriteString(`^(`)
	sb.WriteString(strings.ToLower(strings.Join(prefixes, "|")))
	sb.WriteString(`|`)
	sb.WriteString(strings.ToUpper(strings.Join(prefixes, "|")))
	sb.WriteString(`)`)
	sb.WriteString(`[-–— ]+`) // Separator
	sb.WriteString(`(.+?)$`)
	return sb.String()
}

func deconstruct(re *regexp.Regexp, productName string) (prefix, name string) {
	m := re.FindStringSubmatch(strings.TrimSpace(productName))
	if m == nil {
		return "", strings.TrimSpace(productName)
	}
	return strings.ToLower(strings.TrimSpace(m[1])), strings.TrimSpace(m[2])
}

func getReport(reports map[string]*Report, pa config.PrefixAccount, date string) *Report {
	key := pa.Name + "-" + date
	if report, ok := reports[key]; ok {
		return report
	}
	report := &Report{
		UtskottPrefix:        pa.Name,
		Date:                 date,
		Products:             []Entry{},
		AccountDistributions: []AccountDistribution{},
	}
	reports[key] = report
	return report
}

func sameProductLine(a, b Entry) bool {
	return a.Name == b.Name &&
		a.Variant == b.Variant &&
		a.Account == b.Account &&
		a.UnitPrice == b.UnitPrice &&
		a.IsDiscount == b.IsDiscount
}

func addDistribution(cfg *config.Config, report *Report, account string, amount int) {
	for i := range report.AccountDistributions {
		if report.AccountDistributions[i].Account == account {
			report.AccountDistributions[i].Total += amount
			return
		}
	}
	report.AccountDistributions = append(report.AccountDistributions, AccountDistribution{
		Account:     account,
		Description: accountDescription(cfg, account),
		Total:       amount,
	})
}

func addEntry(cfg *config.Config, report *Report, entry Entry) {
	for i := range report.Products {
		if sameProductLine(report.Products[i], entry) {
			report.Products[i].Quantity += entry.Quantity
			report.Products[i].EffectiveDiscount += entry.EffectiveDiscount
			report.Products[i].Total += entry.Total
			report.Total += entry.Total
			addDistribution(cfg, report, entry.Account, entry.Total)
			return
		}
	}
	report.Products = append(report.Products, entry)
	report.Total += entry.Total
	addDistribution(cfg, report, entry.Account, entry.Total)
}
