package report

// Subdirectories of out_path.reports that the compile functions render PDFs
// into, one per source. The CLI derives each voucher's PDF path and display
// label from these, so they are part of this package's contract.
const (
	SubdirPurchases      = "purchases"
	SubdirFees           = "fees"
	SubdirSwish          = "swish"
	SubdirReconciliation = "reconciliation"
)

// FeeFilePrefix distinguishes fee voucher files (PDF and pending JSON) from
// sales vouchers, whose keys are "<committee>-<date>" without a prefix.
const FeeFilePrefix = "avgift-"

type Entry struct {
	Name              string
	Variant           string
	Account           string
	Quantity          int
	UnitPrice         int
	EffectiveDiscount int
	Total             int
	IsDiscount        bool
}

type AccountDistribution struct {
	Account     string
	Description string
	Total       int
}

type Report struct {
	UtskottPrefix        string
	Date                 string
	Products             []Entry
	Total                int
	AccountDistributions []AccountDistribution
}

type FeeReport struct {
	Date  string     `json:"date"`
	Fees  []FeeEntry `json:"fees"`
	Total int        `json:"total"`
}

type FeeEntry struct {
	Name      string `json:"name"`
	Account   string `json:"account"`
	Quantity  int    `json:"quantity"`
	UnitPrice int    `json:"unitPrice"`
	Total     int    `json:"total"`
}
