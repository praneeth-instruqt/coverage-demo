package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/praneeth-instruqt/coverage-demo/internal/auth"
	"github.com/praneeth-instruqt/coverage-demo/internal/todo"
)

func ptr[T any](v T) *T { return &v }

// newTestMemory returns a store with a controllable clock that advances one
// second per call, so creation order is deterministic.
func newTestMemory() *Memory {
	m := NewMemory()
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	m.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(time.Second)
		return clock
	}
	return m
}

func mustCreate(t *testing.T, m *Memory, userID string, in todo.CreateInput) todo.Todo {
	t.Helper()
	if in.Priority == "" {
		in.Priority = todo.PriorityMedium
	}
	td, err := m.CreateTodo(context.Background(), userID, in)
	if err != nil {
		t.Fatal(err)
	}
	return td
}

func TestRandomID(t *testing.T) {
	a, b := randomID(), randomID()
	if len(a) != 32 || a == b {
		t.Errorf("ids not unique 32-char hex: %q %q", a, b)
	}
}

func TestUsers(t *testing.T) {
	ctx := context.Background()
	m := newTestMemory()

	u, err := m.CreateUser(ctx, "a@b.co", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if u.ID == "" || u.Email != "a@b.co" || u.PasswordHash != "hash" || u.CreatedAt.IsZero() {
		t.Errorf("unexpected user: %+v", u)
	}

	if _, err := m.CreateUser(ctx, "a@b.co", "other"); !errors.Is(err, auth.ErrEmailTaken) {
		t.Errorf("duplicate email err = %v", err)
	}

	if got, err := m.GetUserByEmail(ctx, "a@b.co"); err != nil || got != u {
		t.Errorf("GetUserByEmail = %+v, %v", got, err)
	}
	if got, err := m.GetUserByID(ctx, u.ID); err != nil || got != u {
		t.Errorf("GetUserByID = %+v, %v", got, err)
	}
	if _, err := m.GetUserByEmail(ctx, "x@y.z"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("unknown email err = %v", err)
	}
	if _, err := m.GetUserByID(ctx, "nope"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("unknown id err = %v", err)
	}
}

func TestTodoCRUD(t *testing.T) {
	ctx := context.Background()
	m := newTestMemory()
	due := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	created := mustCreate(t, m, "u1", todo.CreateInput{Title: "a", Description: "d", Priority: todo.PriorityHigh, DueDate: &due})
	if created.ID == "" || created.UserID != "u1" || created.Completed || !created.CreatedAt.Equal(created.UpdatedAt) {
		t.Errorf("unexpected todo: %+v", created)
	}

	got, err := m.GetTodo(ctx, "u1", created.ID)
	if err != nil || got.ID != created.ID || got.Title != "a" {
		t.Errorf("GetTodo = %+v, %v", got, err)
	}

	updated, err := m.UpdateTodo(ctx, "u1", created.ID, todo.UpdateInput{Completed: ptr(true), Title: ptr("b")})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Completed || updated.Title != "b" || !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("update not applied: %+v", updated)
	}
	if got, _ := m.GetTodo(ctx, "u1", created.ID); got.Title != "b" {
		t.Error("update not persisted")
	}

	if err := m.DeleteTodo(ctx, "u1", created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetTodo(ctx, "u1", created.ID); !errors.Is(err, todo.ErrNotFound) {
		t.Errorf("after delete err = %v", err)
	}
	if err := m.DeleteTodo(ctx, "u1", created.ID); !errors.Is(err, todo.ErrNotFound) {
		t.Errorf("double delete err = %v", err)
	}
	if _, err := m.UpdateTodo(ctx, "u1", "missing", todo.UpdateInput{}); !errors.Is(err, todo.ErrNotFound) {
		t.Errorf("update missing err = %v", err)
	}
}

func TestTodosAreIsolatedPerUser(t *testing.T) {
	ctx := context.Background()
	m := newTestMemory()
	mine := mustCreate(t, m, "alice", todo.CreateInput{Title: "alice's"})
	mustCreate(t, m, "bob", todo.CreateInput{Title: "bob's"})

	if _, err := m.GetTodo(ctx, "bob", mine.ID); !errors.Is(err, todo.ErrNotFound) {
		t.Errorf("bob read alice's todo: %v", err)
	}
	if _, err := m.UpdateTodo(ctx, "bob", mine.ID, todo.UpdateInput{Title: ptr("hacked")}); !errors.Is(err, todo.ErrNotFound) {
		t.Errorf("bob updated alice's todo: %v", err)
	}
	if err := m.DeleteTodo(ctx, "bob", mine.ID); !errors.Is(err, todo.ErrNotFound) {
		t.Errorf("bob deleted alice's todo: %v", err)
	}
	items, total, _ := m.ListTodos(ctx, "bob", ListFilter{})
	if total != 1 || items[0].Title != "bob's" {
		t.Errorf("bob's list = %+v", items)
	}
	if got, _ := m.GetTodo(ctx, "alice", mine.ID); got.Title != "alice's" {
		t.Error("alice's todo was modified")
	}
}

func TestListTodosFiltersAndPagination(t *testing.T) {
	ctx := context.Background()
	m := newTestMemory()

	a := mustCreate(t, m, "u", todo.CreateInput{Title: "Buy milk", Priority: todo.PriorityLow})
	b := mustCreate(t, m, "u", todo.CreateInput{Title: "Write report", Description: "quarterly MILK numbers", Priority: todo.PriorityHigh})
	c := mustCreate(t, m, "u", todo.CreateInput{Title: "Call mom", Priority: todo.PriorityHigh})
	if _, err := m.UpdateTodo(ctx, "u", c.ID, todo.UpdateInput{Completed: ptr(true)}); err != nil {
		t.Fatal(err)
	}

	ids := func(ts []todo.Todo) []string {
		out := make([]string, len(ts))
		for i, t := range ts {
			out[i] = t.ID
		}
		return out
	}

	tests := []struct {
		name      string
		f         ListFilter
		wantIDs   []string
		wantTotal int
	}{
		{"all newest first", ListFilter{}, []string{c.ID, b.ID, a.ID}, 3},
		{"completed", ListFilter{Completed: ptr(true)}, []string{c.ID}, 1},
		{"not completed", ListFilter{Completed: ptr(false)}, []string{b.ID, a.ID}, 2},
		{"priority", ListFilter{Priority: ptr(todo.PriorityHigh)}, []string{c.ID, b.ID}, 2},
		{"search title and description", ListFilter{Search: "milk"}, []string{b.ID, a.ID}, 2},
		{"search no match", ListFilter{Search: "zzz"}, []string{}, 0},
		{"limit", ListFilter{Limit: 2}, []string{c.ID, b.ID}, 3},
		{"offset", ListFilter{Offset: 1, Limit: 1}, []string{b.ID}, 3},
		{"offset past end", ListFilter{Offset: 10}, []string{}, 3},
		{"negative offset", ListFilter{Offset: -1, Limit: 1}, []string{c.ID}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, total, err := m.ListTodos(ctx, "u", tt.f)
			if err != nil {
				t.Fatal(err)
			}
			if total != tt.wantTotal {
				t.Errorf("total = %d, want %d", total, tt.wantTotal)
			}
			if fmt.Sprint(ids(items)) != fmt.Sprint(tt.wantIDs) {
				t.Errorf("ids = %v, want %v", ids(items), tt.wantIDs)
			}
		})
	}
}

