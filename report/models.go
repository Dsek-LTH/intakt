package report

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
