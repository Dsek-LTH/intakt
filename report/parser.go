package report

import (
	"intakt/config"
	"intakt/zettle"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var productNameRegexp = regexp.MustCompile(`^\s*([A-Za-z0-9]+)\s*[-–—]\s*(.+?)\s*$`)

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
	sb.WriteString(`^(`)                                         // Assert position at start
	sb.WriteString(strings.ToLower(strings.Join(prefixes, "|"))) // valid prefixes in lowercase
	sb.WriteString(`|`)
	sb.WriteString(strings.ToUpper(strings.Join(prefixes, "|"))) // valid prefixes in uppercase
	sb.WriteString(`)`)
	sb.WriteString(`[-–— ]+`) // Seperator
	sb.WriteString(`(.+?)$`)  // Product name

	return sb.String()
}

func deconstruct(cfg *config.Config, productName string) (prefix, name string) {
	regex := buildRegexFromValidPrefixes(cfg)

	r, _ := regexp.Compile(regex)
	m := r.FindStringSubmatch(strings.TrimSpace(productName))
	if m == nil {
		return "", strings.TrimSpace(productName)
	}

	return strings.ToLower(strings.TrimSpace(m[1])), strings.TrimSpace(m[2])
}

func parseZettleDate(timestamp string) string {
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05-0700",
		"2006-01-02",
	}

	for _, layout := range layouts {
		t, err := time.Parse(layout, timestamp)
		if err == nil {
			return t.Format("2006-01-02")
		}
	}

	return timestamp
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
		return roundDiv(purchasePositiveTotal*(*discount.Percentage), 100)
	}

	return 0
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

	sort.SliceStable(remainders, func(i, j int) bool {
		return remainders[i].remainder > remainders[j].remainder
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

	for _, purchase := range purchases {
		date := parseZettleDate(purchase.Timestamp)
		lines := make([]saleLine, 0, len(purchase.Products))

		for _, product := range purchase.Products {
			prefix, name := deconstruct(cfg, product.Name)
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
