# PomKita Backend

Gin and Go backend for the PomKita SPBU reconciliation application. PostgreSQL stores and protects the data. Go services own business decisions and GORM repositories own database access.

## Bootstrap

Requirements: Go 1.23 or newer. PostgreSQL is required for repository integration tests and local operation.

```sh
cp .env.example .env
go run ./cmd/server
```

The service listens on `:8080` by default.

```sh
curl http://localhost:8080/health
curl http://localhost:8080/ready
```

## Commands

```sh
make check     # format check, vet, tests
make run       # run the server
make tidy      # update Go module metadata
```

The implementation uses the service and GORM repository boundaries documented in
[`docs/CONVENTIONS.md`](docs/CONVENTIONS.md). API contract artifacts are
published from this repository and pinned by the frontend.

## Development conventions

Read `AGENTS.md` before you change code. Read `docs/CONVENTIONS.md` for architecture, API, database, security, test, Git, and handoff rules.

Always use the red-green-refactor TDD sequence for an implementation or a defect fix.
