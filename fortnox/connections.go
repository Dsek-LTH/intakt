package fortnox

import (
	"context"
	"fmt"
)

// CreateVoucherFileConnection attaches an inbox file to a voucher.
// fiscalYear is the Fortnox fiscal year ID returned when the voucher was created.
func (c *Client) CreateVoucherFileConnection(ctx context.Context, fiscalYear int, req VoucherFileConnectionCreateRequest) (*VoucherFileConnectionResult, error) {
	var resp VoucherFileConnectionCreateResponse
	path := fmt.Sprintf("/voucherfileconnections?financialyear=%d", fiscalYear)
	if err := c.postJSON(ctx, path, req, &resp); err != nil {
		return nil, err
	}
	if resp.VoucherFileConnection.FileID == "" {
		return nil, fmt.Errorf("Fortnox did not return a file connection")
	}
	return &resp.VoucherFileConnection, nil
}
