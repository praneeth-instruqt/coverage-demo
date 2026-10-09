package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/praneeth-instruqt/coverage-demo/internal/auth"
	"github.com/praneeth-instruqt/coverage-demo/internal/store"
	"github.com/praneeth-instruqt/coverage-demo/internal/todo"
)

var testSecret = []byte("0123456789abcdef0123456789abcdef")

type testEnv struct {
	t      *testing.T
	srv    *Server
	h      http.Handler
	tokens *auth.TokenManager
}

func newTestEnv(t *testing.T, st store.Store) *testEnv {
	t.Helper()
	if st == nil {
		st = store.NewMemory()
	}
	tokens, err := auth.NewTokenManager(testSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(st, tokens, auth.NewPasswordHasher(1000), nil)
	if err != nil {
		t.Fatal(err)
	}
	return &testEnv{t: t, srv: srv, h: srv.Handler(), tokens: tokens}
}

type response struct {
	*httptest.ResponseRecorder
}

func (r response) decode(v any) {
	if err := json.Unmarshal(r.Body.Bytes(), v); err != nil {
		panic(err)
	}
}

func (r response) errorCode() string {
	var e errorBody
	r.decode(&e)
	return e.Error.Code
}

func (e *testEnv) do(method, path, token string, body any) response {
	e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		buf, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		rd = bytes.NewReader(buf)
	}
	req := httptest.NewRequest(method, path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return response{rec}
}

func (e *testEnv) register(email string) (token string, user auth.User) {
	e.t.Helper()
	res := e.do("POST", "/api/v1/auth/register", "", auth.Credentials{Email: email, Password: "password123"})
	if res.Code != http.StatusCreated {
		e.t.Fatalf("register: %d %s", res.Code, res.Body)
	}
	var ar authResponse
	res.decode(&ar)
	return ar.Token, ar.User
}

func (e *testEnv) createTodo(token string, body any) todo.Todo {
	e.t.Helper()
	res := e.do("POST", "/api/v1/todos", token, body)
	if res.Code != http.StatusCreated {
		e.t.Fatalf("create todo: %d %s", res.Code, res.Body)
	}
	var td todo.Todo
	res.decode(&td)
	return td
}

func expectStatus(t *testing.T, res response, status int, code string) {
	t.Helper()
	if res.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, status, res.Body)
	}
	if code != "" {
		if got := res.errorCode(); got != code {
			t.Errorf("error code = %q, want %q", got, code)
		}
	}
}

// ---------- health / routing ----------

func TestHealth(t *testing.T) {
	e := newTestEnv(t, nil)
	res := e.do("GET", "/healthz", "", nil)
	expectStatus(t, res, http.StatusOK, "")
	if ct := res.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
}

func TestUnknownRoute(t *testing.T) {
	e := newTestEnv(t, nil)
	expectStatus(t, e.do("GET", "/nope", "", nil), http.StatusNotFound, "")
}

func TestMethodNotAllowed(t *testing.T) {
	e := newTestEnv(t, nil)
	res := e.do("PUT", "/api/v1/todos", "", nil)
	if res.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", res.Code)
	}
}

// ---------- auth ----------

