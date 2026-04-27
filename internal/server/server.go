// Package server hosts the HTTP API for the claudewatch daemon.
package server

import (
	"net/http"
	"time"

	"github.com/cfstout/claudewatch/internal/state"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Notifier dispatches an OS notification for a freshly-pending session.
// Implementations must be non-blocking (handler returns must not wait on
// process exec).
type Notifier interface {
	Fire(s state.SessionState)
}

// noopNotifier is the default when no notifier is wired in.
type noopNotifier struct{}

func (noopNotifier) Fire(state.SessionState) {}

// Server bundles the dependencies the HTTP handlers need.
type Server struct {
	Store          *state.Store
	Notifier       Notifier
	DefaultSnooze  time.Duration
}

// New returns a Server with sensible defaults (noop notifier, 10m snooze).
func New(store *state.Store) *Server {
	return &Server{
		Store:         store,
		Notifier:      noopNotifier{},
		DefaultSnooze: 10 * time.Minute,
	}
}

// Router builds the chi router. Caller mounts it on http.ListenAndServe.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/healthz", s.handleHealthz)
	r.Post("/notify", s.handleNotify)

	r.Route("/sessions", func(r chi.Router) {
		r.Get("/", s.handleListSessions)
		r.Post("/", s.handleRegisterSession)
		r.Get("/{name}", s.handleGetSession)
		r.Delete("/{name}", s.handleDeleteSession)
		r.Post("/{name}/clear", s.handleClearSession)
		r.Post("/{name}/snooze", s.handleSnoozeSession)
	})

	r.Get("/pending/oldest", s.handlePendingOldest)
	r.Get("/summary", s.handleSummary)
	return r
}
