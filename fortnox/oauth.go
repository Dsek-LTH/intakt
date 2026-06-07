package fortnox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var oauthHTTPClient = &http.Client{Timeout: 30 * time.Second}

const (
	fortnoxAuthURL  = "https://apps.fortnox.se/oauth-v1/auth"
	fortnoxTokenURL = "https://apps.fortnox.se/oauth-v1/token"
)

type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Scopes       []string
}

type OAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

func (c *OAuthConfig) AuthURL(state string) string {
	params := url.Values{}
	params.Set("client_id", c.ClientID)
	params.Set("redirect_uri", c.RedirectURI)
	params.Set("scope", strings.Join(c.Scopes, " "))
	params.Set("state", state)
	params.Set("response_type", "code")
	params.Set("access_type", "offline")

	return fortnoxAuthURL + "?" + params.Encode()
}

func (c *OAuthConfig) ExchangeCode(ctx context.Context, code string) (*OAuthTokenResponse, error) {
	return c.tokenRequest(ctx, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {c.RedirectURI},
	})
}

func (c *OAuthConfig) Refresh(ctx context.Context, refreshToken string) (*OAuthTokenResponse, error) {
	return c.tokenRequest(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
}

func (c *OAuthConfig) tokenRequest(ctx context.Context, params url.Values) (*OAuthTokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fortnoxTokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.ClientID, c.ClientSecret)

	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("Fortnox token endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var tok OAuthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return nil, fmt.Errorf("decoding Fortnox token response: %w", err)
	}

	return &tok, nil
}
