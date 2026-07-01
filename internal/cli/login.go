package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"intakt/fortnox"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// openBrowser tries to open url in the user's default browser. It returns an
// error if the platform is unsupported or the command fails to start; callers
// should fall back to printing the URL.
func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler"}
	default: // linux, bsd, etc.
		cmd = "xdg-open"
	}

	args = append(args, url)
	launcher := exec.Command(cmd, args...)
	if err := launcher.Start(); err != nil {
		return err
	}
	// The launcher exits as soon as it has handed the URL to the browser; reap
	// it so it doesn't linger as a zombie while we wait for the callback.
	go func() { _ = launcher.Wait() }()
	return nil
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

	authURL := cfg.AuthURL(state)
	if err := openBrowser(authURL); err != nil {
		fmt.Printf("\nOpen this URL in your browser to authenticate with Fortnox:\n\n  %s\n\n", authURL)
	} else {
		fmt.Printf("\nOpening your browser to authenticate with Fortnox. If it doesn't open, visit:\n\n  %s\n\n", authURL)
	}

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

	box := startBox("Fortnox login")

	var code string
	err = box.run(fmt.Sprintf("Waiting for callback on port %d", port), true, func() (string, error) {
		select {
		case code = <-codeCh:
			return "", nil
		case e := <-errCh:
			return "", e
		case <-ctx.Done():
			return "", fmt.Errorf("login cancelled")
		}
	})

	var tok *fortnox.OAuthTokenResponse
	if err == nil {
		err = box.run("Exchanging authorization code", false, func() (string, error) {
			var e error
			tok, e = cfg.ExchangeCode(ctx, code)
			if e != nil {
				return "", fmt.Errorf("exchanging authorization code: %w", e)
			}
			return "", nil
		})
	}
	if err == nil {
		err = box.run("Saving token", false, func() (string, error) {
			return "", fortnox.SaveToken(tokenFile, &fortnox.StoredToken{
				AccessToken:  tok.AccessToken,
				RefreshToken: tok.RefreshToken,
				ExpiresAt:    time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
			})
		})
	}
	box.finish()
	if err != nil {
		return err
	}

	fmt.Printf("\nToken saved to %s\n\n", tokenFile)
	return nil
}