func TestRegister(t *testing.T) {
	e := newTestEnv(t, nil)

	res := e.do("POST", "/api/v1/auth/register", "", auth.Credentials{Email: " Alice@Example.com ", Password: "password123"})
	expectStatus(t, res, http.StatusCreated, "")
	var ar authResponse
	res.decode(&ar)
	if ar.Token == "" || ar.User.ID == "" || ar.User.Email != "alice@example.com" || ar.ExpiresAt.IsZero() {
		t.Errorf("unexpected response: %+v", ar)
	}
	if strings.Contains(res.Body.String(), "pbkdf2") || strings.Contains(res.Body.String(), "password") {
		t.Error("response leaks password hash")
	}

	t.Run("duplicate email", func(t *testing.T) {
		res := e.do("POST", "/api/v1/auth/register", "", auth.Credentials{Email: "ALICE@example.com", Password: "password123"})
		expectStatus(t, res, http.StatusConflict, "email_taken")
	})
	t.Run("invalid email", func(t *testing.T) {
		res := e.do("POST", "/api/v1/auth/register", "", auth.Credentials{Email: "nope", Password: "password123"})
		expectStatus(t, res, http.StatusUnprocessableEntity, "validation_failed")
	})
	t.Run("short password", func(t *testing.T) {
		res := e.do("POST", "/api/v1/auth/register", "", auth.Credentials{Email: "b@c.de", Password: "short"})
		expectStatus(t, res, http.StatusUnprocessableEntity, "validation_failed")
	})
	t.Run("bad json", func(t *testing.T) {
		expectStatus(t, e.do("POST", "/api/v1/auth/register", "", "{"), http.StatusBadRequest, "invalid_request")
	})
}

func TestLogin(t *testing.T) {
	e := newTestEnv(t, nil)
	_, user := e.register("bob@example.com")

	t.Run("success", func(t *testing.T) {
		res := e.do("POST", "/api/v1/auth/login", "", auth.Credentials{Email: "BOB@example.com", Password: "password123"})
		expectStatus(t, res, http.StatusOK, "")
		var ar authResponse
		res.decode(&ar)
		if ar.User.ID != user.ID {
			t.Errorf("user id = %q, want %q", ar.User.ID, user.ID)
		}
		if c, err := e.tokens.Parse(ar.Token); err != nil || c.Subject != user.ID {
			t.Errorf("token invalid: %+v %v", c, err)
		}
	})
	t.Run("wrong password", func(t *testing.T) {
		res := e.do("POST", "/api/v1/auth/login", "", auth.Credentials{Email: "bob@example.com", Password: "wrongpass1"})
		expectStatus(t, res, http.StatusUnauthorized, "invalid_credentials")
	})
	t.Run("unknown email gives same error", func(t *testing.T) {
		res := e.do("POST", "/api/v1/auth/login", "", auth.Credentials{Email: "ghost@example.com", Password: "password123"})
		expectStatus(t, res, http.StatusUnauthorized, "invalid_credentials")
	})
	t.Run("bad json", func(t *testing.T) {
		expectStatus(t, e.do("POST", "/api/v1/auth/login", "", ""), http.StatusBadRequest, "invalid_request")
	})
}

func TestMe(t *testing.T) {
	e := newTestEnv(t, nil)
	token, user := e.register("me@example.com")

	res := e.do("GET", "/api/v1/auth/me", token, nil)
	expectStatus(t, res, http.StatusOK, "")
	var got auth.User
	res.decode(&got)
	if got.ID != user.ID || got.Email != "me@example.com" {
		t.Errorf("me = %+v", got)
	}
}

func TestAuthMiddleware(t *testing.T) {
	e := newTestEnv(t, nil)
	valid, _ := e.register("x@example.com")
	ghost, _, _ := e.tokens.Issue("no-such-user")

	expired, _ := auth.NewTokenManager(testSecret, -time.Minute)
	expiredTok, _, _ := expired.Issue("whoever")

	cases := map[string]struct {
		header string
		msg    string
	}{
		"no header":        {"", "missing"},
		"wrong scheme":     {"Basic abc", "missing"},
		"empty token":      {"Bearer ", "missing"},
		"garbage token":    {"Bearer abc.def.ghi", "invalid token"},
		"expired token":    {"Bearer " + expiredTok, "expired"},
		"deleted user":     {"Bearer " + ghost, "invalid token"},
		"lowercase scheme": {"bearer " + valid, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v1/todos", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			e.h.ServeHTTP(rec, req)
			res := response{rec}

			if tc.msg == "" {
				expectStatus(t, res, http.StatusOK, "")
				return
			}
			expectStatus(t, res, http.StatusUnauthorized, "unauthorized")
			if !strings.Contains(res.Body.String(), tc.msg) {
				t.Errorf("body %s does not mention %q", res.Body, tc.msg)
			}
		})
	}
}

