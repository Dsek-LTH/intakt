package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"intakt/config"
	"intakt/zettle"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

const testToken = "test-token"

// fakeZettle stands in for the Zettle API so these tests need no credentials
// and no network. What it returns is shaped like the real thing: a purchase
// with prefixed product names, which is what the routing table acts on.
type fakeZettle struct {
	purchases []zettle.Purchase
	fees      []zettle.TransactionFee
	err       error

	gotStart, gotEnd string
}

func (f *fakeZettle) GetPurchases(_ context.Context, start, end string) ([]zettle.Purchase, error) {
	f.gotStart, f.gotEnd = start, end
	return f.purchases, f.err
}

func (f *fakeZettle) GetAllTransactions(_ context.Context, start, end string) ([]zettle.TransactionFee, error) {
	f.gotStart, f.gotEnd = start, end
	return f.fees, f.err
}

func testConfig() *config.Config {
	return &config.Config{
		Attestation: config.Attestation{Name: "Test Testsson", StilID: "te0000st-s"},
		AccountDescriptions: map[string]string{
			"3000": "Försäljning inom Sverige",
			"6570": "Bankkostnader",
		},
		PrefixMappings: map[string]config.PrefixAccount{
			"c":  {Name: "cafe", Account: 3000},
			"sk": {Name: "skatt", Account: 3000},
			"z":  {Name: "skatt", Account: 6570},
		},
		Committees: map[string]config.CommitteeInfo{
			"cafe":  {ProperName: "Cafémästeriet", Description: "Cafémästeriets verksamhet", DefaultCostCenter: "CAFE02"},
			"skatt": {ProperName: "Skattmästeriet", Description: "Skattmästeriets verksamhet"},
		},
	}
}

func newTestServer(t *testing.T, z ZettleClient) http.Handler {
	t.Helper()
	srv, err := New(Options{AssetsDir: "../assets", Token: testToken, Zettle: z})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv.Handler()
}

