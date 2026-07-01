package fortnox

import (
	"context"
	"fmt"
	"time"
)

type TokenProvider interface {
	AccessToken(ctx context.Context) (string, error)
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
