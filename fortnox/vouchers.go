package fortnox

import (
	"context"
	"fmt"
	"net/url"
)

func (c *Client) FinancialYearForDate(ctx context.Context, date string) (int, error) {
	var resp FinancialYearListResponse
	if err := c.getJSON(ctx, "/financialyears", &resp); err != nil {
		return 0, err
	}
	for _, fy := range resp.FinancialYears {
		if date >= fy.FromDate && date <= fy.ToDate {
			return fy.ID, nil
		}
	}
	return 0, fmt.Errorf("no financial year found containing date %s", date)
}

func (c *Client) ListVouchersBySeries(ctx context.Context, series string, financialYearID int) ([]VoucherSummary, error) {
	var all []VoucherSummary
	for page := 1; ; page++ {
		params := url.Values{}
		params.Set("voucherseries", series)
		params.Set("financialyear", fmt.Sprintf("%d", financialYearID))
		params.Set("limit", "500")
		params.Set("page", fmt.Sprintf("%d", page))
		var resp VoucherListResponse
		if err := c.getJSON(ctx, "/vouchers?"+params.Encode(), &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Vouchers...)
		if page >= resp.MetaInformation.TotalPages || resp.MetaInformation.TotalPages == 0 {
			break
		}
	}
	return all, nil
}

func (c *Client) DeleteVoucher(ctx context.Context, series string, number, financialYearID int) error {
	return c.deleteReq(ctx, fmt.Sprintf("/vouchers/%s/%d?financialyear=%d", series, number, financialYearID))
}

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
