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
| Session | `POST /api/v1/session/active-context` |
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

## Active context

A Superadmin can set both values for the current login session with
`POST /api/v1/session/active-context`. The request requires the
`X-Requested-With` header and a JSON body with `org_id` and `station_id`.
Both targets must exist. The station must belong to the organization. A disabled
organization or station can remain the active context so a Superadmin can inspect
historical data. Reads continue to work in a disabled scope. Operational writes,
such as shift opening, draft changes, report submission, acknowledgements, and
amendments, return HTTP 409 with `org_disabled` when the organization is disabled, or
`station_disabled` when only the station is disabled. The server returns `204
No Content` after the context update.

A Superadmin can re-enable an organization with `PATCH /api/v1/organizations/{id}`
and a station with `PATCH /api/v1/stations/{id}` by setting `enabled` to `true`.

`GET /api/v1/session` returns `active_context` with `org_id` and `station_id`.
It returns `null` when this session has no active context. The context changes
request scope only. It does not change the authenticated user or role grants.
Another login session keeps its own context.

The latest successful context change also saves an account preference.
A later login on any device starts with that organization and station when both
records still exist and the station still belongs to the organization.
Disabled targets are valid for this preference. If a target is missing or the
station does not belong to the organization, the new session has no active
context. The existing identity defaults then apply. The server does not choose a
station for this fallback.

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
