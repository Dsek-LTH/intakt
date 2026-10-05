package report

import (
	"intakt/config"
	"intakt/zettle"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// -- Parsing --

func feeEntry(fee zettle.TransactionFee, account int) FeeEntry {
	amount := -int(fee.Amount) // Zettle reports fees as negative; store as positive cost
	return FeeEntry{
		Name:      "Zettle fee",
		Account:   strconv.Itoa(account),
		Quantity:  1,
		UnitPrice: amount,
		Total:     amount,
	}
}

func getFeeReport(reports map[string]*FeeReport, date string) *FeeReport {
	if r, ok := reports[date]; ok {
		return r
	}
	r := &FeeReport{Date: date, Fees: []FeeEntry{}}
	reports[date] = r
	return r
}

func sameFeeLine(a, b FeeEntry) bool {
	return a.Name == b.Name && a.Account == b.Account && a.UnitPrice == b.UnitPrice
}

func addFeeEntry(report *FeeReport, entry FeeEntry) {
	for i := range report.Fees {
		if sameFeeLine(report.Fees[i], entry) {
			report.Fees[i].Quantity += entry.Quantity
			report.Fees[i].Total += entry.Total
			report.Total += entry.Total
			return
		}
	}
	report.Fees = append(report.Fees, entry)
	report.Total += entry.Total
}

// ParseFees converts Zettle fee transactions into FeeReports keyed by date.
func ParseFees(cfg *config.Config, fees []zettle.TransactionFee) map[string]*FeeReport {
	reports := make(map[string]*FeeReport)

	pa := cfg.FeePrefixAccount()

	for _, fee := range fees {
		if fee.Amount == 0 {
			continue
		}
		date := parseZettleDate(fee.Timestamp)
		addFeeEntry(getFeeReport(reports, date), feeEntry(fee, pa.Account))
	}

	return reports
}

// -- PDF compilation --

func feeAccountDistributions(cfg *config.Config, fees []FeeEntry) []AccountDistribution {
	byAccount := make(map[string]int)
	for _, fee := range fees {
		byAccount[fee.Account] += fee.Total
	}

	distributions := make([]AccountDistribution, 0, len(byAccount))
	for account, total := range byAccount {
		distributions = append(distributions, AccountDistribution{
			Account:     account,
			Description: accountDescription(cfg, account),
			Total:       total,
		})
	}
	slices.SortFunc(distributions, func(a, b AccountDistribution) int {
		return strings.Compare(a.Account, b.Account)
	})
	return distributions
}

func feeToTexData(cfg *config.Config, key string, r *FeeReport) texData {
	pa := cfg.FeePrefixAccount()
	committee := cfg.Committees[pa.Name]

	return texData{
		Skeleton:     "avgift.tex",
		OutputBase:   FeeFilePrefix + key,
		Created:      time.Now().Format("2006-01-02"),
		CostCenter:   latexEscape(committee.ProperName),
		Type:         "Zettle",
		Date:         latexEscape(r.Date),
		Total:        latexEscape(crowns(r.Total) + " kr"),
		Name:         latexEscape(cfg.Attestation.Name),
		StilID:       latexEscape(cfg.Attestation.StilID),
		Description:  latexEscape(committee.Description),
		Distribution: strings.Join(distributionRowsToTex(feeAccountDistributions(cfg, r.Fees)), "\\\\"),
	}
}

// CompileFees renders every fee report to a PDF via pdflatex.
// onProgress is called after each PDF is compiled; pass nil to skip.
func CompileFees(cfg *config.Config, assetsDir string, reports map[string]*FeeReport, onProgress func()) error {
	outDir := filepath.Join(cfg.OutPath.Reports, SubdirFees)
	return compileAll(reports, onProgress, func(key string, r *FeeReport) error {
		return compileTeX(assetsDir, outDir, feeToTexData(cfg, key, r))
	})
}
