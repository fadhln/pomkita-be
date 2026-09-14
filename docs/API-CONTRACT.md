# API contract baseline

Product endpoints use `/api/v1`. The health endpoints stay unversioned:

```text
GET /health
GET /ready
```

The router exposes the versioned paths below.

| Resource | Method and path |
| --- | --- |
| Session | `POST /api/v1/login` |
| Session | `DELETE /api/v1/logout` |
| Session | `GET /api/v1/session` |
| Shift | `POST /api/v1/shifts` |
| Shift | `GET /api/v1/shifts` |
| Shift | `GET /api/v1/shifts/{id}` |
| Draft | `POST /api/v1/drafts/claim` |
| Draft | `POST /api/v1/drafts/heartbeat` |
| Draft | `POST /api/v1/drafts/readings` |
| Draft | `POST /api/v1/drafts/sales` |
| Draft | `POST /api/v1/drafts/losses` |
| Draft | `POST /api/v1/drafts/evidence` |
| Submission | `POST /api/v1/submissions` |
| Report | `GET /api/v1/reports/{id}` |
| Governance | `POST /api/v1/reports/{id}/acknowledgement` |
| Governance | `POST /api/v1/amendments` |
| Governance | `POST /api/v1/amendments/{id}/approve` |
| Governance | `POST /api/v1/amendments/{id}/reject` |
| Reporting | `GET /api/v1/audit/export` |
| Reporting | `GET /api/v1/audit` |
| Reporting | `GET /api/v1/audit/verify` |
| Reporting | `GET /api/v1/anomalies` |
| Reporting | `GET /api/v1/anomalies/export` |
| Policy | `GET /api/v1/policies/history` |
| Policy | `POST /api/v1/policies/revisions` |

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
- HTTP code stays in `internal/httpapi`.
- Database code stays behind repository or persistence adapters. A handler does
  not query a table.
