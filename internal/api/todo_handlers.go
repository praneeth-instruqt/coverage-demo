package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/praneeth-instruqt/coverage-demo/internal/store"
	"github.com/praneeth-instruqt/coverage-demo/internal/todo"
)

const (
	defaultPageLimit = 20
	maxPageLimit     = 100
)

type listResponse struct {
	Items  []todo.Todo `json:"items"`
	Total  int         `json:"total"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
}

func (s *Server) handleCreateTodo(w http.ResponseWriter, r *http.Request) {
	var in todo.CreateInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
		return
	}
	t, err := s.store.CreateTodo(r.Context(), userIDFrom(r.Context()), in)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/todos/"+t.ID)
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleListTodos(w http.ResponseWriter, r *http.Request) {
	f, err := parseListFilter(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	items, total, err := s.store.ListTodos(r.Context(), userIDFrom(r.Context()), f)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse{Items: items, Total: total, Limit: f.Limit, Offset: f.Offset})
}

func (s *Server) handleGetTodo(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTodo(r.Context(), userIDFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		s.todoError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleUpdateTodo(w http.ResponseWriter, r *http.Request) {
	var in todo.UpdateInput
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	in.Normalize()
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
		return
	}
	t, err := s.store.UpdateTodo(r.Context(), userIDFrom(r.Context()), r.PathValue("id"), in)
	if err != nil {
		s.todoError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleDeleteTodo(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTodo(r.Context(), userIDFrom(r.Context()), r.PathValue("id")); err != nil {
		s.todoError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteCompleted(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.DeleteCompletedTodos(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"deleted": n})
}

func (s *Server) todoError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, todo.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	s.internalError(w, r, err)
}

func parseListFilter(q url.Values) (store.ListFilter, error) {
	f := store.ListFilter{Limit: defaultPageLimit, Search: q.Get("q")}

	if v := q.Get("completed"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return f, errors.New("completed must be true or false")
		}
		f.Completed = &b
	}
	if v := q.Get("priority"); v != "" {
		p := todo.Priority(v)
		if !p.Valid() {
			return f, todo.ErrInvalidPriority
		}
		f.Priority = &p
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPageLimit {
			return f, fmt.Errorf("limit must be an integer between 1 and %d", maxPageLimit)
		}
		f.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return f, errors.New("offset must be a non-negative integer")
		}
		f.Offset = n
	}
	return f, nil
}
