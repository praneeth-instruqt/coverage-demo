package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/praneeth-instruqt/coverage-demo/internal/auth"
	"github.com/praneeth-instruqt/coverage-demo/internal/todo"
)

// Memory is a thread-safe in-memory Store.
type Memory struct {
	mu           sync.RWMutex
	users        map[string]auth.User // keyed by ID
	userIDByMail map[string]string
	todos        map[string]todo.Todo // keyed by ID
	now          func() time.Time
	newID        func() string
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		users:        make(map[string]auth.User),
		userIDByMail: make(map[string]string),
		todos:        make(map[string]todo.Todo),
		now:          func() time.Time { return time.Now().UTC() },
		newID:        randomID,
	}
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error
	return hex.EncodeToString(b)
}

// --- users ---

func (m *Memory) CreateUser(_ context.Context, email, passwordHash string) (auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.userIDByMail[email]; ok {
		return auth.User{}, auth.ErrEmailTaken
	}
	u := auth.User{ID: m.newID(), Email: email, PasswordHash: passwordHash, CreatedAt: m.now()}
	m.users[u.ID] = u
	m.userIDByMail[email] = u.ID
	return u, nil
}

func (m *Memory) GetUserByEmail(_ context.Context, email string) (auth.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.userIDByMail[email]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return m.users[id], nil
}

func (m *Memory) GetUserByID(_ context.Context, id string) (auth.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[id]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return u, nil
}

// --- todos ---

func (m *Memory) CreateTodo(_ context.Context, userID string, in todo.CreateInput) (todo.Todo, error) {
	now := m.now()
	t := todo.Todo{
		UserID:      userID,
		Title:       in.Title,
		Description: in.Description,
		Priority:    in.Priority,
		DueDate:     in.DueDate,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	t.ID = m.newID()
	m.todos[t.ID] = t
	return t, nil
}

func (m *Memory) GetTodo(_ context.Context, userID, id string) (todo.Todo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ownedTodo(userID, id)
}

// ownedTodo must be called with m.mu held.
func (m *Memory) ownedTodo(userID, id string) (todo.Todo, error) {
	t, ok := m.todos[id]
	if !ok || t.UserID != userID {
		return todo.Todo{}, todo.ErrNotFound
	}
	return t, nil
}

func (m *Memory) ListTodos(_ context.Context, userID string, f ListFilter) ([]todo.Todo, int, error) {
	search := strings.ToLower(f.Search)

	m.mu.RLock()
	matched := make([]todo.Todo, 0)
	for _, t := range m.todos {
		if t.UserID != userID {
			continue
		}
		if f.Completed != nil && t.Completed != *f.Completed {
			continue
		}
		if f.Priority != nil && t.Priority != *f.Priority {
			continue
		}
		if search != "" &&
			!strings.Contains(strings.ToLower(t.Title), search) &&
			!strings.Contains(strings.ToLower(t.Description), search) {
			continue
		}
		matched = append(matched, t)
	}
	m.mu.RUnlock()

	// Newest first; ID breaks ties so ordering is deterministic.
	slices.SortFunc(matched, func(a, b todo.Todo) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})

	total := len(matched)
	start := min(max(f.Offset, 0), total)
	end := total
	if f.Limit > 0 {
		end = min(start+f.Limit, total)
	}
	return matched[start:end], total, nil
}

func (m *Memory) UpdateTodo(_ context.Context, userID, id string, in todo.UpdateInput) (todo.Todo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.ownedTodo(userID, id)
	if err != nil {
		return todo.Todo{}, err
	}
	in.Apply(&t, m.now())
	m.todos[id] = t
	return t, nil
}

func (m *Memory) DeleteTodo(_ context.Context, userID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.ownedTodo(userID, id); err != nil {
		return err
	}
	delete(m.todos, id)
	return nil
}

func (m *Memory) DeleteCompletedTodos(_ context.Context, userID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id, t := range m.todos {
		if t.UserID == userID && t.Completed {
			delete(m.todos, id)
			n++
		}
	}
	return n, nil
}
