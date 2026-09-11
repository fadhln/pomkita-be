# PomKita Backend

Gin and Go backend for the PomKita SPBU reconciliation application. PostgreSQL is the enforcement layer. The application role will call allowlisted procedures and will not receive table DML grants.

## Bootstrap

Requirements: Go 1.23 or newer. PostgreSQL and Docker are needed for later integration phases.

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

The implementation follows the phases in `../BE-PLAN.md`. API contract artifacts will be published from this repository and pinned by the frontend.

## Development conventions

Read `AGENTS.md` before you change code. Read `docs/CONVENTIONS.md` for architecture, API, database, security, test, Git, and handoff rules.

Always use the red-green-refactor TDD sequence for an implementation or a defect fix.
