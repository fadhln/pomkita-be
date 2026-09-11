# Backend development conventions

This document defines the backend conventions for people and AI agents. Use ASD-STE100 Simplified Technical English in all project documentation.

## 1. Repository purpose

This repository contains the PomKita backend service.

The service uses Go and Gin for HTTP transport. The service uses PostgreSQL for business rules and data control. The Go service validates request shape, starts a transaction, sets request context, calls an allowlisted procedure, and maps the result to HTTP.

The backend owns the API contract. The frontend must not define backend behavior.

## 2. Directory structure

Use these directories:

```text
cmd/server/           Process start and graceful shutdown
internal/config/      Environment configuration
internal/httpapi/     Router, middleware, handlers, and HTTP errors
internal/db/          pgx pool, transactions, and procedure calls
internal/jwt/         Token issue and verification support
internal/canonical/   Canonical JSON and schema validation
internal/money/       Decimal-string and numeric boundary support
migrations/           Ordered SQL migrations
seed/                 Deterministic development and test data
test/                 Cross-package integration and concurrency tests
docs/                 Repository conventions and decisions
```

Create a package only when the package has one clear purpose. Do not create a package for one function. Keep an interface near the code that uses the interface.

## 3. Change design

Use one vertical slice for one behavior. A slice includes the database rule, procedure, HTTP endpoint, contract, authorization cases, and tests.

Before implementation, record these items in the task or handoff:

- the applicable `PLAN.md` section;
- the actor and permitted role;
- the input and output contract;
- the state transition;
- the lock order;
- the audit event;
- the expected error codes;
- the tests that prove the behavior.

Do not add speculative abstractions. Add an abstraction after a second clear use case exists.

## 4. Go conventions

- Run `gofmt` on all Go files.
- Use short package names in lowercase.
- Use a descriptive name for each exported identifier.
- Add a documentation comment to each exported identifier.
- Pass `context.Context` as the first parameter for I/O work.
- Return an error to the caller. Do not use `panic` for an expected error.
- Wrap an error with operation context. Preserve the original error.
- Compare known errors with `errors.Is` or `errors.As`.
- Accept dependencies through constructors. Do not use mutable package globals.
- Put an interface in the consumer package.
- Keep a handler small. Move transaction and procedure work to `internal/db`.
- Use structured logs. Include `request_id`, action, outcome, and duration.

Use typed configuration. Reject a missing required production value at process start. Do not read environment variables from business code.

## 5. HTTP conventions

Use `/api/v1` for product endpoints. Keep `/health` and `/ready` outside the version prefix.

Use JSON field names in `snake_case`. Use UTC ISO 8601 timestamps with six fractional digits. Send money and volume as decimal strings. Send identifiers as lowercase UUID strings.

Use this error shape:

```json
{
  "code": "STABLE_MACHINE_CODE",
  "message": "Safe user message",
  "request_id": "request identifier",
  "field_errors": {
    "field.path": "validation message"
  }
}
```

Use stable machine codes. Do not make the frontend parse an error message. Do not expose SQL, stack traces, secrets, or internal object names.

Use these status groups:

| Status | Use |
|---:|---|
| 400 | Invalid JSON or request shape |
| 401 | Missing or invalid session |
| 403 | Authenticated actor does not have authority |
| 404 | Resource is absent from the permitted tenant scope |
| 409 | State, revision, lock, stale data, or idempotency conflict |
| 422 | Valid shape with invalid business values or numeric overflow |
| 500 | Unexpected server failure |

Return `X-Request-ID` on every response. Preserve a valid caller request ID. Generate a request ID when the caller does not send one.

Do not retry a write inside an HTTP handler. Retry a complete serializable transaction only for an approved serialization rule.

## 6. API contract conventions

Keep the OpenAPI document and JSON Schemas in this repository. Treat them as source files. Generate derived clients and fixtures from them.

For each endpoint, define:

- authentication and permitted roles;
- request headers and idempotency rules;
- request and response schemas;
- all expected status codes;
- stable error codes;
- one success example;
- one example for each important error.

Reject unknown fields in canonical mutation payloads. Keep array sort rules and `hash_version` explicit. Make an additive change when possible. Coordinate a breaking contract change with a frontend release.

## 7. Database conventions

Name migrations with an ordered numeric prefix and a short action. Example: `000001_create_identity_tables.up.sql`.

Each migration must have an up file and a down file. The up file must apply to an empty database. The down file must reverse only its paired up file.

Use these rules for each tenant table:

- include all applicable tenant columns in foreign keys;
- enable and force row-level security;
- revoke default privileges;
- classify the object in the required catalog;
- add the table lock rank;
- test cross-tenant denial.

Use `SECURITY DEFINER` only for an allowlisted function. Set a fixed `search_path`. Validate request context inside the function. Do not accept a table name, column name, or SQL text as input.

Follow the global lock order in `PLAN.md`. Sort rows by table name and primary key when two locks have the same rank. Append the audit event last in the same transaction.

Use PostgreSQL `numeric` for money and volume. Check overflow before a cast, multiplication, sum, or variance operation. Map SQLSTATE `22003` to HTTP 422.

## 8. Security conventions

Use the verified session JWT as the only actor identity. Do not trust an actor ID, organization ID, station ID, or role from request data.

Use an httpOnly cookie for the session. Apply the configured same-site and CSRF rules. Do not store a raw JWT in the database audit data or logs.

Deny access when request context is missing or invalid. Apply a deny rule before an allow rule. Enforce separation of duties in the database.

Store secrets outside the repository. Commit only safe example values. Review every migration for grants and owner changes.

## 9. Test conventions

Use a unit test for pure Go logic. Use a PostgreSQL integration test for a database rule. Use a concurrency test for locks, leases, idempotency, or cardinality.

Test the success case and the denied cases. Test no context, wrong tenant, wrong station, wrong role, stale state, duplicate request, and numeric boundary when they apply.

For a migration change, test these operations:

1. Apply all up migrations to an empty database.
2. Run the catalog and privilege checks.
3. Run the integration tests.
4. Apply the paired down migration when the phase permits it.
5. Apply the up migration again.

Do not mock PostgreSQL behavior that PostgreSQL enforces. Use a real PostgreSQL container.

Run this command before handoff:

```sh
make check
```

## 10. Git and review conventions

Keep one purpose in one commit. Use an imperative commit subject. Keep the subject short. Add the phase prefix when a phase applies.

Examples:

```text
B0: add request context validation
B1: reject an expired draft claim
docs: define database test rules
```

Do not rewrite unrelated code. Do not commit secrets, local environment files, test output, or binaries. Do not change generated files without the source change that produced them.

A pull request must state the problem, the new behavior, the plan reference, migration impact, contract impact, security impact, and validation commands.

## 11. Handoff format

Use this order in the final handoff:

1. State the delivered behavior.
2. List the important changed files.
3. State the API and migration effect.
4. State the checks and their results.
5. State a material risk or an open dependency.

Do not state that work is complete when a required check did not run.