func TestListTodosTieBreakByID(t *testing.T) {
	m := NewMemory()
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return fixed }
	n := 0
	m.newID = func() string { n++; return fmt.Sprintf("id-%d", n) }

	for range 3 {
		mustCreate(t, m, "u", todo.CreateInput{Title: "x"})
	}
	items, _, _ := m.ListTodos(context.Background(), "u", ListFilter{})
	if items[0].ID != "id-1" || items[1].ID != "id-2" || items[2].ID != "id-3" {
		t.Errorf("unexpected order: %v %v %v", items[0].ID, items[1].ID, items[2].ID)
	}
}

func TestDeleteCompletedTodos(t *testing.T) {
	ctx := context.Background()
	m := newTestMemory()
	done := mustCreate(t, m, "u", todo.CreateInput{Title: "done"})
	mustCreate(t, m, "u", todo.CreateInput{Title: "open"})
	otherDone := mustCreate(t, m, "other", todo.CreateInput{Title: "other done"})
	for _, td := range []todo.Todo{done, otherDone} {
		if _, err := m.UpdateTodo(ctx, td.UserID, td.ID, todo.UpdateInput{Completed: ptr(true)}); err != nil {
			t.Fatal(err)
		}
	}

	n, err := m.DeleteCompletedTodos(ctx, "u")
	if err != nil || n != 1 {
		t.Fatalf("deleted = %d, %v; want 1", n, err)
	}
	if _, total, _ := m.ListTodos(ctx, "u", ListFilter{}); total != 1 {
		t.Errorf("remaining = %d, want 1", total)
	}
	if _, err := m.GetTodo(ctx, "other", otherDone.ID); err != nil {
		t.Error("another user's completed todo was deleted")
	}
}

func TestConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			td, err := m.CreateTodo(ctx, "u", todo.CreateInput{Title: fmt.Sprint(i), Priority: todo.PriorityLow})
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = m.UpdateTodo(ctx, "u", td.ID, todo.UpdateInput{Completed: ptr(i%2 == 0)})
			_, _, _ = m.ListTodos(ctx, "u", ListFilter{})
		})
	}
	wg.Wait()
	if _, total, _ := m.ListTodos(ctx, "u", ListFilter{}); total != 50 {
		t.Errorf("total = %d, want 50", total)
	}
	if n, _ := m.DeleteCompletedTodos(ctx, "u"); n != 25 {
		t.Errorf("deleted = %d, want 25", n)
	}
}