func TestAllTodoRoutesRequireAuth(t *testing.T) {
	e := newTestEnv(t, nil)
	for _, r := range []struct{ method, path string }{
		{"GET", "/api/v1/auth/me"},
		{"GET", "/api/v1/todos"},
		{"POST", "/api/v1/todos"},
		{"DELETE", "/api/v1/todos/completed"},
		{"GET", "/api/v1/todos/x"},
		{"PATCH", "/api/v1/todos/x"},
		{"DELETE", "/api/v1/todos/x"},
	} {
		res := e.do(r.method, r.path, "", nil)
		if res.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", r.method, r.path, res.Code)
		}
		if res.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("%s %s missing WWW-Authenticate", r.method, r.path)
		}
	}
}

// ---------- todos CRUD ----------

func TestTodoCRUDFlow(t *testing.T) {
	e := newTestEnv(t, nil)
	token, _ := e.register("crud@example.com")

	// Create
	res := e.do("POST", "/api/v1/todos", token, map[string]any{
		"title": "  Write tests ", "description": "all of them", "priority": "high", "due_date": "2026-12-01T00:00:00Z",
	})
	expectStatus(t, res, http.StatusCreated, "")
	var created todo.Todo
	res.decode(&created)
	if created.Title != "Write tests" || created.Priority != todo.PriorityHigh || created.DueDate == nil || created.Completed {
		t.Errorf("created = %+v", created)
	}
	if loc := res.Header().Get("Location"); loc != "/api/v1/todos/"+created.ID {
		t.Errorf("Location = %q", loc)
	}

	// Read
	res = e.do("GET", "/api/v1/todos/"+created.ID, token, nil)
	expectStatus(t, res, http.StatusOK, "")
	var got todo.Todo
	res.decode(&got)
	if got.ID != created.ID || got.Title != "Write tests" {
		t.Errorf("got = %+v", got)
	}

	// Update
	res = e.do("PATCH", "/api/v1/todos/"+created.ID, token, map[string]any{"completed": true, "clear_due_date": true})
	expectStatus(t, res, http.StatusOK, "")
	var updated todo.Todo
	res.decode(&updated)
	if !updated.Completed || updated.DueDate != nil || updated.Title != "Write tests" {
		t.Errorf("updated = %+v", updated)
	}

	// List
	res = e.do("GET", "/api/v1/todos", token, nil)
	expectStatus(t, res, http.StatusOK, "")
	var list listResponse
	res.decode(&list)
	if list.Total != 1 || len(list.Items) != 1 || list.Limit != defaultPageLimit || list.Offset != 0 {
		t.Errorf("list = %+v", list)
	}

	// Delete
	res = e.do("DELETE", "/api/v1/todos/"+created.ID, token, nil)
	expectStatus(t, res, http.StatusNoContent, "")
	if res.Body.Len() != 0 {
		t.Errorf("204 with body: %s", res.Body)
	}
	expectStatus(t, e.do("GET", "/api/v1/todos/"+created.ID, token, nil), http.StatusNotFound, "not_found")
	expectStatus(t, e.do("DELETE", "/api/v1/todos/"+created.ID, token, nil), http.StatusNotFound, "not_found")
}

