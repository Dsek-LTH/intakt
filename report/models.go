package report

// The json tags are the wire contract with utlagg — these three types cross the
// HTTP API in both directions (server/api.go), so a field renamed here is a
// field the app stops reading. They are spelled out rather than left to Go's
// field names because the caller reads `committee` and `products`, not
// `UtskottPrefix` and `Products`.
type Entry struct {
	Name              string `json:"name"`
	Variant           string `json:"variant,omitempty"`
	Account           string `json:"account"`
	Quantity          int    `json:"quantity"`
	UnitPrice         int    `json:"unitPrice"`
	EffectiveDiscount int    `json:"effectiveDiscount"`
	Total             int    `json:"total"`
	IsDiscount        bool   `json:"isDiscount,omitempty"`
}

type AccountDistribution struct {
	Account     string `json:"account"`
	Description string `json:"description"`
	Total       int    `json:"total"`
}

type Report struct {
	// "committee" over the wire: the field is the prefix a committee is filed
	// under, and the app calls that a verksamhet's key.
	UtskottPrefix        string                `json:"committee"`
	Date                 string                `json:"date"` // YYYY-MM-DD
	Products             []Entry               `json:"products"`
	Total                int                   `json:"total"`
	AccountDistributions []AccountDistribution `json:"accountDistributions"`
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