func postJSON(t *testing.T, h http.Handler, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshalling request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// ── Auth ──────────────────────────────────────────────────────────

func TestHealthzNeedsNoToken(t *testing.T) {
	h := newTestServer(t, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestEndpointsRequireTheToken(t *testing.T) {
	h := newTestServer(t, &fakeZettle{})

	paths := []string{"/v1/reports/zettle", "/v1/reports/fees", "/v1/reports/swish", "/v1/render"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			for _, token := range []string{"", "wrong-token"} {
				rec := postJSON(t, h, path, reportsRequest{}, token)
				if rec.Code != http.StatusUnauthorized {
					t.Errorf("token %q: status = %d, want 401", token, rec.Code)
				}
			}
		})
	}
}

// ── Fetching ──────────────────────────────────────────────────────

func TestZettleReportsGroupsByCommitteeAndDay(t *testing.T) {
	z := &fakeZettle{
		purchases: []zettle.Purchase{
			{
				Timestamp: "2026-01-02T10:00:00.000+0100",
				Products: []zettle.Product{
					{Name: "c-Cola", Quantity: "2", UnitPrice: 1300},
					{Name: "sk-Biljett", Quantity: "1", UnitPrice: 5000},
				},
			},
			{
				Timestamp: "2026-01-02T12:00:00.000+0100",
				Products: []zettle.Product{
					{Name: "c-Cola", Quantity: "1", UnitPrice: 1300},
				},
			},
		},
	}

	h := newTestServer(t, z)
	rec := postJSON(t, h, "/v1/reports/zettle", reportsRequest{
		From: "2026-01-01", To: "2026-01-31", Config: testConfig(),
	}, testToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var got reportsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if len(got.Reports) != 2 {
		t.Fatalf("got %d reports, want 2 (cafe and skatt for the same day)", len(got.Reports))
	}

	// Sorted, so this order is guaranteed rather than incidental.
	if got.Reports[0].Key != "cafe-2026-01-02" {
		t.Errorf("first key = %q, want cafe-2026-01-02", got.Reports[0].Key)
	}
	if got.Reports[1].Key != "skatt-2026-01-02" {
		t.Errorf("second key = %q, want skatt-2026-01-02", got.Reports[1].Key)
	}

	// Three colas across two purchases, merged onto one line.
	if got.Reports[0].Total != 3900 {
		t.Errorf("cafe total = %d öre, want 3900", got.Reports[0].Total)
	}
	if got.Reports[1].Total != 5000 {
		t.Errorf("skatt total = %d öre, want 5000", got.Reports[1].Total)
	}
}

// The upper bound Zettle is given must be the day after the one asked for,
// because Zettle excludes it and the caller's range is inclusive.
//
// Both boundaries are Swedish midnight and carry Sweden's real offset, so the
// window matches the days zettle.ParseDate groups purchases into.
func TestZettleRangeIsInclusiveSwedishMidnight(t *testing.T) {
	z := &fakeZettle{}
	h := newTestServer(t, z)

	rec := postJSON(t, h, "/v1/reports/zettle", reportsRequest{
		From: "2026-01-01", To: "2026-01-31", Config: testConfig(),
	}, testToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	if want := "2026-01-01T00:00:00+01:00"; z.gotStart != want {
		t.Errorf("start = %q, want %q", z.gotStart, want)
	}
	if want := "2026-02-01T00:00:00+01:00"; z.gotEnd != want {
		t.Errorf("end = %q, want %q (the day after, since Zettle excludes it)", z.gotEnd, want)
	}
}

func TestZettleReportsReportsUpstreamFailureAsBadGateway(t *testing.T) {
	h := newTestServer(t, &fakeZettle{err: fmt.Errorf("zettle is down")})

	rec := postJSON(t, h, "/v1/reports/zettle", reportsRequest{
		From: "2026-01-01", To: "2026-01-31", Config: testConfig(),
	}, testToken)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
	// The upstream message must not be echoed back verbatim.
	if strings.Contains(rec.Body.String(), "zettle is down") {
		t.Error("the upstream error leaked into the response body")
	}
}

func TestZettleEndpointsWhenZettleIsNotConfigured(t *testing.T) {
	h := newTestServer(t, nil)

	for _, path := range []string{"/v1/reports/zettle", "/v1/reports/fees"} {
		rec := postJSON(t, h, path, reportsRequest{
			From: "2026-01-01", To: "2026-01-31", Config: testConfig(),
		}, testToken)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status = %d, want 503", path, rec.Code)
		}
	}
}

func TestFeeReportsGroupByDay(t *testing.T) {
	z := &fakeZettle{
		fees: []zettle.TransactionFee{
			{Timestamp: "2026-01-02T10:00:00.000+0100", Amount: -150},
			{Timestamp: "2026-01-02T12:00:00.000+0100", Amount: -150},
			{Timestamp: "2026-01-03T12:00:00.000+0100", Amount: -200},
		},
	}

	h := newTestServer(t, z)
	rec := postJSON(t, h, "/v1/reports/fees", reportsRequest{
		From: "2026-01-01", To: "2026-01-31", Config: testConfig(),
	}, testToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	var got feeReportsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if len(got.Reports) != 2 {
		t.Fatalf("got %d fee reports, want 2", len(got.Reports))
	}
	// Fees arrive negative from Zettle and are stored as a positive cost.
	if got.Reports[0].Total != 300 {
		t.Errorf("2026-01-02 total = %d öre, want 300", got.Reports[0].Total)
	}
}

// ── Validation ────────────────────────────────────────────────────

func TestBadRequests(t *testing.T) {
	h := newTestServer(t, &fakeZettle{})

	tests := []struct {
		name string
		body reportsRequest
	}{
		{"no config", reportsRequest{From: "2026-01-01", To: "2026-01-31"}},
		{
			"config that does not validate",
			reportsRequest{From: "2026-01-01", To: "2026-01-31", Config: &config.Config{}},
		},
		{
			"unparseable from date",
			reportsRequest{From: "the first", To: "2026-01-31", Config: testConfig()},
		},
		{
			"range that runs backwards",
			reportsRequest{From: "2026-02-01", To: "2026-01-01", Config: testConfig()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := postJSON(t, h, "/v1/reports/zettle", tt.body, testToken)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// A field the service does not know about usually means the caller and the
// service disagree about the shape. Dropping it silently would mean a report
// that books differently than the caller believes it will.
func TestUnknownFieldsAreRejected(t *testing.T) {
	h := newTestServer(t, &fakeZettle{})

	body := `{"from":"2026-01-01","to":"2026-01-31","config":null,"surprise":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/reports/zettle", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ── Swish ─────────────────────────────────────────────────────────

func swishMultipart(t *testing.T, cfg *config.Config, from, to, csv string) (string, io.Reader) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshalling config: %v", err)
	}
	if err := mw.WriteField("config", string(raw)); err != nil {
		t.Fatalf("writing config field: %v", err)
	}
	if err := mw.WriteField("from", from); err != nil {
		t.Fatalf("writing from field: %v", err)
	}
	if err := mw.WriteField("to", to); err != nil {
		t.Fatalf("writing to field: %v", err)
	}

	part, err := mw.CreateFormFile("file", "swish.csv")
	if err != nil {
		t.Fatalf("creating file part: %v", err)
	}
	// The real export is ISO-8859-1; the parser decodes it, so the fixture is
	// written in that encoding too.
	encoded := make([]byte, 0, len(csv))
	for _, r := range csv {
		encoded = append(encoded, byte(r))
	}
	if _, err := part.Write(encoded); err != nil {
		t.Fatalf("writing file part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("closing multipart writer: %v", err)
	}

	return mw.FormDataContentType(), &buf
}

func TestSwishReportsFromUpload(t *testing.T) {
	h := newTestServer(t, nil)

	csv := "* Swish-rapport\n" +
		"Radnr,Clnr,Kontonr,Bokfdag,Transdag,Valutadag,Mottagarnr,Mottagarnamn,Avsändarnr,Avsändarnamn,Meddelande,Tid,Belopp,Orderreferens\n" +
		"1,00000,0000000000,2026-01-02,2026-01-02,2026-01-05,0000000000,TEST,+46700000000,TEST,c-Cola,10:51,13.00,\n" +
		"2,00000,0000000000,2026-01-02,2026-01-02,2026-01-05,0000000000,TEST,+46700000000,TEST,c-Cola,13:13,13.00,\n" +
		"3,00000,0000000000,2026-01-03,2026-01-03,2026-01-06,0000000000,TEST,+46700000000,TEST,sk-Biljett,09:00,50.00,\n"

	contentType, body := swishMultipart(t, testConfig(), "2026-01-01", "2026-01-31", csv)
	req := httptest.NewRequest(http.MethodPost, "/v1/reports/swish", body)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var got reportsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if len(got.Reports) != 2 {
		t.Fatalf("got %d reports, want 2", len(got.Reports))
	}
	if got.Reports[0].Key != "cafe-2026-01-02" || got.Reports[0].Total != 2600 {
		t.Errorf("first report = %s / %d öre, want cafe-2026-01-02 / 2600",
			got.Reports[0].Key, got.Reports[0].Total)
	}
	if got.Reports[1].Key != "skatt-2026-01-03" || got.Reports[1].Total != 5000 {
		t.Errorf("second report = %s / %d öre, want skatt-2026-01-03 / 5000",
			got.Reports[1].Key, got.Reports[1].Total)
	}
}

func TestSwishReportsRequiresTheFile(t *testing.T) {
	h := newTestServer(t, nil)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	raw, _ := json.Marshal(testConfig())
	_ = mw.WriteField("config", string(raw))
	_ = mw.WriteField("from", "2026-01-01")
	_ = mw.WriteField("to", "2026-01-31")
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/reports/swish", &buf)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ── Render ────────────────────────────────────────────────────────

func requirePdflatex(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("pdflatex"); err != nil {
		t.Skip("pdflatex is not installed; skipping render tests")
	}
}

func TestRenderReturnsPDF(t *testing.T) {
	requirePdflatex(t)
	h := newTestServer(t, nil)

	rec := postJSON(t, h, "/v1/render", map[string]any{
		"config":      testConfig(),
		"paymentType": "Swish",
		"report": map[string]any{
			"committee": "cafe",
			"date":      "2026-01-02",
			"total":     2600,
			"products": []map[string]any{
				{"name": "Cola", "account": "3000", "quantity": 2, "unitPrice": 1300, "effectiveDiscount": 0, "total": 2600},
			},
			"accountDistributions": []map[string]any{
				{"account": "3000", "description": "Försäljning inom Sverige", "total": 2600},
			},
		},
	}, testToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", ct)
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF-")) {
		t.Error("response body is not a PDF")
	}
}

func TestRenderRejectsAmbiguousBody(t *testing.T) {
	h := newTestServer(t, nil)

	tests := []struct {
		name string
		body map[string]any
	}{
		{"neither report nor feeReport", map[string]any{"config": testConfig()}},
		{
			"both at once",
			map[string]any{
				"config":    testConfig(),
				"report":    map[string]any{"committee": "cafe", "date": "2026-01-02"},
				"feeReport": map[string]any{"date": "2026-01-02"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := postJSON(t, h, "/v1/render", tt.body, testToken)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// New refuses to build a service that would accept every request.
func TestNewRequiresAToken(t *testing.T) {
	if _, err := New(Options{AssetsDir: "../assets"}); err == nil {
		t.Error("New accepted an empty token")
	}
	if _, err := New(Options{Token: testToken}); err == nil {
		t.Error("New accepted an empty assets directory")
	}
}