func TestCreateTodoValidation(t *testing.T) {
	e := newTestEnv(t, nil)
	token, _ := e.register("v@example.com")

	cases := map[string]struct {
		body   any
		status int
		code   string
	}{
		"default priority": {map[string]any{"title": "x"}, http.StatusCreated, ""},
		"missing title":    {map[string]any{"description": "x"}, http.StatusUnprocessableEntity, "validation_failed"},
		"blank title":      {map[string]any{"title": "   "}, http.StatusUnprocessableEntity, "validation_failed"},
		"bad priority":     {map[string]any{"title": "x", "priority": "urgent"}, http.StatusUnprocessableEntity, "validation_failed"},
		"unknown field":    {map[string]any{"title": "x", "owner": "me"}, http.StatusBadRequest, "invalid_request"},
		"bad due date":     {map[string]any{"title": "x", "due_date": "tomorrow"}, http.StatusBadRequest, "invalid_request"},
		"empty body":       {"", http.StatusBadRequest, "invalid_request"},
		"two objects":      {`{"title":"a"}{"title":"b"}`, http.StatusBadRequest, "invalid_request"},
		"too large":        {`{"title":"` + strings.Repeat("a", maxBodyBytes) + `"}`, http.StatusBadRequest, "invalid_request"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			res := e.do("POST", "/api/v1/todos", token, tc.body)
			expectStatus(t, res, tc.status, tc.code)
			if name == "default priority" {
				var td todo.Todo
				res.decode(&td)
				if td.Priority != todo.PriorityMedium {
					t.Errorf("priority = %q, want medium", td.Priority)
				}
			}
		})
	}
}

func TestUpdateTodoValidation(t *testing.T) {
	e := newTestEnv(t, nil)
	token, _ := e.register("u@example.com")
	td := e.createTodo(token, map[string]any{"title": "x"})

	expectStatus(t, e.do("PATCH", "/api/v1/todos/"+td.ID, token, map[string]any{"title": ""}),
		http.StatusUnprocessableEntity, "validation_failed")
	expectStatus(t, e.do("PATCH", "/api/v1/todos/"+td.ID, token, "not json"),
		http.StatusBadRequest, "invalid_request")
	expectStatus(t, e.do("PATCH", "/api/v1/todos/missing", token, map[string]any{"completed": true}),
		http.StatusNotFound, "not_found")
}

func TestListTodosQuery(t *testing.T) {
	e := newTestEnv(t, nil)
	token, _ := e.register("l@example.com")
	for _, title := range []string{"alpha", "beta", "gamma"} {
		e.createTodo(token, map[string]any{"title": title, "priority": "low"})
	}
	hi := e.createTodo(token, map[string]any{"title": "delta", "priority": "high"})
	e.do("PATCH", "/api/v1/todos/"+hi.ID, token, map[string]any{"completed": true})

	list := func(q string) listResponse {
		t.Helper()
		res := e.do("GET", "/api/v1/todos"+q, token, nil)
		expectStatus(t, res, http.StatusOK, "")
		var l listResponse
		res.decode(&l)
		return l
	}

	if l := list("?completed=true"); l.Total != 1 || l.Items[0].ID != hi.ID {
		t.Errorf("completed filter: %+v", l)
	}
	if l := list("?completed=false&priority=low"); l.Total != 3 {
		t.Errorf("combined filter total = %d", l.Total)
	}
	if l := list("?q=ALP"); l.Total != 1 || l.Items[0].Title != "alpha" {
		t.Errorf("search: %+v", l)
	}
	if l := list("?limit=2&offset=1"); l.Total != 4 || len(l.Items) != 2 || l.Limit != 2 || l.Offset != 1 {
		t.Errorf("pagination: %+v", l)
	}
	if l := list("?q=zzz"); l.Items == nil || len(l.Items) != 0 {
		t.Errorf("empty result should be [] not null: %s", e.do("GET", "/api/v1/todos?q=zzz", token, nil).Body)
	}

	for _, q := range []string{"?completed=maybe", "?priority=urgent", "?limit=0", "?limit=101", "?limit=x", "?offset=-1", "?offset=x"} {
		expectStatus(t, e.do("GET", "/api/v1/todos"+q, token, nil), http.StatusBadRequest, "invalid_query")
	}
}

