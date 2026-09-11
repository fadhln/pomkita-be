# BE-PLAN.md — Backend Implementation Plan (Gin + Go + PostgreSQL)

Status: execution plan. Source of truth for requirements: PLAN.md (SSOT). This file breaks the backend into phased, TDD-verified work.
Technical text uses ASD-STE100 Simplified Technical English.

## 0. Repository

New repository: `pomkita-be` (Go module). The DB is the enforcement layer. The Go service is a thin authorized transport and orchestration layer: it owns HTTP, sessions, validation of request shapes, and calls to allowlisted DB procedures. It never holds table DML grants.

Layout:

    cmd/server/           main, router, graceful shutdown
    internal/httpapi/     Gin handlers per resource, middleware (auth, context, request-id)
    internal/db/          pgx pool, procedure calls, transaction templates
    internal/jwt/         HS256 sign/verify, kid rotation client
    internal/canonical/   RFC 8785 canonical JSON + JSON Schema validation
    internal/money/       numeric handling, decimal strings, overflow checks
    migrations/           versioned SQL migrations (schema, functions, triggers, grants)
    seed/                 bootstrap: org, station, nozzles, superadmin, audit_chain_locks
    test/                 integration tests against real Postgres (testcontainers)

Tools: Go 1.23+, pgx/v5, golang-migrate, testcontainers-go, stretchr/testify. Config by env vars. No ORM.

## 1. Phases

Each phase ends with all its tests green, a migration that applies cleanly to a fresh database, and a commit. Phase gates reference PLAN.md acceptance criteria (§11).

### Phase B0 — Foundation (schema skeleton, auth, roles)

Delivers:

1. Migrations for master data tables only: organizations, stations, users, user_station_roles, sessions, jwt_keys, procedure_registry, audit_chain_locks. Composite tenant keys per PLAN.md §4.
2. Role model: NOLOGIN app roles, table owner roles, audit_owner, relay role. Full grants matrix: REVOKE ALL from PUBLIC and app roles; EXECUTE grants to procedures only. Default privileges revoked. A CI check queries pg_catalog and fails on any unclassified table, view, function, sequence, or role.
3. fn_set_request_context: JWT verification in the database (alg pinned, iss/aud/exp/iat/jti checks, session lookup), transaction-local set_config with true. Fail closed.
4. JWT service in Go: HS256, kid rotation against jwt_keys, 15-minute expiry, jti issuance, logout sets revoked_at.
5. HTTP middleware: verify token, attach request-id, map DB errors (23514, 23505, 22003, 40P01) to HTTP codes.
6. /health and /ready endpoints.

Tests (watch red first, all against real Postgres):

- valid token passes; each bad claim (alg, iss, aud, exp skew, future iat, revoked jti, unknown kid) fails with the exact error.
- context fail-closed: no token, expired token, missing session row.
- grants: app role denied SELECT/INSERT/UPDATE/DELETE on every table (negative matrix, generated from pg_catalog).
- rotation: previous key accepted until max_token_expiry, retired after.

Gate: PLAN.md §11.2 partial (JWT row of pilot matrix: signature, claims, logout, retired key = 100% reject).

### Phase B1 — Reconciliation core (shifts, reports, submit)

Delivers:

1. Migrations: nozzles, dispensers, tanks, nozzle_tank_map, dispenser_nozzle_map, dispenser_prices (exclusion constraints), shifts, shift_drafts + child tables, submit_idempotency, shift_reports, dispenser_readings, sales_declared, loss_identity, loss_entries, deliveries, dip_readings, meter_reset_events, nozzle_baseline_revisions, nozzle_baseline_current, shift_transitions. All composite FKs per PLAN.md §4.3 catalog.
2. Immutability enforcement: BEFORE UPDATE/DELETE triggers on shift_reports and all report children (raise 23514), submitted -> locked only through fn_ack_shift context.
3. fn_open_shift: station lock, station_seq allocation, price/mapping/meter_max/modulus snapshot, snapshot hash.
4. Draft lease protocol: claim_token, claim_expires_at, revision fencing in one conditional UPDATE per mutation.
5. fn_submit_shift: full idempotency protocol per PLAN.md §5.1 (8 steps), canonical request hash, snapshot validation, evidence policy snapshot creation (policy tables migrated here: threshold_policy_revisions, evidence_policy_revisions, evidence_policy_types, policy_snapshot_sets/items), report + children insert in one transaction, Rupiah computation server-side (half-up, overflow check 22003 -> 422).
6. Meter rules: rollover formula with modulus, rollover_threshold, chaining from predecessor station_seq or approved reset or baseline.
7. Recovery job: submitting drafts > 10 minutes -> recovering -> submitted/editing/failed; recovery_count; audit event.
8. read_* procedures for: shift list, shift detail, draft, report view (all verify context).

Tests:

- idempotency: same key+hash replay returns stored report; same key new hash 409; failed row forces new key; takeover race (2 workers, 1 winner, zero-row CAS -> 409).
- draft fencing: expired client write rejected; concurrent child edit increments revision correctly.
- submit atomicity: forced failure after report insert leaves zero rows (transaction rollback proof).
- rollover: 99999.9 -> 0.0 delta correct; over-threshold rollover rejected; double rollover rejected.
- chaining: shift N start = shift N-1 locked end; reset and baseline variants.
- overflow: price × delta > numeric(14,0) max -> SQLSTATE 22003 -> HTTP 422.
- timezone: cross-midnight shift, business_date from timezone_snapshot.

Gate: PLAN.md §11.5, 11.6, 11.12, 11.13, and the concurrency rows of the pilot matrix (takeover race >= 3 iterations).

