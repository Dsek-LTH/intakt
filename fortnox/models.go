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

type VoucherFileConnectionCreateResponse struct {
	VoucherFileConnection VoucherFileConnectionResult `json:"VoucherFileConnection"`
}
