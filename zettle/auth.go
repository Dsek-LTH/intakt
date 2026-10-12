package zettle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
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

// tokenSource hands out an access token and fetches a new one when the current
// one is about to expire. Zettle's tokens last a couple of hours, which a CLI
// run never notices but a long-running server does: holding the first token
// forever makes every request after it expires fail with 401.
type tokenSource struct {
	clientId  string
	assertion string

	mu      sync.Mutex
	token   string
	expires time.Time
}

// tokenExpiryMargin renews a token this long before Zettle says it expires, so
// a request is never sent with one that runs out in flight.
const tokenExpiryMargin = time.Minute

func (s *tokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.token != "" && time.Now().Before(s.expires) {
		return s.token, nil
	}

	tok, err := fetchAccessToken(ctx, s.clientId, s.assertion)
	if err != nil {
		return "", err
	}
	s.token = tok.AccessToken
	s.expires = time.Now().Add(time.Duration(tok.ExpiresIn)*time.Second - tokenExpiryMargin)
	return s.token, nil
}

type bearerAuthTransport struct {
	base     http.RoundTripper
	tokens   *tokenSource
	clientId string
	limiter  *rate.Limiter
}

func (t bearerAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.limiter != nil {
		if err := t.limiter.Wait(req.Context()); err != nil {
			return nil, err
		}
	}
	accessToken, err := t.tokens.Token(req.Context())
	if err != nil {
		return nil, fmt.Errorf("fetching zettle access token: %w", err)
	}
	cloned := req.Clone(req.Context())
	cloned.Header.Set("Authorization", "Bearer "+accessToken)
	cloned.Header.Set("Accept", "application/json")
	cloned.Header.Set("X-iZettle-Application-Id", t.clientId)
	return t.base.RoundTrip(cloned)
}

func fetchAccessToken(ctx context.Context, clientId, assertion string) (token, error) {
	form := url.Values{}
	form.Set("grant_type", zettleAssertionGrantType)
	form.Set("client_id", clientId)
	form.Set("assertion", assertion)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, zettleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return token{}, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-iZettle-Application-Id", clientId)

	resp, err := authHTTPClient.Do(req)
	if err != nil {
		return token{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return token{}, fmt.Errorf("unexpected status %d from token endpoint: %s", resp.StatusCode, body)
	}

	var tok token
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return token{}, err
	}

	if tok.AccessToken == "" {
		return token{}, errors.New("token response did not contain access_token")
	}

	return tok, nil
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

	// Fetched once here so bad credentials fail at startup rather than on the
	// first request. After that the source renews the token as it expires.
	tokens := &tokenSource{clientId: clientId, assertion: assertion}
	if _, err := tokens.Token(ctx); err != nil {
		return nil, err
	}

	return &Client{
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: bearerAuthTransport{
				base:     http.DefaultTransport,
				tokens:   tokens,
				clientId: clientId,
				limiter:  zettleRate,
			},
		},
	}, nil
}
