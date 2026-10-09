// Package api exposes the todo application over HTTP.
package api

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/praneeth-instruqt/coverage-demo/internal/auth"
	"github.com/praneeth-instruqt/coverage-demo/internal/store"
)

// Server holds the dependencies shared by all handlers.
type Server struct {
	store  store.Store
	tokens *auth.TokenManager
	hasher auth.PasswordHasher
	logger *slog.Logger

	// dummyHash is verified against when a login email is unknown, so the
	// response time doesn't reveal which emails are registered.
	dummyHash string
}

// NewServer wires a Server. A nil logger discards logs.
func NewServer(st store.Store, tokens *auth.TokenManager, hasher auth.PasswordHasher, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	dummy, err := hasher.Hash("not-a-real-password")
	if err != nil {
		return nil, fmt.Errorf("prepare dummy hash: %w", err)
	}
	return &Server{store: st, tokens: tokens, hasher: hasher, logger: logger, dummyHash: dummy}, nil
}

// Handler returns the fully wired HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)

	mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("GET /api/v1/auth/me", s.requireAuth(s.handleMe))

	mux.HandleFunc("GET /api/v1/todos", s.requireAuth(s.handleListTodos))
	mux.HandleFunc("POST /api/v1/todos", s.requireAuth(s.handleCreateTodo))
	mux.HandleFunc("DELETE /api/v1/todos/completed", s.requireAuth(s.handleDeleteCompleted))
	mux.HandleFunc("GET /api/v1/todos/{id}", s.requireAuth(s.handleGetTodo))
	mux.HandleFunc("PATCH /api/v1/todos/{id}", s.requireAuth(s.handleUpdateTodo))
	mux.HandleFunc("DELETE /api/v1/todos/{id}", s.requireAuth(s.handleDeleteTodo))

	return withRequestID(s.withLogging(s.withRecover(mux)))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("internal error", "err", err, "request_id", requestIDFrom(r.Context()))
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}
