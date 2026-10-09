// Package store provides persistence for users and todos.
package store

import (
	"context"

	"github.com/praneeth-instruqt/coverage-demo/internal/auth"
	"github.com/praneeth-instruqt/coverage-demo/internal/todo"
)

// ListFilter narrows and paginates a ListTodos call.
type ListFilter struct {
	Completed *bool
	Priority  *todo.Priority
	Search    string // case-insensitive substring match on title and description
	Limit     int    // 0 means no limit
	Offset    int
}

// UserStore persists user accounts.
type UserStore interface {
	// CreateUser returns auth.ErrEmailTaken if the email is already registered.
	CreateUser(ctx context.Context, email, passwordHash string) (auth.User, error)
	GetUserByEmail(ctx context.Context, email string) (auth.User, error)
	GetUserByID(ctx context.Context, id string) (auth.User, error)
}

// TodoStore persists todos. Every operation is scoped to the owning user;
// a todo belonging to another user is reported as todo.ErrNotFound.
type TodoStore interface {
	CreateTodo(ctx context.Context, userID string, in todo.CreateInput) (todo.Todo, error)
	GetTodo(ctx context.Context, userID, id string) (todo.Todo, error)
	// ListTodos returns the requested page and the total count before pagination.
	ListTodos(ctx context.Context, userID string, f ListFilter) ([]todo.Todo, int, error)
	UpdateTodo(ctx context.Context, userID, id string, in todo.UpdateInput) (todo.Todo, error)
	DeleteTodo(ctx context.Context, userID, id string) error
	// DeleteCompletedTodos removes the user's completed todos and returns how many were removed.
	DeleteCompletedTodos(ctx context.Context, userID string) (int, error)
}

// Store combines all persistence needed by the API.
type Store interface {
	UserStore
	TodoStore
}
