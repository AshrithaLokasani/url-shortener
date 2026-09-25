package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/url-shortener/url-shortener/internal/link"
)

// Server is the HTTP transport for the link service.
type Server struct {
	svc     *link.Service
	logger  *slog.Logger
	mux     *http.ServeMux
	server  *http.Server
}

// New constructs an HTTP server bound to addr.
func New(svc *link.Service, logger *slog.Logger, addr string) *Server {
	s := &Server{
		svc:    svc,
		logger: logger,
		mux:    http.NewServeMux(),
	}
	s.routes()
	s.server = &http.Server{
		Addr:              addr,
		Handler:           s.withMiddleware(s.mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return s
}

// Handler exposes the wrapped mux for tests.
func (s *Server) Handler() http.Handler {
	return s.withMiddleware(s.mux)
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	return s.server.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("POST /api/v1/links", s.handleCreate)
	s.mux.HandleFunc("GET /api/v1/links/{code}", s.handleMetadata)
	s.mux.HandleFunc("PATCH /api/v1/links/{code}", s.handleDeactivate)
	s.mux.HandleFunc("GET /{code}", s.handleRedirect)
}
