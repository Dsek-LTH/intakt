package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"intakt/server"
	"intakt/zettle"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// ServeCmd runs intakt as an HTTP service instead of a command.
//
// The other commands here are one person's session: fetch a month, look at what
// came back, upload it. This is the same pipeline with utlagg in that seat —
// the app holds the routing table, reviews what was fetched and books it, and
// calls in for the two things that are genuinely intakt's: reaching Zettle or
// reading a Swish export, and turning a day into a PDF.
//
// It writes nothing. No cache, no output directory, no pending vouchers, no
// Fortnox: a request carries its own configuration and the answer goes back in
// the response. See server/api.go for why that division is where it is.
func ServeCmd(ctx context.Context, argv []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", envOr("INTAKT_ADDR", ":8080"), "address to listen on")
	assetsDir := fs.String("assets", envOr("INTAKT_ASSETS", "assets"), "assets directory")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: intakt serve [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Environment:\n")
		fmt.Fprintf(os.Stderr, "  INTAKT_TOKEN       shared secret the caller presents as a bearer token (required)\n")
		fmt.Fprintf(os.Stderr, "  ZETTLE_CLIENT_ID   Zettle application id  (optional; needed for the zettle endpoints)\n")
		fmt.Fprintf(os.Stderr, "  ZETTLE_API_KEY     Zettle API key         (optional; needed for the zettle endpoints)\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(argv); err != nil {
		return err
	}

	token := os.Getenv("INTAKT_TOKEN")
	if token == "" {
		return errors.New("missing env var INTAKT_TOKEN — the service will not start without an auth token")
	}

	// Zettle is optional. A deployment that only imports Swish exports has no
	// reason to hold Zettle credentials, and refusing to start without them
	// would make the render and Swish endpoints hostage to an integration they
	// do not use. The Zettle endpoints report this themselves.
	var zettleClient server.ZettleClient
	if client, err := zettle.NewClientFromEnv(ctx); err != nil {
		log.Printf("zettle is not configured (%v); the zettle endpoints will report themselves unavailable", err)
	} else {
		zettleClient = client
	}

	srv, err := server.New(server.Options{
		AssetsDir: *assetsDir,
		Token:     token,
		Zettle:    zettleClient,
	})
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:    *addr,
		Handler: srv.Handler(),
		// A render is capped at 60s and may queue behind others, so the write
		// timeout has to clear that with room to spare or a slow render is
		// killed mid-response.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	// Stop accepting, let what is in flight finish. A render interrupted
	// halfway is a request the caller has to repeat, and it is holding a
	// subprocess and a temp directory when it dies.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s (assets: %s)", *addr, *assetsDir)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Print("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
