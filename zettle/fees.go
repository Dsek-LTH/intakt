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
const txPageLimit = 10000

func (c *Client) getTransactionPage(ctx context.Context, startDate, endDate string, txTypes []string, offset int) ([]TransactionFee, error) {
	u, err := url.Parse(zettleFinanceURL)
	if err != nil {
		return nil, fmt.Errorf("parsing finance transactions URL: %w", err)
	}

	query := u.Query()
	query.Set("limit", fmt.Sprint(txPageLimit))
	query.Set("offset", fmt.Sprint(offset))
	query.Set("start", startDate)
	query.Set("end", endDate)
	for _, t := range txTypes {
		query.Add("includeTransactionType", t)
	}
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("building transaction request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching transactions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("unexpected status %d from finance API: %s", resp.StatusCode, body)
	}

	var txs []TransactionFee
	if err := json.NewDecoder(resp.Body).Decode(&txs); err != nil {
		return nil, fmt.Errorf("decoding transaction response: %w", err)
	}

	return txs, nil
}

func (c *Client) getTransactionsByTypes(ctx context.Context, startDate, endDate string, txTypes []string) ([]TransactionFee, error) {
	var all []TransactionFee
	offset := 0

	for {
		txs, err := c.getTransactionPage(ctx, startDate, endDate, txTypes, offset)
		if err != nil {
			return nil, err
		}

		all = append(all, txs...)

		if len(txs) < txPageLimit {
			break
		}

		offset += len(txs)
	}

	return all, nil
}

// GetAllTransactions retrieves PAYMENT, PAYMENT_FEE, and PAYOUT transactions for reconciliation.
func (c *Client) GetAllTransactions(ctx context.Context, startDate, endDate string) ([]TransactionFee, error) {
	return c.getTransactionsByTypes(ctx, startDate, endDate, []string{TxTypePayment, TxTypePaymentFee, TxTypePayout})
}
