package zettle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const zettleFinanceURL = "https://finance.izettle.com/v2/accounts/LIQUID/transactions"

func (c *Client) getFeePage(ctx context.Context, startDate, endDate string, offset int) ([]TransactionFee, error) {
	u, err := url.Parse(zettleFinanceURL)
	if err != nil {
		return nil, fmt.Errorf("parsing finance transactions URL: %w", err)
	}

	query := u.Query()
	query.Set("limit", "50")
	query.Set("offset", fmt.Sprint(offset))
	query.Set("start", startDate)
	query.Set("end", endDate)
	query.Add("includeTransactionType", "PAYMENT_FEE")
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("building fee request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching fees: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("unexpected status %d from finance API: %s", resp.StatusCode, body)
	}

	var fees []TransactionFee
	if err := json.NewDecoder(resp.Body).Decode(&fees); err != nil {
		return nil, fmt.Errorf("decoding fee response: %w", err)
	}

	return fees, nil
}

// GetFees retrieves all payment fees in the given date range,
// following pagination automatically.
func (c *Client) GetFees(ctx context.Context, startDate, endDate string) ([]TransactionFee, error) {
	var all []TransactionFee
	offset := 0

	for {
		fees, err := c.getFeePage(ctx, startDate, endDate, offset)
		if err != nil {
			return nil, err
		}

		all = append(all, fees...)

		if len(fees) == 0 {
			break
		}

		offset += len(fees)
	}

	return all, nil
}
