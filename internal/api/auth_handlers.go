package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/praneeth-instruqt/coverage-demo/internal/auth"
)

type authResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      auth.User `json:"user"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var creds auth.Credentials
	if err := decodeJSON(w, r, &creds); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	creds.Normalize()
	if err := creds.ValidateForRegistration(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
		return
	}

	hash, err := s.hasher.Hash(creds.Password)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	user, err := s.store.CreateUser(r.Context(), creds.Email, hash)
	if errors.Is(err, auth.ErrEmailTaken) {
		writeError(w, http.StatusConflict, "email_taken", err.Error())
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.respondWithToken(w, r, http.StatusCreated, user)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var creds auth.Credentials
	if err := decodeJSON(w, r, &creds); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	creds.Normalize()

	user, err := s.store.GetUserByEmail(r.Context(), creds.Email)
	hash := user.PasswordHash
	switch {
	case errors.Is(err, auth.ErrUserNotFound):
		hash = s.dummyHash
	case err != nil:
		s.internalError(w, r, err)
		return
	}

	ok, verr := s.hasher.Verify(hash, creds.Password)
	if verr != nil {
		s.internalError(w, r, verr)
		return
	}
	if err != nil || !ok {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", auth.ErrInvalidCredentials.Error())
		return
	}
	s.respondWithToken(w, r, http.StatusOK, user)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, err := s.store.GetUserByID(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) respondWithToken(w http.ResponseWriter, r *http.Request, status int, user auth.User) {
	token, exp, err := s.tokens.Issue(user.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, status, authResponse{Token: token, ExpiresAt: exp.UTC(), User: user})
}
