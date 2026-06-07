package fortnox

type VoucherCreateRequest struct {
	Voucher VoucherCreate `json:"Voucher"`
}

type VoucherCreate struct {
	Comments        string       `json:"Comments,omitempty"`
	Description     string       `json:"Description"`
	TransactionDate string       `json:"TransactionDate"`
	VoucherRows     []VoucherRow `json:"VoucherRows"`
	VoucherSeries   string       `json:"VoucherSeries"`
}

type VoucherRow struct {
	Account                int     `json:"Account"`
	Credit                 float64 `json:"Credit"`
	Debit                  float64 `json:"Debit"`
	TransactionInformation string  `json:"TransactionInformation,omitempty"`
	CostCenter             string  `json:"CostCenter,omitempty"`
}

type VoucherCreateResponse struct {
	Voucher VoucherResult `json:"Voucher"`
}

type VoucherResult struct {
	VoucherSeries string `json:"VoucherSeries"`
	VoucherNumber int    `json:"VoucherNumber"`
	Year          int    `json:"Year"`
}

type InboxUploadResponse struct {
	File InboxFile `json:"File"`
}

type InboxFile struct {
	ID string `json:"Id"`
}

type VoucherFileConnectionCreateRequest struct {
	VoucherFileConnection VoucherFileConnectionCreate `json:"VoucherFileConnection"`
}

type VoucherFileConnectionCreate struct {
	FileID        string `json:"FileId"`
	VoucherSeries string `json:"VoucherSeries"`
	VoucherNumber int    `json:"VoucherNumber"`
}

type VoucherFileConnectionResult struct {
	FileID        string `json:"FileId"`
	VoucherSeries string `json:"VoucherSeries"`
	VoucherNumber int    `json:"VoucherNumber"`
}

type FinancialYear struct {
	ID        int    `json:"Id"`
	FromDate  string `json:"FromDate"`
	ToDate    string `json:"ToDate"`
}

type FinancialYearListResponse struct {
	FinancialYears []FinancialYear `json:"FinancialYears"`
}

type VoucherSummary struct {
	VoucherSeries   string `json:"VoucherSeries"`
	VoucherNumber   int    `json:"VoucherNumber"`
	Year            int    `json:"Year"`
	Description     string `json:"Description"`
	TransactionDate string `json:"TransactionDate"`
}

type MetaInformation struct {
	TotalResources int `json:"@TotalResources"`
	TotalPages     int `json:"@TotalPages"`
	CurrentPage    int `json:"@CurrentPage"`
}

type VoucherListResponse struct {
	Vouchers        []VoucherSummary `json:"Vouchers"`
	MetaInformation MetaInformation  `json:"MetaInformation"`
}

type VoucherFileConnectionCreateResponse struct {
	VoucherFileConnection VoucherFileConnectionResult `json:"VoucherFileConnection"`
}
