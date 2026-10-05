package fortnox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

const DefaultBaseURL = "https://api.fortnox.se/3"

// fortnoxRate keeps every rolling 5-second window under Fortnox's limit of
// 25 requests. The token bucket's burst counts on top of the refill rate
// within a window, so worst case is burst + 5s of refills: 4 + 20 = 24.
var fortnoxRate = rate.NewLimiter(rate.Every(250*time.Millisecond), 4)

type Client struct {
	baseURL       string
	httpClient    *http.Client
	tokenProvider TokenProvider
	limiter       *rate.Limiter
}

func NewClient(tokenProvider TokenProvider) *Client {
	return &Client{
		baseURL:       DefaultBaseURL,
		httpClient:    http.DefaultClient,
		tokenProvider: tokenProvider,
		limiter:       fortnoxRate,
	}
}

// OAuthConfigFromEnv reads FORTNOX_CLIENT_ID and FORTNOX_CLIENT_SECRET from
// the environment, failing with the missing variable's name.
func OAuthConfigFromEnv() (*OAuthConfig, error) {
	clientID := os.Getenv("FORTNOX_CLIENT_ID")
	clientSecret := os.Getenv("FORTNOX_CLIENT_SECRET")
	if clientID == "" {
		return nil, fmt.Errorf("missing env var FORTNOX_CLIENT_ID")
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("missing env var FORTNOX_CLIENT_SECRET")
	}
	return &OAuthConfig{ClientID: clientID, ClientSecret: clientSecret}, nil
}

// NewClientFromEnv builds a Client authenticated with a RefreshingTokenProvider,
// reading FORTNOX_CLIENT_ID and FORTNOX_CLIENT_SECRET from the environment and
// the stored token from tokenFile.
func NewClientFromEnv(tokenFile string) (*Client, error) {
	cfg, err := OAuthConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return NewClient(&RefreshingTokenProvider{Config: cfg, TokenFile: tokenFile}), nil
}

func (c *Client) newRequest(ctx context.Context, method string, path string, body io.Reader, contentType string) (*http.Request, error) {
	if c.tokenProvider == nil {
		return nil, fmt.Errorf("fortnox token provider is nil")
	}

	token, err := c.tokenProvider.AccessToken(ctx)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	return req, nil
}

// do sends the request through the rate limiter, retrying on HTTP 429. The
// limit is shared per tenant, so another integration (or a previous run) can
// exhaust the window even when this process stays under it.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	const maxRetries = 3
	for attempt := 0; ; attempt++ {
		if err := c.limiter.Wait(req.Context()); err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests || attempt == maxRetries {
			return resp, nil
		}

		wait := retryAfter(resp, attempt)
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()

		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(wait):
		}

		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("rewinding request body for retry: %w", err)
			}
			req.Body = body
		}
	}
}

// retryAfter returns how long to wait before retrying a 429 response,
// honouring the Retry-After header when present and backing off otherwise.
func retryAfter(resp *http.Response, attempt int) time.Duration {
	if s := resp.Header.Get("Retry-After"); s != "" {
		if secs, err := strconv.Atoi(s); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return time.Duration(attempt+1) * 2 * time.Second
}

func (c *Client) uploadMultipart(ctx context.Context, path, filename string, data []byte, out any) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		return fmt.Errorf("creating multipart field: %w", err)
	}

	if _, err := fw.Write(data); err != nil {
		return fmt.Errorf("writing multipart data: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := c.newRequest(ctx, http.MethodPost, path, &body, w.FormDataContentType())
	if err != nil {
		return err
	}

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return decodeResponse(resp, out)
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := c.newRequest(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeResponse(resp, out)
}

func (c *Client) postJSON(ctx context.Context, path string, in any, out any) error {
	payload, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("marshalling Fortnox request JSON: %w", err)
	}

	req, err := c.newRequest(ctx, http.MethodPost, path, bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return decodeResponse(resp, out)
}
