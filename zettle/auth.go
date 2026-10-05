package zettle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

var authHTTPClient = &http.Client{Timeout: 30 * time.Second}

// zettleRate throttles all API requests to keep the sharded, concurrent fetch
// well inside Zettle's limits: ~10 requests/second with a small burst.
var zettleRate = rate.NewLimiter(rate.Limit(10), 10)

const (
	zettleTokenURL = "https://oauth.zettle.com/token"

	zettleAssertionGrantType = "urn:ietf:params:oauth:grant-type:jwt-bearer"
)

type token struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// Client is an authenticated Zettle HTTP client.
type Client struct {
	http *http.Client
}

type bearerAuthTransport struct {
	base     http.RoundTripper
	token    string
	clientId string
	limiter  *rate.Limiter
}

func (t bearerAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.limiter != nil {
		if err := t.limiter.Wait(req.Context()); err != nil {
			return nil, err
		}
	}
	cloned := req.Clone(req.Context())
	cloned.Header.Set("Authorization", "Bearer "+t.token)
	cloned.Header.Set("Accept", "application/json")
	cloned.Header.Set("X-iZettle-Application-Id", t.clientId)
	return t.base.RoundTrip(cloned)
}

func fetchAccessToken(ctx context.Context, clientId, assertion string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", zettleAssertionGrantType)
	form.Set("client_id", clientId)
	form.Set("assertion", assertion)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, zettleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-iZettle-Application-Id", clientId)

	resp, err := authHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var tok token
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", err
	}

	if tok.AccessToken == "" {
		return "", errors.New("token response did not contain access_token")
	}

	return tok.AccessToken, nil
}

// NewClientFromEnv reads ZETTLE_CLIENT_ID and ZETTLE_API_KEY from the
// environment and returns an authenticated Client.
func NewClientFromEnv(ctx context.Context) (*Client, error) {
	clientId := os.Getenv("ZETTLE_CLIENT_ID")
	assertion := os.Getenv("ZETTLE_API_KEY")

	if clientId == "" {
		return nil, errors.New("missing env var ZETTLE_CLIENT_ID")
	}
	if assertion == "" {
		return nil, errors.New("missing env var ZETTLE_API_KEY")
	}

	accessToken, err := fetchAccessToken(ctx, clientId, assertion)
	if err != nil {
		return nil, err
	}

	return &Client{
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: bearerAuthTransport{
				base:     http.DefaultTransport,
				token:    accessToken,
				clientId: clientId,
				limiter:  zettleRate,
			},
		},
	}, nil
}
