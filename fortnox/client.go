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
	"time"

	"golang.org/x/time/rate"
)

const DefaultBaseURL = "https://api.fortnox.se/3"

// fortnoxRate is 25 requests per 5-second window.
var fortnoxRate = rate.NewLimiter(rate.Every(5*time.Second/25), 25)

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

// NewClientFromEnv builds a Client authenticated with a RefreshingTokenProvider,
// reading FORTNOX_CLIENT_ID and FORTNOX_CLIENT_SECRET from the environment and
// the stored token from tokenFile.
func NewClientFromEnv(tokenFile string) (*Client, error) {
	clientID := os.Getenv("FORTNOX_CLIENT_ID")
	clientSecret := os.Getenv("FORTNOX_CLIENT_SECRET")
	if clientID == "" {
		return nil, fmt.Errorf("missing env var FORTNOX_CLIENT_ID")
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("missing env var FORTNOX_CLIENT_SECRET")
	}

	return NewClient(&RefreshingTokenProvider{
		Config:    &OAuthConfig{ClientID: clientID, ClientSecret: clientSecret},
		TokenFile: tokenFile,
	}), nil
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

func (c *Client) do(req *http.Request) (*http.Response, error) {
	if err := c.limiter.Wait(req.Context()); err != nil {
		return nil, err
	}
	return c.httpClient.Do(req)
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
