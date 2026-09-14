# API contract baseline

Product endpoints use `/api/v1`. The health endpoints stay unversioned:

```text
GET /health
GET /ready
```

The server currently keeps the unversioned product paths as compatibility
aliases. New clients must use the versioned paths.

| Resource | Method and path |
| --- | --- |
| Session | `POST /api/v1/login` |
| Session | `DELETE /api/v1/logout` |
| Session | `GET /api/v1/session` |
| Shift | `POST /api/v1/shift/open` |
| Shift | `GET /api/v1/shifts` |
| Shift | `GET /api/v1/shifts/{id}` |
| Draft | `POST /api/v1/draft/claim` |
| Draft | `POST /api/v1/draft/heartbeat` |
| Draft | `POST /api/v1/draft/reading` |
| Draft | `POST /api/v1/draft/sales` |
| Draft | `POST /api/v1/draft/loss` |
| Draft | `POST /api/v1/draft/evidence` |
| Draft | `GET /api/v1/draft` |
| Submission | `POST /api/v1/shift/submit` |
| Report | `GET /api/v1/report/{id}` |
| Governance | `POST /api/v1/shift/ack` |
| Governance | `POST /api/v1/amendment/request` |
| Governance | `POST /api/v1/amendment/approve` |
| Governance | `POST /api/v1/amendment/reject` |
| Governance | `GET /api/v1/amendments` |
| Governance | `GET /api/v1/anomalies` |
| Reporting | `GET /api/v1/report/{id}/printout` |
| Reporting | `GET /api/v1/anomalies/export` |
| Reporting | `GET /api/v1/audit` |
| Reporting | `GET /api/v1/audit/export` |
| Reporting | `GET /api/v1/audit/verify` |
| Policy | `GET /api/v1/policy/history` |
| Policy | `POST /api/v1/policy/revision` |
| Policy | `POST /api/v1/policy/revision/tombstone` |

All JSON objects use `snake_case`. Mutation payloads reject unknown fields.
Money and volume values are decimal strings. Every response includes
`X-Request-ID` and errors use the stable `code`, `message`, `request_id`, and
`field_errors` shape.

## Naming rules

- Go files use lower snake case without phase prefixes.
- Tests use the unit name, for example `shift_service_test.go`.
- Migrations use an ordered numeric prefix, for example
  `000001_initial_schema.up.sql` and `000001_initial_schema.down.sql`.
- Packages use the domain or adapter name. Do not use a generic package name
  for a repository boundary.
- HTTP code stays in `internal/httpapi` until the adapter migration moves it to
  `internal/adapter/http`.
- Database code stays behind repository or persistence adapters. A handler does
  not query a table.
