package report

import (
	"intakt/config"
	"intakt/zettle"
	"testing"
)

// testConfig returns a minimal config with two prefix mappings ("sk" is also
// the hard fallback for unmatched products) and matching account descriptions.
func testConfig() *config.Config {
	return &config.Config{
		AccountDescriptions: map[string]string{
			"3000": "Sales",
			"3001": "Cafe sales",
		},
		PrefixMappings: map[string]config.PrefixAccount{
			"sk": {Name: "skatt", Account: 3000},
			"ca": {Name: "cafe", Account: 3001},
		},
		Committees: map[string]config.CommitteeInfo{
			"skatt": {ProperName: "Skattmästeriet", Description: "Kiosk", DefaultCostCenter: "SK1"},
			"cafe":  {ProperName: "Cafét", Description: "Cafe", DefaultCostCenter: "CA1"},
		},
		SwishAmountFallbacks: map[int]string{2500: "CA - Kaffe"},
	}
}

func purchase(ts string, products ...zettle.Product) zettle.Purchase {
	return zettle.Purchase{Timestamp: ts, Products: products}
}

func TestParsePurchasesRoutesByPrefix(t *testing.T) {
	cfg := testConfig()
	purchases := []zettle.Purchase{
		purchase("2025-01-15T10:00:00.000+0000",
			zettle.Product{Name: "CA - Kaffe", Quantity: "2", UnitPrice: 1500},
			zettle.Product{Name: "SK - Godis", Quantity: "1", UnitPrice: 500},
		),
	}

	reports := ParsePurchases(cfg, purchases)

	if len(reports) != 2 {
		t.Fatalf("expected 2 reports (one per committee), got %d: %v", len(reports), reports)
	}
	cafe, ok := reports["cafe-2025-01-15"]
	if !ok {
		t.Fatal("missing report cafe-2025-01-15")
	}
	if cafe.Total != 3000 {
		t.Errorf("cafe total = %d, want 3000", cafe.Total)
	}
	if len(cafe.Products) != 1 || cafe.Products[0].Name != "Kaffe" {
		t.Errorf("cafe products = %+v, want one entry named Kaffe (prefix stripped)", cafe.Products)
	}
	if sk := reports["skatt-2025-01-15"]; sk.Total != 500 {
		t.Errorf("skatt total = %d, want 500", sk.Total)
	}
}

func TestParsePurchasesUnknownPrefixFallsBackToSK(t *testing.T) {
	cfg := testConfig()
	reports := ParsePurchases(cfg, []zettle.Purchase{
		purchase("2025-01-15T10:00:00.000+0000",
			zettle.Product{Name: "Mystery item", Quantity: "1", UnitPrice: 700},
		),
	})

	sk, ok := reports["skatt-2025-01-15"]
	if !ok {
		t.Fatalf("expected fallback report skatt-2025-01-15, got %v", reports)
	}
	// The full product name is kept when no prefix matched.
	if sk.Products[0].Name != "Mystery item" {
		t.Errorf("fallback entry name = %q, want %q", sk.Products[0].Name, "Mystery item")
	}
}

func TestParsePurchasesAggregatesSameProductLine(t *testing.T) {
	cfg := testConfig()
	reports := ParsePurchases(cfg, []zettle.Purchase{
		purchase("2025-01-15T10:00:00.000+0000",
			zettle.Product{Name: "CA - Kaffe", Quantity: "1", UnitPrice: 1500}),
		purchase("2025-01-15T12:00:00.000+0000",
			zettle.Product{Name: "CA - Kaffe", Quantity: "2", UnitPrice: 1500}),
	})

	cafe := reports["cafe-2025-01-15"]
	if len(cafe.Products) != 1 {
		t.Fatalf("expected identical lines to merge, got %d lines", len(cafe.Products))
	}
	if got := cafe.Products[0].Quantity; got != 3 {
		t.Errorf("merged quantity = %d, want 3", got)
	}
	if cafe.Total != 4500 {
		t.Errorf("total = %d, want 4500", cafe.Total)
	}
	if len(cafe.AccountDistributions) != 1 || cafe.AccountDistributions[0].Total != 4500 {
		t.Errorf("distributions = %+v, want single 3001/4500", cafe.AccountDistributions)
	}
}

