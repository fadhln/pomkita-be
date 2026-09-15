# Backend development conventions

This document defines the backend conventions for people and AI agents. Use ASD-STE100 Simplified Technical English in all project documentation.

## 1. Repository purpose

This repository contains the PomKita backend service.

The service uses Go and Gin for HTTP transport. The service uses PostgreSQL for data integrity and transaction control. Go services validate business rules, repository adapters perform database work, and HTTP adapters map typed results to the public contract.

The backend owns the API contract. The frontend must not define backend behavior.

## 2. Directory structure

Use these directories:

```text
cmd/server/           Process start and graceful shutdown
internal/config/      Environment configuration
internal/httpapi/     Router, middleware, HTTP errors, and transport support
internal/httpapi/<module>/  Module handlers and request DTOs
internal/service/     Module use cases and business rules
internal/repository/  Module GORM repositories and shared database store
internal/jwt/         Token issue and verification support
internal/canonical/   Canonical JSON and schema validation
migrations/           Ordered SQL migrations
docs/                 Repository conventions and decisions
```

Create a package only when the package has one clear purpose. Do not create a package for one function. Keep an interface near the code that uses the interface.

For one module, the dependency direction is `httpapi -> service -> repository`.
HTTP handlers map requests and responses. Services own business rules.
Repositories own database queries and transactions. Keep GORM models in
`internal/repository/store` and do not return them from a repository.

Keep a handler unit test beside its module. Put tests that construct the root
router in `internal/httpapi/integration`.

## 3. Change design

Use one vertical slice for one behavior. A slice includes the service rule, repository operation, HTTP endpoint, contract, authorization cases, and tests.

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

## 4. Test-driven development

Always use test-driven development (TDD) for an implementation or a defect fix. Use the red-green-refactor sequence.

### Red

1. Select one observable behavior from the plan or defect report.
2. Add the smallest test that specifies this behavior.
3. Run only the applicable test.
4. Confirm that the test fails.
5. Confirm that the failure is caused by the missing behavior.

Do not change production code before the red step. If the test passes, the test does not prove the new behavior. Correct the test or select a different test boundary.

### Green

1. Add the smallest production change that can pass the test.
2. Run the applicable test.
3. Stop and correct the implementation if the test fails.
4. Run the related package tests after the applicable test passes.

Do not add unrelated behavior during the green step.

### Refactor

1. Improve names, structure, or duplication only after the test is green.
2. Do not change behavior during this step.
3. Run the applicable tests after each material refactor.
4. Run the full repository check before handoff.

A documentation-only change has no production behavior. Run `git diff --check` and the repository check for this type of change.

## 5. Go conventions

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
- Keep a handler small. Move transaction and repository work to the persistence adapter.
- Use structured logs. Include `request_id`, action, outcome, and duration.

Use typed configuration. Reject a missing required production value at process start. Do not read environment variables from business code.

## 6. HTTP conventions

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

## 7. API contract conventions

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

## 8. Database conventions

Name migrations with an ordered numeric prefix and a short action. Example: `000001_create_identity_tables.up.sql`.

Each migration must have an up file and a down file. The up file must apply to an empty database. The down file must reverse only its paired up file.

Use these rules for each tenant table:

- include all applicable tenant columns in foreign keys;
- enforce lifecycle and uniqueness rules with constraints and indexes;
- add the table lock rank where a transaction spans several aggregates;
- test cross-tenant denial in repository and service tests.

Do not add business functions, procedures, triggers, RLS policies, custom
application roles, or broad grants to the clean migration set. Keep business
rules in Go services and keep SQL focused on structural integrity.

Follow the global lock order in `PLAN.md`. Sort rows by table name and primary key when two locks have the same rank. Append the audit event last in the same transaction.

Use PostgreSQL `numeric` for money and volume. Check overflow before a cast, multiplication, sum, or variance operation. Map SQLSTATE `22003` to HTTP 422.

## 9. Security conventions

Use the verified session JWT as the only actor identity. Do not trust an actor ID, organization ID, station ID, or role from request data.

Use an httpOnly cookie for the session. Apply the configured same-site and CSRF rules. Do not store a raw JWT in the database audit data or logs.

Deny access when request context is missing or invalid. Apply a deny rule before an allow rule. Enforce separation of duties in the database.

Store secrets outside the repository. Commit only safe example values. Review every migration for grants and owner changes.

## 10. Test conventions

Use a unit test for pure Go logic. Use a PostgreSQL integration test for a database rule. Use a concurrency test for locks, leases, idempotency, or cardinality.

Test the success case and the denied cases. Test no context, wrong tenant, wrong station, wrong role, stale state, duplicate request, and numeric boundary when they apply.

Use these rules for each test:

- Prove one behavior or one invariant.
- Give the test a name that states the condition and result.
- Use arrange, act, and assert sections when the test has more than one step.
- Assert an observable result. Do not assert a private implementation detail.
- Use explicit expected values. Do not use an assertion that only checks for a non-empty result when an exact value is known.
- Keep the test deterministic. Use a fixed clock, fixed identifier, and deterministic seed data when they apply.
- Do not use `time.Sleep` to coordinate a test. Use a controllable clock, channel, barrier, or database lock.
- Keep tests independent. Do not depend on test order or data from a different test.
- Clean up each resource that the test creates.
- Mark a helper with `t.Helper()`.
- Use a table test only when all cases use the same behavior and assertion structure.
- Use `t.Parallel()` only when the test data and external resources are isolated.
- Do not call a production external service from a test.
- Do not skip or retry a flaky test. Find and correct the cause.

Name a Go test with this pattern when it improves clarity:

```text
Test<Unit>_<Condition>_<Result>
```

For a defect fix, first add a regression test that fails on the old code. Keep the regression test after the fix.

Mock an external boundary only when a real boundary is not part of the behavior under test. Do not mock PostgreSQL behavior that PostgreSQL enforces. Use a real PostgreSQL container.

For a migration change, test these operations:

1. Apply all up migrations to an empty database.
2. Run the catalog and privilege checks.
3. Run the integration tests.
4. Apply the paired down migration when the phase permits it.
5. Apply the up migration again.

Run this command before handoff:

```sh
make check
```

Test coverage is a signal. It is not proof of correct behavior. Cover each changed branch and each applicable failure path.

## 11. Git and review conventions

Keep one purpose in one commit. Use an imperative commit subject. Keep the subject short. Add the phase prefix when a phase applies.

Examples:

```text
B0: add request context validation
B1: reject an expired draft claim
docs: define database test rules
```

Do not rewrite unrelated code. Do not commit secrets, local environment files, test output, or binaries. Do not change generated files without the source change that produced them.

A pull request must state the problem, the new behavior, the plan reference, migration impact, contract impact, security impact, and validation commands.

## 12. Handoff format

Use this order in the final handoff:

1. State the delivered behavior.
2. List the important changed files.
3. State the API and migration effect.
4. State the red-step command and the expected failure.
5. State the green-step and full-check commands.
6. State a material risk or an open dependency.

Do not state that work is complete when a required check did not run.
