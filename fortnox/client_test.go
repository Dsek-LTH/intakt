package fortnox

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// staticToken is a TokenProvider stub for tests.
type staticToken string

func (s staticToken) AccessToken(context.Context) (string, error) { return string(s), nil }

// testClient returns a Client pointed at a stub API server, with an
// unthrottled rate limiter so tests stay fast.
func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{
		baseURL:       srv.URL,
		httpClient:    srv.Client(),
		tokenProvider: staticToken("test-token"),
		limiter:       rate.NewLimiter(rate.Inf, 1),
	}
}

func TestCreateVoucherSendsAuthAndParsesResult(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vouchers" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		w.Write([]byte(`{"Voucher": {"VoucherSeries": "IR", "VoucherNumber": 42, "Year": 7}}`))
	})

	res, err := c.CreateVoucher(context.Background(), VoucherCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if res.VoucherSeries != "IR" || res.VoucherNumber != 42 || res.Year != 7 {
		t.Errorf("result = %+v", res)
	}
}

func TestCreateVoucherRejectsIncompleteResponse(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"Voucher": {}}`))
	})
	if _, err := c.CreateVoucher(context.Background(), VoucherCreateRequest{}); err == nil {
		t.Error("expected an error for a response without series/number")
	}
}

func TestErrorResponsesIncludeStatusAndBody(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"ErrorInformation":{"message":"kontot saknas"}}`, http.StatusBadRequest)
	})
	_, err := c.CreateVoucher(context.Background(), VoucherCreateRequest{})
	if err == nil {
		t.Fatal("expected an error for HTTP 400")
	}
	for _, want := range []string{"400", "kontot saknas"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestDoRetriesOn429AndResendsBody(t *testing.T) {
	var bodies []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if len(bodies) < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"Voucher": {"VoucherSeries": "IR", "VoucherNumber": 1, "Year": 1}}`))
	})

	if _, err := c.CreateVoucher(context.Background(), VoucherCreateRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 3 {
		t.Fatalf("got %d requests, want 3", len(bodies))
	}
	for i, b := range bodies[1:] {
		if b != bodies[0] {
			t.Errorf("retry %d body = %q, want %q", i+1, b, bodies[0])
		}
	}
}

func TestDoGivesUpAfterRepeated429(t *testing.T) {
	requests := 0
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, err := c.CreateVoucher(context.Background(), VoucherCreateRequest{})
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("err = %v, want a 429 error", err)
	}
	if requests != 4 {
		t.Errorf("got %d requests, want 4 (initial + 3 retries)", requests)
	}
}

func TestUploadToInboxSendsMultipartFile(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("no multipart file field: %v", err)
		} else {
			file.Close()
			if header.Filename != "receipt.pdf" {
				t.Errorf("filename = %q", header.Filename)
			}
		}
		w.Write([]byte(`{"File": {"Id": "abc-123"}}`))
	})

	id, err := c.UploadToInbox(context.Background(), "receipt.pdf", []byte("%PDF-1.4"))
	if err != nil {
		t.Fatal(err)
	}
	if id != "abc-123" {
		t.Errorf("file id = %q, want abc-123", id)
	}
}

func TestTokenStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "token.json")
	want := &StoredToken{
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(time.Hour).Truncate(time.Second),
	}
	if err := SaveToken(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken ||
		!got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("round trip mismatch: %+v", got)
	}
}