func TestParsePurchasesAppliesFixedDiscount(t *testing.T) {
	cfg := testConfig()
	p := purchase("2025-01-15T10:00:00.000+0000",
		zettle.Product{Name: "CA - Kaffe", Quantity: "1", UnitPrice: 2000})
	p.Discounts = []zettle.PurchaseDiscount{{Value: 500}}

	reports := ParsePurchases(cfg, []zettle.Purchase{p})
	cafe := reports["cafe-2025-01-15"]

	if cafe.Total != 1500 {
		t.Errorf("total after discount = %d, want 1500", cafe.Total)
	}
	if got := cafe.Products[0].EffectiveDiscount; got != 500 {
		t.Errorf("effective discount = %d, want 500", got)
	}
	if got := cafe.AccountDistributions[0].Total; got != 1500 {
		t.Errorf("distribution total = %d, want 1500", got)
	}
}

func TestParsePurchasesSplitsDiscountAcrossLines(t *testing.T) {
	cfg := testConfig()
	p := purchase("2025-01-15T10:00:00.000+0000",
		zettle.Product{Name: "CA - Kaffe", Quantity: "1", UnitPrice: 3000},
		zettle.Product{Name: "SK - Godis", Quantity: "1", UnitPrice: 1000},
	)
	p.Discounts = []zettle.PurchaseDiscount{{Value: 400}}

	reports := ParsePurchases(cfg, []zettle.Purchase{p})

	// 400 öre split proportionally over 3000+1000: 300 and 100.
	if got := reports["cafe-2025-01-15"].Total; got != 2700 {
		t.Errorf("cafe total = %d, want 2700", got)
	}
	if got := reports["skatt-2025-01-15"].Total; got != 900 {
		t.Errorf("skatt total = %d, want 900", got)
	}
}

func TestPurchaseDiscountValue(t *testing.T) {
	amount := 250
	pct := 10.0
	cases := []struct {
		name     string
		discount zettle.PurchaseDiscount
		total    int
		want     int
	}{
		{"value wins", zettle.PurchaseDiscount{Value: 500, Amount: &amount}, 1000, 500},
		{"amount", zettle.PurchaseDiscount{Amount: &amount}, 1000, 250},
		{"percentage rounds", zettle.PurchaseDiscount{Percentage: &pct}, 1015, 102}, // 101.5 → 102
		{"empty", zettle.PurchaseDiscount{}, 1000, 0},
	}
	for _, tc := range cases {
		if got := purchaseDiscountValue(tc.discount, tc.total); got != tc.want {
			t.Errorf("%s: purchaseDiscountValue = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestAllocateByLargestRemainderIsExact(t *testing.T) {
	lines := []saleLine{{total: 3}, {total: 3}, {total: 3}}
	got := allocateByLargestRemainder(10, lines, 9)

	sum := 0
	for _, a := range got {
		sum += a
	}
	if sum != 10 {
		t.Fatalf("allocations %v sum to %d, want exactly 10", got, sum)
	}
	// Equal remainders: the extra öre lands on the earliest line.
	if got[0] != 4 || got[1] != 3 || got[2] != 3 {
		t.Errorf("allocations = %v, want [4 3 3]", got)
	}
}

func TestAllocateByLargestRemainderSkipsNonPositiveLines(t *testing.T) {
	lines := []saleLine{{total: 500}, {total: -200}, {total: 500}}
	got := allocateByLargestRemainder(100, lines, 1000)
	if got[1] != 0 {
		t.Errorf("negative line was allocated %d, want 0", got[1])
	}
	if got[0]+got[2] != 100 {
		t.Errorf("allocations = %v, want the full 100 on positive lines", got)
	}
}

func TestRoundDiv(t *testing.T) {
	cases := []struct{ n, d, want int }{
		{5, 2, 3},   // rounds half up
		{4, 2, 2},   // exact
		{1, 3, 0},   // rounds down
		{2, 3, 1},   // rounds up
		{100, 0, 0}, // guards division by zero
	}
	for _, tc := range cases {
		if got := roundDiv(tc.n, tc.d); got != tc.want {
			t.Errorf("roundDiv(%d, %d) = %d, want %d", tc.n, tc.d, got, tc.want)
		}
	}
}
