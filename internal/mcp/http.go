package mcp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/alexander-akhmetov/poisk/internal/app"
	"github.com/modelcontextprotocol/go-sdk/auth"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// newHTTPHandler wraps the streamable MCP handler with static bearer-token auth.
func newHTTPHandler(token string, server *gomcp.Server) http.Handler {
	verifier := func(_ context.Context, got string, _ *http.Request) (*auth.TokenInfo, error) {
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			return nil, auth.ErrInvalidToken
		}
		// RequireBearerToken rejects a zero Expiration; static tokens don't
		// expire, so report a synthetic future timestamp.
		return &auth.TokenInfo{Expiration: time.Now().Add(time.Hour)}, nil
	}

	// Stateless mode: poisk tools are pure request/response, and stateful
	// sessions are never reaped when clients disconnect without a DELETE.
	handler := gomcp.NewStreamableHTTPHandler(
		func(*http.Request) *gomcp.Server { return server },
		&gomcp.StreamableHTTPOptions{Stateless: true},
	)
	return auth.RequireBearerToken(verifier, nil)(handler)
}

// RunHTTP serves MCP over Streamable HTTP on addr, requiring the bearer token
// on every request. It blocks until ctx is cancelled, then shuts down
// gracefully with a bounded grace period for in-flight requests.
func RunHTTP(ctx context.Context, addr, token string, indexSvc *app.IndexService, searchSvc *app.SearchService, docSvc *app.DocumentService, statusSvc *app.StatusService) error {
	if token == "" {
		return fmt.Errorf("HTTP mode requires a token: set server.token in config or POISK_SERVER_TOKEN")
	}
	if addr == "" {
		return fmt.Errorf("HTTP mode requires a listen address: set server.listen or --listen")
	}

	server := newServer(indexSvc, searchSvc, docSvc, statusSvc)
	// No ReadTimeout/WriteTimeout: streamable HTTP sessions hold long-lived
	// SSE streams that a global timeout would kill.
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           newHTTPHandler(token, server),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
