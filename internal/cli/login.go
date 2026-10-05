package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"intakt/fortnox"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"
)

// fortnoxScopes are the Fortnox API permissions intakt needs: creating
// vouchers, uploading receipts to the inbox, and attaching them.
var fortnoxScopes = []string{"bookkeeping", "connectfile", "inbox", "deletevoucher"}

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

// randomState returns an unguessable OAuth state parameter.
func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// oauthCallbackHandler validates the OAuth redirect and delivers the
// authorization code on codeCh, or the reason the consent failed on errCh.
func oauthCallbackHandler(state string, codeCh chan<- string, errCh chan<- error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
	}
}

// announceAuthURL opens the consent page in a browser, falling back to
// printing the URL when that fails.
func announceAuthURL(authURL string) {
	if err := openBrowser(authURL); err != nil {
		fmt.Printf("\nOpen this URL in your browser to authenticate with Fortnox:\n\n  %s\n\n", authURL)
		return
	}
	fmt.Printf("\nOpening your browser to authenticate with Fortnox. If it doesn't open, visit:\n\n  %s\n\n", authURL)
}

// runLoginSteps drives the login pipeline as box rows: wait for the OAuth
// callback, exchange the code, and persist the token to tokenFile.
func runLoginSteps(ctx context.Context, box *liveBox, cfg *fortnox.OAuthConfig, port int,
	codeCh <-chan string, errCh <-chan error, tokenFile string) error {

	var code string
	err := box.run(fmt.Sprintf("Waiting for callback on port %d", port), true, func() (string, error) {
		select {
		case code = <-codeCh:
			return "", nil
		case e := <-errCh:
			return "", e
		case <-ctx.Done():
			return "", fmt.Errorf("login cancelled")
		}
	})
	if err != nil {
		return err
	}

	var tok *fortnox.OAuthTokenResponse
	err = box.run("Exchanging authorization code", false, func() (string, error) {
		var e error
		tok, e = cfg.ExchangeCode(ctx, code)
		if e != nil {
			return "", fmt.Errorf("exchanging authorization code: %w", e)
		}
		return "", nil
	})
	if err != nil {
		return err
	}

	return box.run("Saving token", false, func() (string, error) {
		return "", fortnox.SaveToken(tokenFile, &fortnox.StoredToken{
			AccessToken:  tok.AccessToken,
			RefreshToken: tok.RefreshToken,
			ExpiresAt:    time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
		})
	})
}

func doLogin(ctx context.Context, tokenFile string) error {
	cfg, err := fortnox.OAuthConfigFromEnv()
	if err != nil {
		return err
	}
	cfg.Scopes = fortnoxScopes

	// Keep the listener open so the port cannot be grabbed between finding it
	// and the HTTP server starting.
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return fmt.Errorf("starting callback listener: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	cfg.RedirectURI = fmt.Sprintf("http://localhost:%d/callback", port)

	state, err := randomState()
	if err != nil {
		return err
	}
	announceAuthURL(cfg.AuthURL(state))

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", oauthCallbackHandler(state, codeCh, errCh))
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Shutdown(ctx)

	box := startBox("Fortnox login")
	err = runLoginSteps(ctx, box, cfg, port, codeCh, errCh, tokenFile)
	box.finish()
	if err != nil {
		if box.hasFailure() {
			return reported(err) // the failed step is already shown in the box
		}
		return err
	}

	fmt.Printf("\nToken saved to %s\n\n", tokenFile)
	return nil
}
