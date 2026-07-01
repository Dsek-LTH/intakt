package report

import (
	"intakt/config"
	"intakt/zettle"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

func parseZettleDate(ts string) string {
	t, ok := zettle.ParseDate(ts)
	if !ok {
		return ts
	}
	return t.Format("2006-01-02")
}

func parseQuantity(quantity string) int {
	q, err := strconv.Atoi(strings.TrimSpace(quantity))
	if err != nil {
		return 0
	}
	return q
}

func productEntry(product zettle.Product, account int, name string) Entry {
	quantity := parseQuantity(product.Quantity)
	total := quantity*product.UnitPrice - product.DiscountValue
	return Entry{
		Name:              name,
		Variant:           product.VariantName,
		Account:           strconv.Itoa(account),
		Quantity:          quantity,
		UnitPrice:         product.UnitPrice,
		EffectiveDiscount: product.DiscountValue,
		Total:             total,
	}
}

// roundDiv rounds numerator / denominator using integer arithmetic.
// It assumes non-negative values, which is the normal case for discounts.
func roundDiv(numerator, denominator int) int {
	if denominator == 0 {
		return 0
	}
	return (numerator + denominator/2) / denominator
}

func purchaseDiscountValue(discount zettle.PurchaseDiscount, purchasePositiveTotal int) int {
	if discount.Value != 0 {
		return discount.Value
	}
	if discount.Amount != nil {
		return *discount.Amount
	}
	if discount.Percentage != nil {
		return roundDiv(int(float64(purchasePositiveTotal)**discount.Percentage), 100)
	}
	return 0
}

func addDiscountToProductLine(cfg *config.Config, report *Report, entry Entry, discountAmount int) {
	if discountAmount == 0 {
		return
	}
	for i := range report.Products {
		if sameProductLine(report.Products[i], entry) {
			report.Products[i].EffectiveDiscount += discountAmount
			report.Products[i].Total -= discountAmount
			report.Total -= discountAmount
			addDistribution(cfg, report, entry.Account, -discountAmount)
			return
		}
	}
}

type saleLine struct {
	report *Report
	entry  Entry
	total  int
}

func allocateByLargestRemainder(amount int, lines []saleLine, positiveTotal int) []int {
	allocations := make([]int, len(lines))
	remainders := make([]struct{ index, remainder int }, 0, len(lines))
	allocatedTotal := 0

	for i, line := range lines {
		if line.total <= 0 {
			continue
		}
		numerator := amount * line.total
		base := numerator / positiveTotal
		remainder := numerator % positiveTotal
		allocations[i] = base
		allocatedTotal += base
		remainders = append(remainders, struct{ index, remainder int }{i, remainder})
	}

	slices.SortStableFunc(remainders, func(a, b struct{ index, remainder int }) int {
		return b.remainder - a.remainder
	})

	remaining := amount - allocatedTotal
	for i := 0; i < remaining; i++ {
		allocations[remainders[i%len(remainders)].index]++
	}

	return allocations
}

func addPurchaseDiscounts(cfg *config.Config, lines []saleLine, discounts []zettle.PurchaseDiscount) {
	if len(lines) == 0 || len(discounts) == 0 {
		return
	}

	positiveTotal := 0
	for _, line := range lines {
		if line.total > 0 {
			positiveTotal += line.total
		}
	}
	if positiveTotal == 0 {
		return
	}

	for _, discount := range discounts {
		discountAmount := purchaseDiscountValue(discount, positiveTotal)
		if discountAmount == 0 {
			continue
		}
		allocations := allocateByLargestRemainder(discountAmount, lines, positiveTotal)
		for i, allocated := range allocations {
			if allocated == 0 {
				continue
			}
			line := lines[i]
			addDiscountToProductLine(cfg, line.report, line.entry, allocated)
		}
	}
}

// ParsePurchases converts a slice of Zettle purchases into a map of Reports
// keyed by "<committee>-<date>".
func ParsePurchases(cfg *config.Config, purchases []zettle.Purchase) map[string]*Report {
	reports := make(map[string]*Report)
	fallback := cfg.PrefixMappings["sk"]
	re := regexp.MustCompile(buildRegexFromValidPrefixes(cfg))

	for _, purchase := range purchases {
		date := parseZettleDate(purchase.Timestamp)
		lines := make([]saleLine, 0, len(purchase.Products))

		for _, product := range purchase.Products {
			prefix, name := deconstruct(re, product.Name)
			pa, ok := cfg.PrefixMappings[prefix]
			if prefix == "" || !ok {
				pa = fallback
				name = product.Name
			}

			report := getReport(reports, pa, date)
			entry := productEntry(product, pa.Account, name)
			addEntry(cfg, report, entry)

			if entry.Total > 0 {
				lines = append(lines, saleLine{
					report: report,
					entry:  entry,
					total:  entry.Total,
				})
			}
		}

		addPurchaseDiscounts(cfg, lines, purchase.Discounts)
	}

	return reports
}
