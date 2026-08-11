package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"intakt/config"
	"intakt/report"
	"intakt/zettle"
	"log"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"time"
)

// Request body ceilings. A config with every committee in it is a few kilobytes
// and a report a little more; a Swish export for a whole term is a couple of
// megabytes. These are generous against those and still small enough that a
// malformed or hostile request cannot exhaust memory.
const (
	maxJSONBody = 8 << 20  // 8 MiB
	maxCSVBody  = 32 << 20 // 32 MiB
)

// ZettleClient is the slice of the Zettle API the handlers use. Named as an
// interface so the tests can serve canned purchases without reaching the
// network or holding real credentials.
type ZettleClient interface {
	GetPurchases(ctx context.Context, startDate, endDate string) ([]zettle.Purchase, error)
	// Named as the client names it: the fee report is built from the payment
	// and payout transactions behind a day, not from a "fees" endpoint.
	GetAllTransactions(ctx context.Context, startDate, endDate string) ([]zettle.TransactionFee, error)
}

type Options struct {
	// Directory holding the .tex skeletons and their images.
	AssetsDir string
	// Shared secret the caller presents as a bearer token. Required.
	Token string
	// Where Zettle data comes from. Nil is allowed: the Swish and render
	// endpoints work without it, and the Zettle ones report that they are not
	// configured rather than failing at startup — a deployment that only ever
	// imports Swish exports should not need Zettle credentials.
	Zettle ZettleClient
	// Ceiling on concurrent renders. Zero picks one from the CPU count.
	MaxConcurrentRenders int
}

type Server struct {
	assetsDir string
	token     string
	zettle    ZettleClient

	// A render is a pdflatex subprocess doing filesystem work — far heavier
	// than the parsing endpoints, and the one thing here that can flatten the
	// container if it arrives in bulk. Booking a term of Swish reports is
	// exactly that shape, so the concurrency is capped rather than left to
	// however many requests happen to be in flight.
	renderSlots chan struct{}
}

func New(opts Options) (*Server, error) {
	if opts.AssetsDir == "" {
		return nil, fmt.Errorf("assets directory is required")
	}
	if opts.Token == "" {
		return nil, fmt.Errorf("auth token is required")
	}

	slots := opts.MaxConcurrentRenders
	if slots <= 0 {
		slots = max(1, runtime.NumCPU())
	}

	return &Server{
		assetsDir:   opts.AssetsDir,
		token:       opts.Token,
		zettle:      opts.Zettle,
		renderSlots: make(chan struct{}, slots),
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Unauthenticated, and deliberately: this is what a container orchestrator
	// probes, and it reveals nothing.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.Handle("POST /v1/reports/zettle", s.authenticated(s.handleZettleReports))
	mux.Handle("POST /v1/reports/fees", s.authenticated(s.handleFeeReports))
	mux.Handle("POST /v1/reports/swish", s.authenticated(s.handleSwishReports))
	mux.Handle("POST /v1/render", s.authenticated(s.handleRender))

	return logRequests(mux)
}

// ── Middleware ────────────────────────────────────────────────────

func (s *Server) authenticated(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		presented, ok := strings.CutPrefix(header, "Bearer ")
		// Constant time, so the comparison cannot be turned into an oracle for
		// guessing the token a byte at a time.
		if !ok || subtle.ConstantTimeCompare([]byte(presented), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		// No request bodies and no query strings: they carry the section's
		// takings and, in the Swish case, its members' payments.
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

// ── Helpers ───────────────────────────────────────────────────────

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{Error: msg})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writing response: %v", err)
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	dec := json.NewDecoder(r.Body)
	// An unknown field is usually the caller and the service disagreeing about
	// the shape — worth a 400 rather than being silently dropped, since a
	// dropped field here is a report that quietly books differently.
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	return nil
}

// validateConfig is the same check Load applies to config.yaml. A request body
// is no more trustworthy than a file, and rather less.
func validateConfig(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	return cfg.Validate()
}

// parseRange reads the inclusive day range a request asks for. Both ends are
// Swedish local days — see report.Location for why that matters.
func parseRange(from, to string) (time.Time, time.Time, error) {
	start, err := report.ParseDay(from)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid `from` date %q: expected YYYY-MM-DD", from)
	}
	end, err := report.ParseDay(to)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid `to` date %q: expected YYYY-MM-DD", to)
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("`to` date %s is before `from` date %s", to, from)
	}
	return start, end, nil
}

// sortedReports flattens the keyed map the parsers return into a stable,
// ordered list. Go randomises map iteration, so without this the same request
// would come back in a different order every time and the caller's review
// screen would reshuffle on every fetch.
func sortedReports(reports map[string]*report.Report) []reportItem {
	items := make([]reportItem, 0, len(reports))
	for key, r := range reports {
		items = append(items, reportItem{Key: key, Report: r})
	}
	slices.SortFunc(items, func(a, b reportItem) int {
		return strings.Compare(a.Key, b.Key)
	})
	return items
}

func sortedFeeReports(reports map[string]*report.FeeReport) []feeReportItem {
	items := make([]feeReportItem, 0, len(reports))
	for key, r := range reports {
		items = append(items, feeReportItem{Key: key, FeeReport: r})
	}
	slices.SortFunc(items, func(a, b feeReportItem) int {
		return strings.Compare(a.Key, b.Key)
	})
	return items
}

// zettleTimestamp formats a range boundary for the Zettle API.
//
// The wall clock goes over as-is, labelled "+00:00" the way Zettle documents
// its own timestamps. Sending Sweden's real offset instead would move the
// boundary an hour or two against a clock that is already Swedish — the same
// mistake parseZettleDate describes, made at the other end of the round trip.
func zettleTimestamp(t time.Time) string {
	return t.Format("2006-01-02T15:04:05") + "+00:00"
}
