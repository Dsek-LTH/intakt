package fortnox

import (
	"context"
	"fmt"
)

func (c *Client) CreateVoucher(ctx context.Context, req VoucherCreateRequest) (*VoucherResult, error) {
	var resp VoucherCreateResponse

	if err := c.postJSON(ctx, "/vouchers", req, &resp); err != nil {
		return nil, err
	}

	if resp.Voucher.VoucherSeries == "" || resp.Voucher.VoucherNumber == 0 {
		return nil, fmt.Errorf("fortnox voucher response did not contain voucher series and number")
	}

	return &resp.Voucher, nil
}
