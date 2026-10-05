package server

import (
	"encoding/json"
	"fmt"
	"intakt/config"
	"intakt/report"
	"log"
	"net/http"
	"strings"
)

// POST /v1/reports/zettle — card takings for a date range, grouped into one
// report per committee per day.
func (s *Server) handleZettleReports(w http.ResponseWriter, r *http.Request) {
	var req reportsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateConfig(req.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	start, end, err := parseRange(req.From, req.To)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.zettle == nil {
		writeError(w, http.StatusServiceUnavailable, "zettle is not configured on this service")
		return
	}

	// The end date is shifted forward a day because Zettle's range excludes its
	// upper bound and the caller's is inclusive.
	purchases, err := s.zettle.GetPurchases(r.Context(),
		zettleTimestamp(start), zettleTimestamp(end.AddDate(0, 0, 1)))
	if err != nil {
		log.Printf("zettle purchases: %v", err)
		writeError(w, http.StatusBadGateway, "could not fetch purchases from Zettle")
		return
	}

	writeJSON(w, reportsResponse{Reports: sortedReports(report.ParsePurchases(req.Config, purchases))})
}

// POST /v1/reports/fees — Zettle's own payment fees for a date range, grouped
// by day. These are a cost rather than income, and use the other skeleton.
func (s *Server) handleFeeReports(w http.ResponseWriter, r *http.Request) {
	var req reportsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateConfig(req.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	start, end, err := parseRange(req.From, req.To)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.zettle == nil {
		writeError(w, http.StatusServiceUnavailable, "zettle is not configured on this service")
		return
	}

	fees, err := s.zettle.GetAllTransactions(r.Context(),
		zettleTimestamp(start), zettleTimestamp(end.AddDate(0, 0, 1)))
	if err != nil {
		log.Printf("zettle fees: %v", err)
		writeError(w, http.StatusBadGateway, "could not fetch fees from Zettle")
		return
	}

	writeJSON(w, feeReportsResponse{Reports: sortedFeeReports(report.ParseFees(req.Config, fees))})
}

// POST /v1/reports/swish — the same grouping, from an uploaded Swish export
// rather than an API. Multipart, because the CSV is a file: a `config` field
// holding the JSON config, `from` and `to` fields, and the export as `file`.
//
// The upload is streamed straight into the parser rather than read into memory
// first — a term's export is a couple of megabytes and there is no reason to
// hold a second copy of it.
func (s *Server) handleSwishReports(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCSVBody)

	if err := r.ParseMultipartForm(4 << 20); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid multipart body: %v", err))
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	raw := r.FormValue("config")
	if strings.TrimSpace(raw) == "" {
		writeError(w, http.StatusBadRequest, "the `config` field is required")
		return
	}
	var cfg config.Config
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid `config` field: %v", err))
		return
	}
	if err := validateConfig(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	start, end, err := parseRange(r.FormValue("from"), r.FormValue("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "the `file` field is required and must be the Swish CSV export")
		return
	}
	defer file.Close()

	reports, err := report.ParseSwish(&cfg, file, start, end)
	if err != nil {
		// A parse failure here is the caller's file, not the service's fault,
		// and the message names the row — worth returning verbatim.
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, reportsResponse{Reports: sortedReports(reports)})
}

// POST /v1/render — one report as a PDF.
//
// Separate from fetching on purpose: between the two the caller reviews what
// came back and sets the cost centre, and reports it decides to skip are never
// rendered at all. Fusing the two is what forced the old CLI to write a
// manifest file and have it hand-edited.
func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	var req renderRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateConfig(req.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	switch {
	case req.Report != nil && req.FeeReport != nil:
		writeError(w, http.StatusBadRequest, "send either `report` or `feeReport`, not both")
		return
	case req.Report == nil && req.FeeReport == nil:
		writeError(w, http.StatusBadRequest, "one of `report` or `feeReport` is required")
		return
	}

	// Wait for a render slot, or give up if the caller does. Holding the slot
	// for the whole render is the point — see Server.renderSlots.
	select {
	case s.renderSlots <- struct{}{}:
		defer func() { <-s.renderSlots }()
	case <-r.Context().Done():
		writeError(w, http.StatusRequestTimeout, "client went away while waiting to render")
		return
	}

	var (
		pdf []byte
		err error
	)
	if req.FeeReport != nil {
		pdf, err = report.RenderFeeReport(req.Config, s.assetsDir, req.FeeReport)
	} else {
		paymentType := req.PaymentType
		if strings.TrimSpace(paymentType) == "" {
			paymentType = "Zettle"
		}
		pdf, err = report.RenderReport(req.Config, s.assetsDir, req.Report, paymentType)
	}
	if err != nil {
		// The pdflatex log is in here and it is long. It goes to the service's
		// own log, where someone can read it; the caller gets told which step
		// failed and nothing else.
		log.Printf("render: %v", err)
		writeError(w, http.StatusInternalServerError, "could not render the report")
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="intaktsrakning.pdf"`)
	if _, err := w.Write(pdf); err != nil {
		log.Printf("writing pdf response: %v", err)
	}
}
