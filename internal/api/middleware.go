package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/praneeth-instruqt/coverage-demo/internal/auth"
)

type ctxKey int

const (
	userIDKey ctxKey = iota
	requestIDKey
)

const requestIDHeader = "X-Request-ID"

// userIDFrom returns the authenticated user's ID set by requireAuth.
func userIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(userIDKey).(string)
	return id
}

// requestIDFrom returns the request ID set by withRequestID.
func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// withRequestID propagates the caller's X-Request-ID or generates a new one.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if id == "" || len(id) > 128 {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

// statusRecorder captures the status code written by a handler.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// withLogging logs one line per request.
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start),
			"request_id", requestIDFrom(r.Context()),
		)
	})
}

// withRecover turns panics into 500 responses instead of dropping the connection.
func (s *Server) withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}
				s.logger.Error("panic recovered", "panic", v, "request_id", requestIDFrom(r.Context()))
				writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// requireAuth validates the Bearer token and stores the user ID in the context.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="todo"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing or malformed bearer token")
			return
		}
		claims, err := s.tokens.Parse(token)
		if err != nil {
			msg := "invalid token"
			if errors.Is(err, auth.ErrTokenExpired) {
				msg = "token has expired"
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="todo", error="invalid_token"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", msg)
			return
		}
		// Ensure the account still exists.
		if _, err := s.store.GetUserByID(r.Context(), claims.Subject); err != nil {
			if errors.Is(err, auth.ErrUserNotFound) {
				writeError(w, http.StatusUnauthorized, "unauthorized", "invalid token")
				return
			}
			s.internalError(w, r, err)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userIDKey, claims.Subject)))
	}
}