func TestDeleteCompleted(t *testing.T) {
	e := newTestEnv(t, nil)
	token, _ := e.register("dc@example.com")
	a := e.createTodo(token, map[string]any{"title": "a"})
	e.createTodo(token, map[string]any{"title": "b"})
	e.do("PATCH", "/api/v1/todos/"+a.ID, token, map[string]any{"completed": true})

	res := e.do("DELETE", "/api/v1/todos/completed", token, nil)
	expectStatus(t, res, http.StatusOK, "")
	var out map[string]int
	res.decode(&out)
	if out["deleted"] != 1 {
		t.Errorf("deleted = %d, want 1", out["deleted"])
	}
}

func TestUsersCannotAccessEachOthersTodos(t *testing.T) {
	e := newTestEnv(t, nil)
	alice, _ := e.register("alice@example.com")
	bob, _ := e.register("bob@example.com")
	td := e.createTodo(alice, map[string]any{"title": "private"})

	expectStatus(t, e.do("GET", "/api/v1/todos/"+td.ID, bob, nil), http.StatusNotFound, "not_found")
	expectStatus(t, e.do("PATCH", "/api/v1/todos/"+td.ID, bob, map[string]any{"title": "pwned"}), http.StatusNotFound, "not_found")
	expectStatus(t, e.do("DELETE", "/api/v1/todos/"+td.ID, bob, nil), http.StatusNotFound, "not_found")

	var l listResponse
	e.do("GET", "/api/v1/todos", bob, nil).decode(&l)
	if l.Total != 0 {
		t.Errorf("bob sees %d todos", l.Total)
	}
	var got todo.Todo
	e.do("GET", "/api/v1/todos/"+td.ID, alice, nil).decode(&got)
	if got.Title != "private" {
		t.Errorf("alice's todo changed: %+v", got)
	}
}

// ---------- middleware ----------

func TestRequestID(t *testing.T) {
	e := newTestEnv(t, nil)

	res := e.do("GET", "/healthz", "", nil)
	if id := res.Header().Get(requestIDHeader); len(id) != 16 {
		t.Errorf("generated request id = %q", id)
	}

	req := httptest.NewRequest("GET", "/healthz", nil)
	req.Header.Set(requestIDHeader, "abc-123")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if id := rec.Header().Get(requestIDHeader); id != "abc-123" {
		t.Errorf("propagated request id = %q", id)
	}

	req = httptest.NewRequest("GET", "/healthz", nil)
	req.Header.Set(requestIDHeader, strings.Repeat("x", 200))
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if id := rec.Header().Get(requestIDHeader); len(id) != 16 {
		t.Errorf("oversized request id should be replaced, got len %d", len(id))
	}
}

func TestRecoverMiddleware(t *testing.T) {
	e := newTestEnv(t, nil)
	h := e.srv.withRecover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	expectStatus(t, response{rec}, http.StatusInternalServerError, "internal_error")
}

func TestRecoverRepanicsAbortHandler(t *testing.T) {
	e := newTestEnv(t, nil)
	h := e.srv.withRecover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if err, _ := recover().(error); !errors.Is(err, http.ErrAbortHandler) {
			t.Errorf("recovered %v, want ErrAbortHandler", err)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

func TestStatusRecorderUnwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec}
	if sr.Unwrap() != rec {
		t.Error("Unwrap should return the underlying writer")
	}
}

func TestContextHelpersWithoutValues(t *testing.T) {
	ctx := context.Background()
	if userIDFrom(ctx) != "" || requestIDFrom(ctx) != "" {
		t.Error("expected empty values from bare context")
	}
}

// ---------- store failures ----------

var errBoom = errors.New("boom")

// failingStore wraps a real store and fails selected operations.
type failingStore struct {
	*store.Memory
	failGetUserByID    bool
	failGetUserByEmail bool
	failCreateUser     bool
}

