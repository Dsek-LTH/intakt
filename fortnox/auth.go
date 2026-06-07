package fortnox

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

type TokenProvider interface {
	AccessToken(ctx context.Context) (string, error)
}

type StaticTokenProvider struct {
	Token string
}

func (p StaticTokenProvider) AccessToken(ctx context.Context) (string, error) {
	token := strings.TrimSpace(p.Token)
	if token == "" {
		return "", fmt.Errorf("fortnox access token is empty")
	}

	return token, nil
}

type EnvTokenProvider struct {
	Name string
}

func (p EnvTokenProvider) AccessToken(ctx context.Context) (string, error) {
	if strings.TrimSpace(p.Name) == "" {
		return "", fmt.Errorf("fortnox access token env var name is empty")
	}

	token := strings.TrimSpace(os.Getenv(p.Name))
	if token == "" {
		return "", fmt.Errorf("fortnox access token env var %s is empty", p.Name)
	}

	return token, nil
}

// RefreshingTokenProvider loads a stored token from disk and refreshes it
// automatically when it is about to expire. Fortnox rotates refresh tokens on
// each use, so the new token is written back to TokenFile after every refresh.
type RefreshingTokenProvider struct {
	Config    *OAuthConfig
	TokenFile string
}

func (p *RefreshingTokenProvider) AccessToken(ctx context.Context) (string, error) {
	tok, err := LoadToken(p.TokenFile)
	if err != nil {
		return "", fmt.Errorf("loading Fortnox token from %s: %w — run intakt login first", p.TokenFile, err)
	}

	if time.Now().Add(30 * time.Second).Before(tok.ExpiresAt) {
		return tok.AccessToken, nil
	}

	resp, err := p.Config.Refresh(ctx, tok.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("refreshing Fortnox token: %w", err)
	}

	newTok := &StoredToken{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second),
	}

	if err := SaveToken(p.TokenFile, newTok); err != nil {
		return "", fmt.Errorf("saving refreshed Fortnox token: %w", err)
	}

	return newTok.AccessToken, nil
}
