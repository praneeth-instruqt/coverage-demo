# coverage-demo

A Todo REST API in Go with user authentication, built only on the standard library.

## Features

- **Auth**: register and login. Passwords are hashed with PBKDF2-HMAC-SHA256 and requests are authenticated with HS256 JWT bearer tokens.
- **CRUD for todos**: each todo belongs to one user, and other users get a 404 for it.
- **Listing**: filter by status or priority, search text, and paginate.
- **Middleware**: request IDs, structured JSON logging (`slog`), panic recovery and graceful shutdown.
- **CI**: two GitHub Actions workflows run on every PR into `main` and every push to `main`:
  - `test.yaml`: race-enabled tests and the coverage workflow described below.
  - `ci.yml`: gofmt, vet, golangci-lint, a binary build and a Docker build.

## Quick start

```sh
make run                       # starts on :8080 with a dev JWT secret
make test                      # unit tests with -race
make cover                     # per-file coverage, regenerates COVERAGE.md + coverage.html
make lint                      # golangci-lint
make docker                    # build container image
```

## Configuration

| Variable          | Default  | Description                                   |
|-------------------|----------|-----------------------------------------------|
| `JWT_SECRET`      | required | HMAC signing key, at least 32 bytes           |
| `ADDR`            | `:8080`  | Listen address                                |
| `TOKEN_TTL`       | `24h`    | Access token lifetime (Go duration)           |
| `HASH_ITERATIONS` | `600000` | PBKDF2 iterations                             |

## API

Every `/api/v1/todos` route and `/api/v1/auth/me` need `Authorization: Bearer <token>`.

| Method | Path                         | Description                              |
|--------|------------------------------|------------------------------------------|
| GET    | `/healthz`                   | Liveness check                           |
| POST   | `/api/v1/auth/register`      | `{email, password}` → `201 {token, expires_at, user}` |
| POST   | `/api/v1/auth/login`         | `{email, password}` → `200 {token, expires_at, user}` |
| GET    | `/api/v1/auth/me`            | Current user                             |
| GET    | `/api/v1/todos`              | List: `?completed=`, `?priority=`, `?q=`, `?limit=` (1–100, default 20), `?offset=` |
| POST   | `/api/v1/todos`              | Create: `{title, description?, priority?, due_date?}` |
| GET    | `/api/v1/todos/{id}`         | Get one                                  |
| PATCH  | `/api/v1/todos/{id}`         | Partial update: any of `title, description, completed, priority, due_date, clear_due_date` |
| DELETE | `/api/v1/todos/{id}`         | Delete → `204`                           |
| DELETE | `/api/v1/todos/completed`    | Delete all completed → `{deleted: n}`    |

`priority` is one of `low`, `medium` (the default) or `high`. `due_date` is RFC 3339.

Errors use one shape: `{"error": {"code": "validation_failed", "message": "title is required"}}`.

### Example

```sh
TOKEN=$(curl -s -X POST localhost:8080/api/v1/auth/register \
  -d '{"email":"me@example.com","password":"password123"}' | jq -r .token)

curl -s -X POST localhost:8080/api/v1/todos -H "Authorization: Bearer $TOKEN" \
  -d '{"title":"Ship it","priority":"high"}'

curl -s "localhost:8080/api/v1/todos?completed=false" -H "Authorization: Bearer $TOKEN"
```

## Code coverage

[`COVERAGE.md`](COVERAGE.md) lists the coverage of every file and the overall percentage. CI regenerates it and commits it to `main` after each merge, so don't edit it by hand.

On every push to a branch, `test.yaml` runs the tests and computes coverage. If the branch has an open PR into `main`:

- **PR description**: a coverage section is added, or replaced on later pushes. It shows the overall percentage and the coverage of each file the PR changes. Text you write in the description is kept.
- **Below 85%**: the section adds a table of the files pulling the total down. Each row shows the file's coverage, its uncovered statements and its **impact**, meaning how many percentage points the total would rise if that file were fully covered. Uncovered lines that the PR adds also get inline review comments, and the check fails.
- **Each new push** replaces the previous run's line comments, so they stay current.

### Excluding files

Add paths to [`.coverageignore`](.coverageignore) to leave them out of the overall percentage. They are still listed in `COVERAGE.md` under "Excluded". Each line is one of:

```
internal/legacy/          # a whole directory
cmd/server/main.go        # a single file
internal/*/mock_*.go      # a glob on the full path
*_gen.go                  # a glob on the file name
```

## Layout

```
cmd/server/        entrypoint, config wiring, graceful shutdown
internal/api/      HTTP handlers, routing, middleware
internal/auth/     users, password hashing, JWT tokens
internal/config/   environment configuration
internal/store/    Store interfaces + thread-safe in-memory implementation
internal/todo/     Todo domain model and validation
tools/coverage/    coverage report generator used by CI (COVERAGE.md, PR section, line comments)
.github/workflows/ test.yaml (tests + coverage), ci.yml (lint + build)
```

Data is kept in memory, so it is lost on restart. To make it persistent, implement `store.Store` (for example on Postgres) and pass it in `cmd/server/main.go`.