func (f *failingStore) GetUserByID(ctx context.Context, id string) (auth.User, error) {
	if f.failGetUserByID {
		return auth.User{}, errBoom
	}
	return f.Memory.GetUserByID(ctx, id)
}

func (f *failingStore) GetUserByEmail(ctx context.Context, email string) (auth.User, error) {
	if f.failGetUserByEmail {
		return auth.User{}, errBoom
	}
	return f.Memory.GetUserByEmail(ctx, email)
}

func (f *failingStore) CreateUser(ctx context.Context, email, hash string) (auth.User, error) {
	if f.failCreateUser {
		return auth.User{}, errBoom
	}
	return f.Memory.CreateUser(ctx, email, hash)
}

func (f *failingStore) CreateTodo(context.Context, string, todo.CreateInput) (todo.Todo, error) {
	return todo.Todo{}, errBoom
}

func (f *failingStore) GetTodo(context.Context, string, string) (todo.Todo, error) {
	return todo.Todo{}, errBoom
}

func (f *failingStore) ListTodos(context.Context, string, store.ListFilter) ([]todo.Todo, int, error) {
	return nil, 0, errBoom
}

func (f *failingStore) DeleteCompletedTodos(context.Context, string) (int, error) {
	return 0, errBoom
}

func TestStoreErrorsReturn500(t *testing.T) {
	fs := &failingStore{Memory: store.NewMemory()}
	e := newTestEnv(t, fs)
	token, _ := e.register("err@example.com")

	for _, r := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/v1/todos", map[string]any{"title": "x"}},
		{"GET", "/api/v1/todos", nil},
		{"GET", "/api/v1/todos/x", nil},
		{"DELETE", "/api/v1/todos/completed", nil},
	} {
		expectStatus(t, e.do(r.method, r.path, token, r.body), http.StatusInternalServerError, "internal_error")
	}

	creds := auth.Credentials{Email: "err@example.com", Password: "password123"}

	fs.failGetUserByEmail = true
	expectStatus(t, e.do("POST", "/api/v1/auth/login", "", creds), http.StatusInternalServerError, "internal_error")
	fs.failGetUserByEmail = false

	fs.failCreateUser = true
	expectStatus(t, e.do("POST", "/api/v1/auth/register", "", auth.Credentials{Email: "new@example.com", Password: "password123"}),
		http.StatusInternalServerError, "internal_error")
	fs.failCreateUser = false

	fs.failGetUserByID = true
	expectStatus(t, e.do("GET", "/api/v1/todos", token, nil), http.StatusInternalServerError, "internal_error")
}

func TestMeWhenUserLookupFails(t *testing.T) {
	// requireAuth succeeds, then the handler's own lookup fails.
	fs := &flakyUserStore{Memory: store.NewMemory()}
	e := newTestEnv(t, fs)
	token, _ := e.register("flaky@example.com")
	fs.failAfter = 1
	expectStatus(t, e.do("GET", "/api/v1/auth/me", token, nil), http.StatusInternalServerError, "internal_error")
}

type flakyUserStore struct {
	*store.Memory
	failAfter int // fail GetUserByID once this many calls have succeeded; 0 disables
	calls     int
}

func (f *flakyUserStore) GetUserByID(ctx context.Context, id string) (auth.User, error) {
	f.calls++
	if f.failAfter > 0 && f.calls > f.failAfter {
		return auth.User{}, errBoom
	}
	return f.Memory.GetUserByID(ctx, id)
}

func TestLoginWithCorruptStoredHash(t *testing.T) {
	m := store.NewMemory()
	if _, err := m.CreateUser(context.Background(), "corrupt@example.com", "garbage"); err != nil {
		t.Fatal(err)
	}
	e := newTestEnv(t, m)
	res := e.do("POST", "/api/v1/auth/login", "", auth.Credentials{Email: "corrupt@example.com", Password: "password123"})
	expectStatus(t, res, http.StatusInternalServerError, "internal_error")
}
