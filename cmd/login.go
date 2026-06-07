package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"intakt/fortnox"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func defaultTokenPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ".fortnox_token.json"
	}
	return filepath.Join(dir, "intakt", "fortnox_token.json")
}

func oauthConfigFromEnv() (*fortnox.OAuthConfig, error) {
	clientID := os.Getenv("FORTNOX_CLIENT_ID")
	clientSecret := os.Getenv("FORTNOX_CLIENT_SECRET")

	if clientID == "" {
		return nil, fmt.Errorf("missing env var FORTNOX_CLIENT_ID")
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("missing env var FORTNOX_CLIENT_SECRET")
	}

	return &fortnox.OAuthConfig{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       []string{"bookkeeping", "connectfile", "inbox", "deletevoucher"},
	}, nil
}

func doLogin(ctx context.Context, tokenFile string) error {
	cfg, err := oauthConfigFromEnv()
	if err != nil {
		return err
	}

	// Keep the listener open so the port cannot be grabbed between finding it
	// and the HTTP server starting.
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return fmt.Errorf("starting callback listener: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	cfg.RedirectURI = fmt.Sprintf("http://localhost:%d/callback", port)

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Errorf("generating state: %w", err)
	}
	state := hex.EncodeToString(b)

	fmt.Printf("Open this URL in your browser to authenticate with Fortnox:\n\n  %s\n\nWaiting for callback on port %d...\n", cfg.AuthURL(state), port)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// Fortnox redirects with error/error_description on failure.
		if e := r.URL.Query().Get("error"); e != "" {
			desc := r.URL.Query().Get("error_description")
			msg := fmt.Errorf("Fortnox returned an error: %s — %s", e, desc)
			fmt.Fprintf(w, "Authentication failed: %v", msg)
			errCh <- msg
			return
		}

		if r.URL.Query().Get("state") != state {
			msg := fmt.Errorf("state mismatch in OAuth callback")
			http.Error(w, msg.Error(), http.StatusBadRequest)
			errCh <- msg
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			msg := fmt.Errorf("no authorization code in callback (params: %s)", r.URL.RawQuery)
			http.Error(w, msg.Error(), http.StatusBadRequest)
			errCh <- msg
			return
		}

		fmt.Fprintln(w, "Authentication successful — you can close this tab.")
		codeCh <- code
	})

	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Shutdown(ctx)

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return fmt.Errorf("login cancelled")
	}

	tok, err := cfg.ExchangeCode(ctx, code)
	if err != nil {
		return fmt.Errorf("exchanging authorization code: %w", err)
	}

	if err := fortnox.SaveToken(tokenFile, &fortnox.StoredToken{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
	}); err != nil {
		return fmt.Errorf("saving token: %w", err)
	}

	fmt.Printf("Token saved to %s\n", tokenFile)
	return nil
}
