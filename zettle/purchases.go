package zettle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const zettlePurchaseURL = "https://purchase.izettle.com/purchases/v2"
const purchaseLimit = 1000

func (c *Client) getPurchasePage(ctx context.Context, startDate, endDate, lastPurchaseHash string) (purchasePage, error) {
	u, err := url.Parse(zettlePurchaseURL)
	if err != nil {
		return purchasePage{}, fmt.Errorf("parsing purchase URL: %w", err)
	}

	query := u.Query()
	query.Set("limit", fmt.Sprint(purchaseLimit))
	query.Set("descending", "true")
	query.Set("startDate", startDate)
	query.Set("endDate", endDate)
	if lastPurchaseHash != "" {
		query.Set("lastPurchaseHash", lastPurchaseHash)
	}
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return purchasePage{}, fmt.Errorf("building purchase request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return purchasePage{}, fmt.Errorf("fetching purchases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return purchasePage{}, fmt.Errorf("unexpected status %d from purchase API", resp.StatusCode)
	}

	var page purchasePage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return purchasePage{}, fmt.Errorf("decoding purchase response: %w", err)
	}

	return page, nil
}

// GetPurchases retrieves all purchases in the given date range,
// following pagination automatically.
func (c *Client) GetPurchases(ctx context.Context, startDate, endDate string) ([]Purchase, error) {
	var all []Purchase
	lastHash := ""

	for {
		page, err := c.getPurchasePage(ctx, startDate, endDate, lastHash)
		if err != nil {
			return nil, err
		}

		all = append(all, page.Purchases...)

		if len(page.Purchases) == 0 {
			break
		}

		lastHash = page.LastPurchaseHash
	}

	return all, nil
}