### Phase B2 — Governance (ack, amendment, audit chain, relay)

Delivers:

1. ack_decisions, ack_head, ack_supersessions migrations per PLAN.md §3.4 exact DDL.
2. fn_ack_shift: head/cardinality protocol, ack_seq allocation, needs_correction and locked transitions, deferred triggers.
3. fn_approve_amendment: allowlist paths, requester != approver (break-glass branch with reason), stale_check_hash verification, clone + apply + anomaly re-evaluation + pointer move + supersession in one transaction; amendments and amendment_items migrations.
4. fn_transition_shift with transition table checks and shift_transitions rows.
5. Audit chain: fn_append_audit_event (chain lock, org_sequence allocation, exact byte serialization per PLAN.md §9), audit_outbox insert in the same business transaction; audit_denied path on a separate connection with a 3-second timeout, fail-closed.
6. Relay worker: fn_relay_claim_event / fn_relay_finish_event, 5-minute lease, bounded exponential retry, idempotent sink send (log file sink in dev).
7. Break-glass flags on ack_decisions and amendments with non-empty reason CHECK; anomaly exposure in read_*.
8. Abandonment scheduler: failed > 24 hours -> abandoned, idempotent.

Tests:

- ack: happy path locked; rejection -> needs_correction; second ack on same version rejected; supersession after amendment (old head NULL, new head NULL, replacement version_no = old + 1).
- amendment: each allowlist path applies; meter path rejected with reason; requester self-approval rejected; break-glass accepted with reason and flagged; stale base 409; one pending per base (partial unique).
- chain: append 100 events, verifier reads by org_sequence, zero gaps; tamper test (manual UPDATE as audit_owner then verify fails); business rollback when chain append fails (force error).
- relay: crash mid-claim leaves expired lease; retry same event_id, sink dedupes; attempt_count increments, no duplicate chain row.
- audit_denied: connection cut -> request gets no response (fail closed); request_id unique.

Gate: PLAN.md §11.4, 11.10, and audit replay row (export + chain verify, zero gaps).

### Phase B3 — Anomalies, alerts, evidence enforcement

Delivers:

1. alert_rules, alert_events migrations with full constraint set per PLAN.md §6.
2. Occurrence procedure: rule-row lock, fired/cleared insert, period_bucket, unique-key dedupe.
3. Starvation scheduler: open/awaiting shift > 24 hours -> fired; idempotent.
4. Variance alert (opt-in rule type).
5. fn_validate_evidence per PLAN.md §6 (snapshot-driven, type/MIME/count checks, wajib/opsional exception logic) used by submit and amendment approval, plus the deferred safety-net trigger.
6. read_* procedures: anomaly list (union: break-glass, loss exceptions, above-threshold variance), alert list, ack queue.
7. Backfill support: fn_open_shift backfill branch (original_event_date, shift_ke, Owner approval fields), out-of-order guard, carried-forward meter readings (observed=false, is_carried_forward=true).

Tests:

- starvation: 1 shift > 24h -> exactly 1 fired; retry does not duplicate; cleared on lock.
- fired/cleared unique keys enforced; source_version_no nullability by source_kind.
- evidence wajib: missing evidence rejects submit; exception rejected. opsional: missing evidence requires exception; exception with finalized evidence rejected.
- backfill: idempotent retry = 1 report; out-of-order rejected when later chained report exists; carried-forward reading flags set.

Gate: PLAN.md §11.3, 11.9, 11.11, and starvation row of the pilot matrix.

### Phase B4 — Reporting, printout API, hardening

Delivers:

1. read_* procedures: report printout payload (immutable snapshot join), anomaly export, audit trail export (with chain verification endpoint), policy history.
2. Printout payload equals the report snapshot exactly (same bytes policy: canonical JSON emitted from the same read path the FE uses).
3. Load and soak tests: 3 concurrent workers per station, 12-shift simulated day; deadlock tests per PLAN.md §8 lock order.
4. Negative-test matrix generator: for every procedure, unauthorized tenant access attempts (cross-org, cross-station, no context) must fail; runs in CI.
5. Backup/restore runbook: pg_dump + WAL archiving config, restore rehearsal with checksum comparison.
6. seed/ finalization: demo org + station + nozzles + users for the pilot.

Tests:

- printout == report screen payload (byte comparison in test).
- isolation matrix green (repeat 3x in CI).
- deadlock: zero deadlocks across 3-worker concurrency suite.
- restore: checksum match after restore rehearsal.

Gate: PLAN.md §11.7, 11.14, and the isolation, backfill, and backup rows of the pilot matrix.

## 2. Definition of done (every phase)

- Tests written first, watched red, then green.
- Migration applies cleanly to an empty database and is reversible (down migration).
- CI: lint (golangci-lint), vet, race detector on, integration tests against real Postgres, unclassified-object check green.
- All code committed; phase summary vs this file posted at the boundary.
- No phase adds a table, procedure, role, or grant outside PLAN.md §4/§7 without first updating PLAN.md (SSOT) in the same commit.

## 3. Cross-cutting rules

- Every request: verify JWT -> fn_set_request_context -> procedure call -> map error -> respond. No handler queries tables directly.
- Money and volume never touch float64. decimal strings end to end; pgx numeric scan to string.
- All timestamps timestamptz(6); JSON serialization UTC ISO 8601 with six fractional digits.
- Errors: 400 shape, 401 auth, 403 authorization, 404 tenant scope, 409 conflict/state/stale/idempotency-hash, 422 validation/overflow, 500 unexpected (audit + alert).
- Structured logging with request_id; no PII, no raw JWT in logs.
