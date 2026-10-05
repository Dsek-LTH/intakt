package zettle

import (
	"context"
	"sync"
	"time"
)

// maxFetchShards bounds how many concurrent date-sharded requests a single
// range fetch fans out into. The shared rate limiter (zettleRate) keeps the
// combined request rate within Zettle's limits regardless of this value.
const maxFetchShards = 4

// GetPurchasesRange fetches all purchases with transaction dates in the
// inclusive range [start, end], splitting the range across up to
// maxFetchShards concurrent workers. The result may contain boundary
// duplicates; callers deduplicate by PurchaseUUID.
func (c *Client) GetPurchasesRange(ctx context.Context, start, end time.Time) ([]Purchase, error) {
	return fetchSharded(ctx, start, end, c.GetPurchases)
}

// GetAllTransactionsRange fetches all reconciliation transactions in the
// inclusive range [start, end], sharded like GetPurchasesRange. Callers
// deduplicate by originating transaction UUID and type.
func (c *Client) GetAllTransactionsRange(ctx context.Context, start, end time.Time) ([]TransactionFee, error) {
	return fetchSharded(ctx, start, end, c.GetAllTransactions)
}

// fetchSharded splits [start, end] into contiguous sub-ranges and runs fetch on
// each concurrently, concatenating the results in range order. It returns the
// first error encountered.
func fetchSharded[T any](
	ctx context.Context,
	start, end time.Time,
	fetch func(ctx context.Context, startDate, endDate string) ([]T, error),
) ([]T, error) {
	ranges := shardRange(start, end, maxFetchShards)

	results := make([][]T, len(ranges))
	errs := make([]error, len(ranges))
	var wg sync.WaitGroup
	for i, r := range ranges {
		i, r := i, r
		wg.Add(1)
		go func() {
			defer wg.Done()
			// endDate is exclusive of the following day so the whole end day is
			// included, matching the single-range behaviour.
			results[i], errs[i] = fetch(ctx, apiTimestamp(r.start), apiTimestamp(r.end.AddDate(0, 0, 1)))
		}()
	}
	wg.Wait()

	var all []T
	for i := range ranges {
		if errs[i] != nil {
			return nil, errs[i]
		}
		all = append(all, results[i]...)
	}
	return all, nil
}

// dateRange is an inclusive span of whole days.
type dateRange struct{ start, end time.Time }

// shardRange splits the inclusive day span [start, end] into at most n
// contiguous sub-ranges of whole days, distributing any remainder onto the
// earliest shards. It never returns more shards than there are days.
func shardRange(start, end time.Time, n int) []dateRange {
	start = dateOnly(start)
	end = dateOnly(end)
	if n < 1 {
		n = 1
	}

	days := int(end.Sub(start).Hours()/24) + 1 // inclusive of both endpoints
	if days < 1 {
		days = 1
	}
	if n > days {
		n = days
	}

	ranges := make([]dateRange, 0, n)
	base, extra := days/n, days%n
	cur := start
	for i := 0; i < n; i++ {
		size := base
		if i < extra {
			size++
		}
		subEnd := cur.AddDate(0, 0, size-1)
		ranges = append(ranges, dateRange{start: cur, end: subEnd})
		cur = subEnd.AddDate(0, 0, 1)
	}
	return ranges
}

// dateOnly strips the time-of-day, keeping the location.
func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// apiTimestamp formats a time for Zettle's date-range query parameters.
func apiTimestamp(t time.Time) string {
	return t.Format("2006-01-02T15:04:05-07:00")
}
