// Package server exposes the income pipeline over HTTP.
//
// The service is a pure transform and holds no state of its own. It does not
// know what a committee is beyond what arrives in the request, does not decide
// what a report is worth, and never talks to Fortnox — the caller (utlagg) owns
// the configuration, the review, and the booking. What lives here is the part
// that is genuinely intakt's: reaching Zettle, reading a Swish export, grouping
// takings into a day's report, and rendering that report as a PDF.
//
// That division is deliberate. Fortnox vouchers are written with the
// bookkeeper's own login so Fortnox attributes them by name, which is something
// only the app — the thing that knows who is signed in — can do.
package server

import (
	"intakt/config"
	"intakt/report"
)

// reportsRequest asks for a date range. The configuration comes with it: the
// caller holds the routing table in its database, so there is one source of
// truth for it and no config file to keep in step on this side.
type reportsRequest struct {
	From   string         `json:"from"` // YYYY-MM-DD, inclusive, Swedish local day
	To     string         `json:"to"`   // YYYY-MM-DD, inclusive
	Config *config.Config `json:"config"`
}

// reportItem is one report plus the key it is filed under. The key is
// "<committee>-<date>" and is derivable from the body, but it is what the
// caller stores rows against, so it is stated rather than implied.
//
// The embedded pointer inlines the report's own fields, so the JSON is flat:
// {key, committee, date, products, total, accountDistributions}.
type reportItem struct {
	Key string `json:"key"`
	*report.Report
}

// feeReportItem is the same idea for Zettle's payment fees, which are not
// grouped by committee — the key is just the date.
type feeReportItem struct {
	Key string `json:"key"`
	*report.FeeReport
}

type reportsResponse struct {
	Reports []reportItem `json:"reports"`
}

type feeReportsResponse struct {
	Reports []feeReportItem `json:"reports"`
}

// renderRequest asks for one report as a PDF. Exactly one of Report and
// FeeReport must be set — they use different skeletons.
//
// The report is sent back rather than looked up, because by this point it is no
// longer the one that was fetched: the caller has reviewed it, and the whole
// reason fetching and rendering are separate calls is that the cost centre can
// be corrected in between. A service that cached the fetch and rendered from
// its own copy would render the wrong thing.
type renderRequest struct {
	Config *config.Config `json:"config"`
	// "Zettle" or "Swish" — what appears under the heading. Ignored for fees.
	PaymentType string            `json:"paymentType"`
	Report      *report.Report    `json:"report,omitempty"`
	FeeReport   *report.FeeReport `json:"feeReport,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}
