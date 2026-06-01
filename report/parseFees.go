package report

import (
	"intakt/config"
	"intakt/zettle"
	"strconv"
)

func feeEntry(fee zettle.TransactionFee, account int) FeeEntry {
	amount := int(fee.Amount)

	return FeeEntry{
		Name:      "Zettle fee",
		Account:   strconv.Itoa(account),
		Quantity:  1,
		UnitPrice: amount,
		Total:     amount,
	}
}

func getFeeReport(reports map[string]*FeeReport, date string) *FeeReport {
	if report, ok := reports[date]; ok {
		return report
	}

	report := &FeeReport{
		Date: date,
		Fees: []FeeEntry{},
	}

	reports[date] = report
	return report
}

func sameFeeLine(a, b FeeEntry) bool {
	return a.Name == b.Name &&
		a.Account == b.Account &&
		a.UnitPrice == b.UnitPrice
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
//
// Fees are grouped by date. Within the same date, fees with the same currency,
// amount, account, and name are tallied into one FeeEntry.
func ParseFees(cfg *config.Config, fees []zettle.TransactionFee) map[string]*FeeReport {
	reports := make(map[string]*FeeReport)

	pa, ok := cfg.PrefixMappings["zettle_fee"]
	if !ok {
		pa = cfg.PrefixMappings["sk"]
	}

	for _, fee := range fees {
		if fee.Amount == 0 {
			continue
		}

		date := parseZettleDate(fee.Timestamp)
		report := getFeeReport(reports, date)

		entry := feeEntry(fee, pa.Account)
		addFeeEntry(report, entry)
	}

	return reports
}
