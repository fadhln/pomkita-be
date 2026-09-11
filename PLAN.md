# PLAN.md — SPBU Reconciliation Application (name TBD)

Status: draft v19, build target.
UI language: All user-facing copy is Bahasa Indonesia. Technical text uses ASD-STE100 Simplified Technical English.

## 1. Product target

The application supports one organization and many SPBU stations. It records one manual shift report per station and reconciles the report at nozzle level.

The MVP prevents fraud in loss/gain and sales declarations. It has two separate reconciliation units:

- volume, in liters, with separate loss and gain thresholds;
- money, in Rupiah, with a separate variance threshold.

The default loss-liter threshold is 10.00 L. The threshold is a versioned policy value and can be changed by the Owner.

The MVP includes immutable reports, amendment, re-acknowledgement, audit trail, policy versioning, anomaly records, and printout. It does not include cash reconciliation, Operator acknowledgement, external notification, dispenser integration, temperature correction, or multiple organizations.

## 2. Roles, permissions, and separation of duties

| Role | Scope | MVP authority |
|---|---|---|
| Operator | One station | Account only. Operational actions are deferred. |
| Supervisor | One station | Open and submit a shift; enter data; print; request amendment. |
| Station Admin | One or more stations | Acknowledge shift; approve amendment; generate report; view all assigned stations; manage dispenser. |
| Owner | Organization | Create users; approve amendment; view audit, reports, and anomalies; change policy; fallback acknowledgement. |
| Superadmin | All organizations | Break-glass actions only with a reason. All actions are audited. |

The server evaluates every action for the user_id from the verified session JWT. Deny rules have precedence over allow rules. Multiple roles do not bypass separation of duties.

The server enforces these rules:

1. A user who created data in a report cannot acknowledge that shift.
2. An amendment requester cannot approve the same amendment.
3. One pending amendment is allowed for one base report.
4. ack_shift is effective only for Station Admin and Owner.
5. Owner cannot enter operational data.
6. Break-glass is available to Owner and Superadmin only. It requires a non-empty reason. The database validates the reason, stores is_break_glass, is_superadmin, and the reason, writes an audit event, and exposes the action in the anomaly list.

Permission names are ack_shift, approve_amendment, manage_users, generate_report, view_all_stations, and manage_dispensers.

## 3. Authoritative state rules

This section is the only lifecycle definition in this plan.

### 3.1 Shift lifecycle

The shifts.status enum is:

open | submitting | failed | abandoned | awaiting_confirmation | needs_correction | locked

Allowed transitions are:

| From | To | Server condition |
|---|---|---|
| open | submitting | fn_submit_shift owns the draft lease. |
| submitting | awaiting_confirmation | Report, snapshot, anomaly, and evidence checks commit. |
| submitting | failed | Submit transaction fails. The report transaction rolls back. |
| failed | open | fn_reopen_failed_shift finds no report and increments draft revision. |
| failed | abandoned | Scheduler finds a failed shift older than 24 hours. |
| awaiting_confirmation | locked | fn_ack_shift accepts the current report. |
| awaiting_confirmation | needs_correction | fn_ack_shift rejects the current report. |
| needs_correction | awaiting_confirmation | fn_approve_amendment creates the next report. This transition requires a report. |
| locked | awaiting_confirmation | fn_approve_amendment creates the next report from the current locked report, supersedes its acknowledgement, and requires re-acknowledgement. |

abandoned is terminal. It does not block a new shift. needs_correction does not block a new shift. A partial unique index allows at most one shift per station with status open, submitting, failed, or awaiting_confirmation.

Backfill is a flag, not a status. A backfill stores original_event_date, shift_ke, backfilled, backfill_approver, backfill_approved_at, and backfill_reason. The approval is by Owner. The partial unique key is (org_id, station_id, original_event_date, shift_ke) where backfilled = true. More than one shift can exist on one event date when shift_ke differs.

Every transition uses fn_transition_shift. The procedure locks the station, then the shift, checks the table above, inserts one shift_transitions row, and writes one audit event in the same transaction.

### 3.2 Report lifecycle and immutability

shift_reports.status is submitted | locked. shifts.current_report_id is the only current-report pointer. There is no is_current column.

fn_submit_shift creates version 1. fn_approve_amendment creates the next version by cloning the complete base report and applying only the amendment allowlist. The old report is never edited. A locked base report is allowed, but its old acknowledgement is superseded and the new report requires acknowledgement.

The database enforces immutability:

- shift_reports has a BEFORE DELETE trigger that always raises 23514.
- shift_reports has a BEFORE UPDATE trigger. It allows only submitted -> locked, and only when current_user = report_writer and the transaction-local value app.transition = lock_report is set by fn_ack_shift. Every other column must be byte-for-byte unchanged. Any other update raises 23514.
- Report child tables (dispenser_readings, sales_declared, loss_entries, loss_exception, delivery_snapshots, dip_snapshots, and evidence_event) have BEFORE UPDATE OR DELETE triggers that always raise 23514.
- shifts.current_report_id, report status, and acknowledgement pointers can change only in the named transition procedures. Their triggers reject direct changes and reject a pointer that is not the same tenant and shift.
- The application role has no table DML grant. The report_writer role is NOLOGIN and is reached only through allowlisted SECURITY DEFINER procedures with a fixed search_path.

The transition context is narrow and database-owned. fn_submit_shift may insert one report, its children, and its ack_head, and may update the matching draft, submit_idempotency row, and shift status. fn_ack_shift may update only report status submitted -> locked, ack_head, ack_decisions, ack_supersessions, and the matching shift status. fn_approve_amendment may insert the new report and its children, set the amendment decision, supersede the old head, insert the replacement head, and move the current pointer. fn_transition_shift may update only the shift status and insert its transition row. Each procedure sets one exact transaction-local context value; each trigger accepts only the context that names its allowed columns. No procedure accepts arbitrary column names or SQL text.

If a report insert, immutable trigger, policy check, anomaly calculation, audit append, or outbox insert fails, the complete business transaction rolls back. No partial report is visible.

### 3.3 Amendment lifecycle

amendments.status is pending | approved | rejected | superseded.

- pending -> approved creates exactly one new report and sets applied_report_id.
- pending -> rejected requires rejection_reason.
- pending -> superseded is used when a new amendment replaces a pending amendment for the same base version.

Approval locks the station, shift, amendment, and current report in the global order in §8. The base report must be the current report and must have status submitted or locked. It checks that base_report_id = shifts.current_report_id, checks stale_check_hash, checks requester/approver separation, clones all report rows, applies the allowlist, re-evaluates anomaly and evidence rules, and moves the current pointer. A stale base returns 409 and stays pending until the requester submits a new amendment.

Allowed amendment paths are exactly:

- sales_declared.cash_amount and sales_declared.cashless_amount;
- loss_entries.liters, loss_entries.cash_amount, and loss_entries.note;
- references to existing deliveries and dip_readings.

Meter fields and meter source fields are never allowed. The requester cannot amend data created by the requester, unless break-glass is used with a reason. Polymorphic amendment targets are checked by fn_approve_amendment; the target must exist in the base report snapshot and must have the same tenant and shift. old_value and new_value are jsonb values, and old_value must equal the base snapshot value before the new value is applied. stale_check_hash is SHA-256 of the versioned RFC 8785 canonical base-report payload plus version_no. Static SQL foreign keys cannot express this check.

### 3.4 Acknowledgement lifecycle and cardinality

An acknowledgement decision is acked or rejected. A rejected decision remains the active decision until it is superseded. The active decision is the one referenced by ack_head.active_ack_id; it is not inferred from MAX(ack_seq).

The schema is:

    ack_decisions(
      ack_id uuid primary key,
      org_id uuid not null, station_id uuid not null, shift_id uuid not null,
      report_id uuid not null, version_no integer not null,
      ack_seq bigint not null, decision ack_decision not null,
      actor_user_id uuid not null, decided_at timestamptz(6) not null,
      rejection_reason text null,
      is_superadmin boolean not null, is_break_glass boolean not null,
      break_glass_reason text null,
      unique(org_id, station_id, shift_id, report_id, version_no, ack_id),
      unique(org_id, station_id, shift_id, report_id, version_no, ack_seq),
      foreign key(org_id, station_id, shift_id, report_id, version_no)
        references shift_reports(org_id, station_id, shift_id, report_id, version_no)
    )

    ack_head(
      org_id uuid not null, station_id uuid not null, shift_id uuid not null,
      report_id uuid not null, version_no integer not null,
      active_ack_id uuid null,
      primary key(org_id, station_id, shift_id, report_id, version_no),
      foreign key(org_id, station_id, shift_id, report_id, version_no)
        references shift_reports(org_id, station_id, shift_id, report_id, version_no),
      foreign key(org_id, station_id, shift_id, report_id, version_no, active_ack_id)
        references ack_decisions(org_id, station_id, shift_id, report_id, version_no, ack_id)
        deferrable initially deferred
    )

ack_head is inserted with active_ack_id = NULL with every report. This gives one and only one head row for every report version by a primary key. The second foreign key gives declarative membership: a non-null active pointer can reference only a decision for the same tenant, station, shift, report, and version.

fn_ack_shift locks the station, shift, current report, and head. It rejects a non-null head, allocates ack_seq under the report lock, inserts one decision, updates the head, and changes the shift to locked or needs_correction. A deferred constraint trigger requires exactly one non-null head for the current report when the shift enters locked or needs_correction, and requires a null head before an acknowledgement. A second deferred trigger requires every superseded acknowledgement to have one ack_supersessions row and every replacement to be the next report version. Direct DML is denied, so no procedure can create two active decisions.

ack_decisions and ack_head have BEFORE UPDATE OR DELETE triggers. ack_decisions are insert-only. ack_head changes are accepted only in fn_submit_shift, fn_ack_shift, or fn_approve_amendment with their exact transition context.

For an amendment of a locked report, fn_approve_amendment inserts ack_supersessions with the old full acknowledgement key and the new full report key, then sets the old head to NULL and creates the new head with NULL in one transaction. ack_supersessions.superseded_ack_id is unique. This is the only way to deactivate an acknowledgement.

## 4. Database model and tenant keys

All UUID identifiers are generated by the database. All money calculations use numeric, never floating point. Decimal values in JSON are strings. timestamptz columns used for audit and state events have precision 6.

### 4.1 Master data

The following tables exist:

- organizations(org_id PK, name, created_at).
- stations(org_id, station_id, timezone, created_at, primary key(org_id, station_id)). A station row is the station lock row.
- users(user_id PK, org_id, display_name, enabled, created_at).
- user_station_roles(org_id, station_id, user_id, role, primary key(org_id, station_id, user_id, role)).
- nozzles(org_id, station_id, nozzle_id, dispenser_id, meter_max numeric(10,1), primary key(org_id, station_id, nozzle_id)). meter_max >= 0.
- dispensers(org_id, station_id, dispenser_id, primary key(org_id, station_id, dispenser_id)).
- tanks(org_id, station_id, tank_id, primary key(org_id, station_id, tank_id)).
- nozzle_tank_map(map_id PK, org_id, station_id, nozzle_id, tank_id, valid_period tstzrange). An exclusion constraint prevents overlapping periods for one nozzle.
- dispenser_nozzle_map(map_id PK, org_id, station_id, dispenser_id, nozzle_id, valid_period tstzrange). Exclusion constraints prevent overlap per dispenser and per nozzle. One dispenser can have many nozzles; one nozzle has one active dispenser.
- dispenser_prices(price_id PK, org_id, station_id, nozzle_id, price numeric(14,0), valid_period tstzrange, created_by). An exclusion constraint prevents overlapping periods for one nozzle.

All writes to prices, maps, nozzle master data, reset events, and baseline pointers first lock the station row. Price and mapping are resolved when a shift opens. The shift stores an immutable shift_price_map_snapshot JSON document and its SHA-256 hash. The document contains the price, dispenser, nozzle, meter maximum, modulus, and mapping IDs used by the shift. Its schema is validated by fn_open_shift; a trigger blocks changes after insert.

### 4.2 Operational tables

- shifts(shift_id PK, org_id, station_id, station_seq bigint, supervisor_id, opened_at, closed_at, timezone_snapshot, business_date, original_event_date, shift_ke, backfilled, backfill_approver, backfill_approved_at, backfill_reason, status, current_report_id, shift_price_map_snapshot jsonb, shift_price_map_hash bytea, created_at). Unique keys are (org_id, station_id, shift_id), (org_id, station_id, station_seq), and the backfill partial key in §3.1. station_seq is immutable and allocated under the station lock.
- shift_drafts(draft_id PK, org_id, station_id, shift_id, owned_by, claim_token uuid, claim_expires_at timestamptz(6), status, updated_by, updated_at, revision integer, recovery_count integer). Unique (org_id, station_id, shift_id) enforces one draft for the life of a shift. Unique (org_id, station_id, draft_id) is the parent key for every draft child. Status is editing|submitting|submitted|failed|recovering.
- draft_readings(row_id PK, org_id, station_id, draft_id, nozzle_id, meter_start, meter_end, created_by, unique(org_id, station_id, draft_id, nozzle_id)).
- draft_sales(row_id PK, org_id, station_id, draft_id, dispenser_id, cash_amount numeric(14,0), cashless_amount numeric(14,0), created_by, unique(org_id, station_id, draft_id, dispenser_id)).
- draft_losses(row_id PK, org_id, station_id, draft_id, loss_id, direction, reason_code, liters numeric(8,2), cash_amount numeric(14,0), note, created_by). loss_id is the immutable logical loss identifier promoted to loss_identity.
- draft_evidence_staging(row_id PK, org_id, station_id, draft_id, loss_row_id, evidence_type, object_key, content_hash, size_bytes, mime, status, uploaded_by), where status is uploaded|verified|finalized.
- submit_idempotency(idem_id PK, org_id, station_id, shift_id, idempotency_key, request_hash, status, claim_token, attempt_count, lease_started_at, lease_expires_at, resulting_report_id, error_detail, created_at, updated_at). Status is in_progress|succeeded|failed; lease_expires_at is always absolute; unique (org_id, station_id, shift_id, idempotency_key).
- shift_reports(report_id PK, org_id, station_id, shift_id, version_no, supersedes_report_id, status, submitted_by, submitted_at, policy_snapshot_set_id). Unique (org_id, station_id, report_id), (org_id, station_id, shift_id, report_id, version_no), and (org_id, station_id, shift_id, version_no). supersedes_report_id has a same-shift trigger. There is no is_current.
- dispenser_readings(reading_id PK, org_id, station_id, shift_id, report_id, nozzle_id, meter_start, meter_end, price_used numeric(14,0), expected_sale_rupiah numeric(14,0), observed, is_carried_forward, source_shift_id, source_report_id, source_reading_id). Unique (org_id, station_id, shift_id, report_id, nozzle_id) and (org_id, station_id, shift_id, report_id, reading_id). is_carried_forward is true if and only if all source columns are non-null. The source has a composite FK to another reading and is checked by procedure to be the predecessor locked report for the same nozzle.
- sales_declared(sales_id PK, org_id, station_id, shift_id, report_id, dispenser_id, cash_amount numeric(14,0) not null, cashless_amount numeric(14,0) not null default 0, created_by). Unique (org_id, station_id, shift_id, report_id, dispenser_id) and a report FK.
- loss_identity(loss_id PK, org_id, station_id, created_by, created_at). Unique (org_id, station_id, loss_id) and insert-only.
- loss_entries(row_id PK, org_id, station_id, shift_id, report_id, version_no, loss_id, nozzle_id, direction, reason_code, liters numeric(8,2), cash_amount numeric(14,0), note, created_by). Unique (org_id, station_id, shift_id, report_id, loss_id) and (org_id, station_id, shift_id, report_id, row_id). It has composite FKs to the exact report version and to loss_identity.
- loss_exception(exception_id PK, org_id, station_id, shift_id, report_id, loss_id, reason, actor_user_id, created_at). Unique (org_id, station_id, shift_id, report_id, loss_id). It is insert-only and is forbidden when the report evidence mode is wajib.
- deliveries(delivery_id PK, org_id, station_id, shift_id, do_number, tank_id, liters, created_by) and dip_readings(dip_id PK, org_id, station_id, shift_id, tank_id, dip_liters, created_by). Both have composite shift FKs. delivery_snapshots and dip_snapshots copy source IDs into each report version and have same-tenant, same-shift source FKs.
- shift_transitions(transition_id PK, org_id, station_id, shift_id, from_status, to_status, actor_user_id, at, reason), insert-only.
- amendments(amendment_id PK, org_id, station_id, shift_id, base_report_id, reason, status, requester_user_id, approver_user_id, requested_at, decided_at, rejection_reason, applied_report_id, stale_check_hash, is_break_glass, break_glass_reason). Unique (org_id,station_id,amendment_id) is the parent key for amendment items. Unique pending base report is enforced by a partial unique index on (org_id, station_id, base_report_id) where status = pending. Base and applied report FKs are same-tenant and same-shift.
- amendment_items(item_id PK, amendment_id, org_id, station_id, shift_id, target_kind, target_logical_id, field, old_value jsonb, new_value jsonb). target_kind is sales_declared|loss_entry|delivery|dip_reading. A procedure checks target membership in the base snapshot and checks the field allowlist.
- meter_reset_events(reset_id PK, org_id, station_id, nozzle_id, old_value, new_value, effective_shift_id, reason, actor_user_id, approver_user_id, approved_at, status). Status is pending|approved. Unique (org_id, station_id, nozzle_id, effective_shift_id) where status = approved. A procedure checks old_value against the latest eligible locked reading and checks actor != approver.
- ack_supersessions(supersession_id PK, old_org_id, old_station_id, old_shift_id, old_report_id, old_version_no, superseded_ack_id, replacement_org_id, replacement_station_id, replacement_shift_id, replacement_report_id, replacement_version_no, reason, created_at). The old decision key is unique. A deferred trigger requires replacement_version_no = old_version_no + 1 and the replacement report to be current when the transaction commits.

report_id and version_no are always used together with org_id, station_id, and shift_id. No table uses an unscoped report or shift foreign key.

### 4.3 Complete tenant foreign-key catalog

Every foreign key below includes all available tenant columns. An organization-only parent uses org_id. A station-scoped parent uses org_id and station_id. CI queries pg_constraint and fails if a tenant-bearing FK is missing from this catalog or if a station-scoped FK omits either tenant column.

| Child columns | Parent key |
|---|---|
| stations.org_id | organizations(org_id) |
| users.org_id | organizations(org_id) |
| audit_chain_locks.org_id and audit_log.org_id | organizations(org_id) |
| user_station_roles.org_id,station_id | stations(org_id,station_id) |
| user_station_roles.user_id | users(user_id) plus trigger for same org_id |
| nozzles.org_id,station_id,dispenser_id | dispensers(org_id,station_id,dispenser_id) |
| dispensers.org_id,station_id and tanks.org_id,station_id | stations(org_id,station_id) |
| nozzle_tank_map.org_id,station_id,nozzle_id | nozzles(org_id,station_id,nozzle_id) |
| nozzle_tank_map.org_id,station_id,tank_id | tanks(org_id,station_id,tank_id) |
| dispenser_nozzle_map.org_id,station_id,dispenser_id | dispensers(org_id,station_id,dispenser_id) |
| dispenser_nozzle_map.org_id,station_id,nozzle_id | nozzles(org_id,station_id,nozzle_id) |
| dispenser_prices.org_id,station_id,nozzle_id | nozzles(org_id,station_id,nozzle_id) |
| shifts.org_id,station_id | stations(org_id,station_id) |
| shifts.supervisor_id | users(user_id) plus same-organization trigger |
| Every created_by, requester_user_id, approver_user_id, actor_user_id, submitted_by, and updated_by column | users(user_id) plus a same-organization and, when applicable, same-station trigger |
| shift_drafts.org_id,station_id,shift_id | shifts(org_id,station_id,shift_id) |
| shift_drafts.org_id,station_id,draft_id | unique shift_drafts(org_id,station_id,draft_id) |
| Every draft child org_id,station_id,draft_id | shift_drafts(org_id,station_id,draft_id) |
| draft_readings.org_id,station_id,nozzle_id | nozzles(org_id,station_id,nozzle_id) |
| draft_sales.org_id,station_id,dispenser_id | dispensers(org_id,station_id,dispenser_id) |
| submit_idempotency.org_id,station_id,shift_id | shifts(org_id,station_id,shift_id) |
| shift_reports.org_id,station_id,shift_id | shifts(org_id,station_id,shift_id) |
| shifts.org_id,station_id,shift_id,current_report_id | shift_reports(org_id,station_id,shift_id,report_id) |
| shift_reports.org_id,station_id,shift_id,supersedes_report_id | shift_reports(org_id,station_id,shift_id,report_id) |
| dispenser_readings.org_id,station_id,shift_id,report_id | shift_reports(org_id,station_id,shift_id,report_id) |
| dispenser_readings source columns | dispenser_readings(org_id,station_id,source_shift_id,source_report_id,source_reading_id) |
| sales_declared.org_id,station_id,shift_id,report_id | shift_reports(org_id,station_id,shift_id,report_id) |
| loss_entries.org_id,station_id,shift_id,report_id,version_no | shift_reports(org_id,station_id,shift_id,report_id,version_no) |
| loss_entries.org_id,station_id,loss_id | loss_identity(org_id,station_id,loss_id) |
| loss_exception.org_id,station_id,shift_id,report_id,loss_id | loss_entries(org_id,station_id,shift_id,report_id,loss_id) |
| delivery_snapshots and dip_snapshots report columns | shift_reports(org_id,station_id,shift_id,report_id) |
| delivery_snapshots.org_id,station_id,shift_id,delivery_id | deliveries(org_id,station_id,shift_id,delivery_id) |
| dip_snapshots.org_id,station_id,shift_id,dip_id | dip_readings(org_id,station_id,shift_id,dip_id) |
| ack_decisions report columns | shift_reports(org_id,station_id,shift_id,report_id,version_no) |
| ack_head report columns | shift_reports(org_id,station_id,shift_id,report_id,version_no) |
| ack_head active decision columns | ack_decisions(org_id,station_id,shift_id,report_id,version_no,ack_id) |
| ack_supersessions old decision columns | ack_decisions(org_id,station_id,shift_id,report_id,version_no,ack_id) |
| ack_supersessions replacement report columns | shift_reports(org_id,station_id,shift_id,report_id) |
| policy_snapshot_sets.org_id,station_id,shift_id | shifts(org_id,station_id,shift_id) |
| policy_snapshot_items set columns | policy_snapshot_sets(org_id,station_id,shift_id,set_id) |
| policy_snapshot_items.org_id,rev_id | threshold_policy_revisions(org_id,rev_id) or evidence_policy_revisions(org_id,rev_id), selected by policy_kind |
| shift_reports.org_id,station_id,shift_id,policy_snapshot_set_id | policy_snapshot_sets(org_id,station_id,shift_id,set_id) |
| evidence_event report and loss-row columns | loss_entries(org_id,station_id,shift_id,report_id,row_id) |
| threshold_policy_revisions.org_id,station_id | organizations(org_id) and stations(org_id,station_id) when station_id is not null |
| evidence_policy_revisions.org_id,station_id | organizations(org_id) and stations(org_id,station_id) when station_id is not null |
| evidence_policy_types.org_id,rev_id | evidence_policy_revisions(org_id,rev_id) |
| policy revision supersedes_org_id,supersedes_rev_id | same policy revision parent, with trigger requiring an older valid_from |
| amendments base report columns | shift_reports(org_id,station_id,shift_id,report_id) |
| amendments.applied_report_id | shift_reports(org_id,station_id,shift_id,report_id) |
| amendment_items.org_id,station_id,amendment_id | amendments(org_id,station_id,amendment_id) |
| meter_reset_events nozzle and effective shift columns | nozzles(org_id,station_id,nozzle_id) and shifts(org_id,station_id,shift_id) |
| nozzle_baseline_revisions.org_id,station_id,nozzle_id | nozzles(org_id,station_id,nozzle_id) |
| nozzle_baseline_current.org_id,station_id,nozzle_id | nozzles(org_id,station_id,nozzle_id) |
| nozzle_baseline_current revision columns | nozzle_baseline_revisions(org_id,station_id,nozzle_id,baseline_rev_id) |
| sessions.kid | jwt_keys(kid) |
| alert_rules.org_id,station_id | stations(org_id,station_id) |
| alert_events.org_id,station_id | stations(org_id,station_id) |
| alert_events.org_id,station_id,rule_id | alert_rules(org_id,station_id,rule_id) |
| alert_events.related_fired_event_id | alert_events(org_id,station_id,event_id), checked by deferred trigger for fired type and same subject |
| audit_outbox | audit_log(org_id,event_id) |
| outbox_relay_state | audit_outbox(org_id,event_id) |
| sessions.user_id | users(user_id) plus trigger for same org |

The old acknowledgement columns in ack_supersessions are named old_org_id, old_station_id, old_shift_id, old_report_id, old_version_no, superseded_ack_id. The table has the exact FK (old_org_id, old_station_id, old_shift_id, old_report_id, old_version_no, superseded_ack_id). This explicit name avoids an accidental duplicate or tenant omission.

### 4.4 Policy revisions and immutable snapshots

threshold_policy_revisions and evidence_policy_revisions are append-only revision tables. Each has rev_id PK, policy_id, org_id, nullable station_id, valid_from, supersedes_org_id, supersedes_rev_id, disabled, created_by, and created_at, plus unique (org_id, rev_id). A partial unique index enforces one revision at one time for an organization scope and for a station scope. An insert trigger enforces one successor per revision and that supersedes_rev_id points backward. BEFORE UPDATE OR DELETE triggers always raise 23514. No old row is updated. A disabled revision is a tombstone.

Threshold revisions have these concrete fields: loss_liter_threshold, gain_liter_threshold, loss_rupiah_threshold, gain_rupiah_threshold, variance_rupiah_threshold, and rollover_threshold. The default loss-liter value is 10.00.

Evidence revisions have mode (opsional|wajib) with CHECK mode in (opsional,wajib), and child rows in evidence_policy_types(org_id, rev_id, evidence_type, minimum_count_per_loss, accepted_mime_types). The child has PRIMARY KEY (org_id,rev_id,evidence_type), minimum_count_per_loss >= 0, a non-empty type, and a non-empty MIME array. Each MIME value matches the server MIME grammar. The child row is the complete accepted-type definition. A trigger requires at least one accepted type; wajib requires at least one positive minimum.

Resolution is as-of resolution_time: choose the newest valid_from <= resolution_time, with station scope before organization scope. Do not use a future revision to hide a current revision. Threshold and evidence policies resolve at submit. Price resolves at shift open.

policy_snapshot_sets(set_id PK, org_id, station_id, shift_id, created_at) has unique (org_id,station_id,shift_id,set_id). policy_snapshot_items(item_id PK, org_id, station_id, shift_id, set_id, policy_kind, policy_id, rev_id, scope, payload jsonb, payload_hash) has PRIMARY KEY item_id, unique (org_id,station_id,shift_id,set_id,policy_kind), and exactly one threshold and one evidence item per set. The threshold payload must contain exactly the six threshold keys and hash_version. The evidence payload must contain exactly mode, types, and hash_version; each type object must contain type, minimum_count_per_loss, and sorted accepted_mime_types. Unknown keys are rejected. The hash is SHA-256 of the versioned canonical JSON bytes.

A deferred constraint trigger requires both items before a report can reach awaiting_confirmation. After any report references a set, a trigger rejects UPDATE and DELETE of its set or items. The report stores the set ID and the full snapshot remains readable for printout and audit.

## 5. Submit, chronology, meter, and numeric rules

### 5.1 Submit idempotency and draft fencing

fn_submit_shift takes a canonical request payload and an idempotency key. The canonical hash is SHA-256 of RFC 8785 JSON without the idempotency key. Field order, decimal strings, null values, sorted arrays, and hash_version are fixed by the repository JSON Schema. Unknown fields are rejected.

The one protocol is:

1. Lock station, then shift, then draft, then the idempotency row in the global order. Insert the row with ON CONFLICT DO NOTHING; the insert winner owns the claim.
2. A row with a different hash always returns 409.
3. A succeeded row returns the stored report after a fresh authorization check.
4. An unexpired in_progress row returns 409.
5. An expired in_progress row is taken over by conditional UPDATE ... WHERE claim_token = old_token AND lease_expires_at < now() RETURNING. Zero rows returns 409.
6. A failed row always returns 409; retry requires a new idempotency key, even when the payload is identical.
7. The lease is 10 minutes. A worker heartbeat runs every 60 seconds and extends lease_expires_at only when its claim token still matches and the row is in_progress.
8. Every draft-child mutation and submit checks claim_token, claim_expires_at > now(), and the expected revision in one conditional UPDATE. An expired client cannot write.
9. Report creation and setting succeeded are one transaction. If final claim fencing updates zero rows, the whole report transaction rolls back. Setting failed is a separate claim-gated transaction after rollback.

A recovery job moves submitting drafts older than 10 minutes to recovering, checks for a report and submit result, then moves the draft to submitted if a report exists, otherwise to editing or failed. It increments recovery_count and writes an audit event.

### 5.2 Station chronology and baseline

shifts.station_seq is allocated by locking the station and taking the next monotonic value. It is immutable. It is the only chronology key. UUID order and local time order are never used for predecessor selection.

For one station and nozzle, the eligible meter source for a target shift is the chronologically latest of:

- the locked report reading of the preceding shift;
- an approved meter reset whose effective_shift_id has a lower station_seq than the target;
- the current baseline revision.

The latest eligible source by station_seq wins. A reset cannot be approved without either a preceding locked report for that nozzle or an explicit provisioning baseline. A reset stores old_value, new_value, effective_shift_id, reason, actor, approver, approved_at, and status. Actor and approver must differ. Approval uses the station lock and the station/nozzle lock.

Baseline uses append-only nozzle_baseline_revisions(baseline_rev_id PK, org_id, station_id, nozzle_id, effective_station_seq bigint default 0, initial_meter_value, meter_max, created_by, created_at) and nozzle_baseline_current(org_id, station_id, nozzle_id, current_baseline_rev_id). The revision has unique (org_id,station_id,nozzle_id,baseline_rev_id). The pointer has a four-column FK to the revision, so it cannot point to another nozzle. Owner approval creates a new revision and moves the pointer in one transaction. A revision checks 0 <= initial_meter_value <= meter_max. Provisioning baseline has effective_station_seq = 0.

Meter values are numeric(10,1). meter_max is the display maximum. modulus = meter_max + 0.1. For a rollover, delta = meter_end - meter_start + modulus; it is valid only when both values are in range, the delta is at most rollover_threshold (default 20% of modulus), and there is one rollover only. A non-rollover negative delta and a second rollover are rejected.

Amendment never changes meter fields in a submitted or locked report. Meter correction uses a new shift or an approved reset.

### 5.3 Reconciliation and numeric limits

Volume is per nozzle and then summed by shift. Loss and gain are separate:

- loss anomaly: sum of loss_entries.liters where direction = loss is greater than loss_liter_threshold;
- gain anomaly: sum where direction = gain is greater than gain_liter_threshold.

Gain does not offset loss. cash_amount is optional on a loss row. When present, loss and gain Rupiah claims use their own thresholds. DO and dip values are informative in v1.

Money variance is:

sum(nozzle expected_sale_rupiah) - sum(dispenser cash_amount + cashless_amount).

expected_sale_rupiah = ROUND(meter_delta * price, 0) with deterministic half-up rounding per reading. The sum uses the rounded reading values. A negative variance is stored and compared by absolute value to variance_rupiah_threshold, whose default is zero.

All volumes are numeric(8,2) with >= 0. Volume sums use numeric(12,2). Money and price use numeric(14,0). The maximum is 99,999,999,999,999. Before every cast, multiplication, sum, and variance operation, the procedure checks overflow. Overflow raises SQLSTATE 22003, rolls back the transaction, and maps to HTTP 422. JSON and TypeScript use decimal strings.

### 5.4 Backfill

Backfill uses the same shift lifecycle. It cannot be inserted out of order when a later locked report has a chained nozzle reading or when an active draft would be invalidated. Only non-meter claims can be entered. Meter readings use the predecessor snapshot with observed = false and is_carried_forward = true. Approval uses the station lock and stores all approval fields in shifts.

## 6. Evidence and anomaly rules

At submit and amendment approval, fn_validate_evidence reads the evidence policy snapshot of that report. It does not read a mutable live policy row. For every loss row it:

1. rejects an evidence type not present in the snapshot types array;
2. rejects a MIME type not listed for that type;
3. counts distinct finalized evidence_id values per type and checks minimum_count_per_loss;
4. rejects loss_exception when mode is wajib;
5. requires exactly one exception when mode is opsional and the loss has no finalized evidence, and rejects an exception when finalized evidence exists.

The same checks run in a deferred constraint trigger on loss_exception and report finalization. The trigger is a safety net; the procedures are the normal write path.

evidence_event is append-only. Its columns are (evidence_event_id PK, evidence_id, org_id, station_id, shift_id, report_id, loss_row_id, event_seq, event_type, evidence_type, object_key, content_hash bytea, size_bytes bigint, mime, actor_user_id, at). CHECK constraints require event_seq >= 1, content_hash length = 32, size_bytes > 0, a non-empty object_key, and event_type in (uploaded,verified,finalized). A unique key (org_id,station_id,shift_id,report_id,loss_row_id,evidence_id,event_seq) prevents duplicate sequence numbers. The allowed event sequence is uploaded -> verified -> finalized; the procedure locks the loss row, checks the previous event, and inserts the next sequence. Direct insert, update, and delete are denied. Object storage is write-once: a hash collision with different bytes is rejected.

alert_rule_type has values starvation|variance. alert_channel has value in_app. alert_subject_kind has values shift|report. alert_event_type has values fired|cleared. alert_source_kind has values shift_transition|report|scheduler|amendment. alert_rules contains (rule_id uuid PK, org_id uuid, station_id uuid, rule_type alert_rule_type, alert_key text, threshold numeric, enabled boolean, channel alert_channel, created_by uuid, created_at timestamptz(6)) with unique (org_id,station_id,rule_id) and unique (org_id,station_id,alert_key). CHECK constraints allow threshold >= 0 and enabled true|false. Alert starvation is mandatory. Variance alerts are opt-in. channel is in_app in the MVP; external notification is deferred.

alert_events contains:

    (event_id uuid PK, org_id uuid, station_id uuid, rule_id uuid,
     subject_kind alert_subject_kind, subject_id uuid,
     event_type alert_event_type, period_start timestamptz(6) NOT NULL,
     period_bucket timestamptz(6) GENERATED ALWAYS AS
       (date_bin(interval '1 hour', period_start,
                 timestamptz '1970-01-01 00:00:00+00')) STORED,
     related_fired_event_id uuid NULL, source_kind alert_source_kind,
     source_id uuid, source_version_no integer NULL,
     source_at timestamptz(6), created_by uuid NULL,
     created_at timestamptz(6) NOT NULL)

event_type is fired|cleared. period_bucket is the immutable UTC expression date_bin('1 hour', period_start, '1970-01-01 00:00:00+00'). CHECK constraints require event_type in (fired,cleared), source_kind in (shift_transition,report,scheduler,amendment), source_id non-null, and source_at non-null. A fired row has related_fired_event_id = event_id. A cleared row has a non-null related fired ID. A deferred trigger checks that the related event is a fired event for the same tenant, rule, subject, and period bucket. The same trigger checks that source_id exists in the table named by source_kind and has the event tenant. Unique constraints are:

- (org_id,station_id,rule_id,subject_kind,subject_id,event_type,period_bucket) for one event of each type in a bucket;
- (org_id,station_id,related_fired_event_id,event_type) for one clear per fired event.

The source columns are mandatory for both event types. source_kind is shift_transition|report|scheduler|amendment; source_id identifies the source row, and source_version_no is required for report and amendment sources. The occurrence procedure locks the alert rule row before it inserts a fired or cleared event. Retries use the unique keys and do not create duplicates.

alert_events has BEFORE UPDATE OR DELETE triggers that always raise 23514. A deferred trigger requires source_version_no to be null for scheduler and shift_transition sources and non-null for report and amendment sources. It also requires a fired row to self-reference its own event_id and a cleared row to reference a fired row.

## 7. Authentication, RLS, and privileges

The only actor identity is a verified signed session JWT. Required claims are iss, sub, jti, iat, exp, aud = spbu-recon, kid, and alg = HS256. The verifier rejects other algorithms, missing claims, invalid signature, wrong issuer or audience, future iat, expired exp outside 60 seconds skew, and a jti that is absent or revoked in sessions.

sessions(jti PK, kid, issued_at, expires_at, revoked_at) checks expires_at - issued_at <= interval 15 minutes. jwt_keys(kid PK, secret_ref, status, activated_at, retired_at, max_token_expiry) uses active|previous|retired. A previous key is retired only when max_token_expiry <= now(). Logout sets revoked_at. Raw JWT text is never stored in audit.

The API passes the raw JWT to fn_set_request_context. That SECURITY DEFINER function verifies it in the database, checks sessions, sets transaction-local values with set_config(..., true), and sets tenant, user, role, and break-glass context. A missing or invalid context fails closed.

All tenant tables have RLS and FORCE ROW LEVEL SECURITY. Table owners and service roles are NOLOGIN NOBYPASSRLS. Policies allow rows only when org_id and, where applicable, station_id match the transaction-local context. The break-glass policy branch requires a verified Owner/Superadmin context and a non-empty reason.

The app role has no SELECT, INSERT, UPDATE, DELETE, or TRUNCATE on any table, sequence, view, or materialized view. The app reads only through SECURITY DEFINER functions named read_*; each function verifies context and has a fixed search_path. Views are not exposed. Any internal view is security_invoker.

The relay role is NOLOGIN and NOBYPASSRLS. A role-specific RLS policy permits it to read audit_outbox and read or update outbox_relay_state only through relay claims. The only relay grant is EXECUTE on fn_relay_claim_event and fn_relay_finish_event; both functions check the lease token. No relay function can insert or update audit_log or audit_outbox.

Write functions are the only API surface. Every procedure is listed in procedure_registry(name PK, allowed_roles, action, lock_rank, enabled). CI compares this table with pg_proc and fails for an unclassified procedure, function, sequence, table, view, materialized view, or role. Grants are explicit and default privileges are revoked.

## 8. Lock order and concurrency

Every transaction that takes more than one row lock uses this total order. It never uses an unranked resource:

| Rank | Resource tables |
|---:|---|
| 5 | organizations |
| 10 | stations |
| 20 | shifts |
| 21 | shift_transitions |
| 30 | shift_drafts, draft_readings, draft_sales, draft_losses, draft_evidence_staging |
| 40 | submit_idempotency |
| 45 | policy_snapshot_sets |
| 46 | policy_snapshot_items |
| 47 | threshold_policy_revisions, evidence_policy_revisions |
| 48 | evidence_policy_types |
| 49 | amendments, amendment_items |
| 50 | shift_reports |
| 51 | dispenser_readings, sales_declared, loss_entries, loss_exception, delivery_snapshots, dip_snapshots, evidence_event |
| 52 | ack_head |
| 53 | ack_decisions |
| 54 | ack_supersessions |
| 70 | alert_rules |
| 71 | alert_events |
| 80 | nozzles, dispensers, tanks, dispenser_prices, nozzle_tank_map, dispenser_nozzle_map, meter_reset_events, nozzle_baseline_revisions, nozzle_baseline_current, deliveries, dip_readings |
| 90 | loss_identity |
| 100 | audit_chain_locks, audit_log, audit_denied |
| 110 | audit_outbox, outbox_relay_state, sessions, jwt_keys, procedure_registry |
| 120 | users, user_station_roles |

The DDL manifest stores one lock_rank for every listed table, including organizations, stations, all operational tables, policy tables, alert tables, master tables, audit tables, authentication tables, and user tables. For multiple rows at one rank, sort by table_name and primary_key ascending. A transaction never locks a lower rank after a higher rank. Submit is station -> shift -> draft -> idempotency -> policy snapshot -> report -> report children -> ack head. The station and shift locks are required for chronology and are taken before the draft lock. Acknowledgement is station -> shift -> current report -> ack head -> decision. Amendment is station -> shift -> amendment -> current report -> new report children -> old and new acknowledgement heads. Audit append is last. CI runs deadlock tests with at least three concurrent workers for submit takeover, acknowledgement, amendment, reset, policy snapshot, and alert clear.

## 9. Audit chain and relay

audit_log is owned by a dedicated audit_owner role. The app has no direct table privilege. fn_append_audit_event is SECURITY DEFINER, has a fixed search_path, and locks the permanent audit_chain_locks(org_id PK) row.

The audit columns are (event_id UUID PK, org_id, org_sequence bigint, event_type, payload jsonb, outcome, outcome_error, created_at timestamptz(6), prev_hash bytea, row_hash bytea). The table has UNIQUE(org_id,event_id) and UNIQUE(org_id,org_sequence). The chain starts at org_sequence = 1 with 32 zero bytes as prev_hash.

The exact hash bytes are UTF-8 bytes of RFC 8785 canonical JSON for this array, in this order:

    [hash_version, org_id_text, org_sequence_decimal_text, event_id_lowercase_text,
     event_type, payload, created_at_utc_text, prev_hash_lowercase_hex]

created_at_utc_text is the database value formatted as YYYY-MM-DDTHH24:MI:SS.USZ in UTC, with exactly six fractional digits. The database assigns created_at once at append. UUIDs are lowercase text. org_sequence is decimal text. prev_hash is 32 bytes and its hash-input form is lowercase hexadecimal. payload uses the versioned canonical schema; decimals and big integers are strings, arrays have fixed sort keys, null is allowed only where specified, and unknown fields are rejected. row_hash = SHA-256(input_bytes).

The business mutation, chain append, and one audit_outbox row are one database transaction. If the chain lock, sequence allocation, canonicalization, hash, or outbox insert fails, the transaction raises and all business writes roll back. A serialization failure retries the complete transaction, never the audit append alone. The relay never writes chain rows.

audit_outbox(org_id,event_id, event_type, payload, created_at) has PRIMARY KEY (org_id,event_id) and a composite FK to audit_log(org_id,event_id). It has an insert-only trigger. outbox_relay_state(org_id,event_id, relay_status, attempt_count, lease_token, lease_expires_at, last_attempt_at, next_attempt_at, delivered_at, last_error) has a composite PK and FK to the same outbox event. This is the event-state correlation key.

The relay role has SELECT on audit_outbox and SELECT, UPDATE only on outbox_relay_state. It has no insert, update, or delete privilege on audit_log or audit_outbox. A relay claim locks the state row, sets a five-minute lease, and sends the same event_id to the sink. Success is marked only by a matching lease token. Failure increments attempt_count, keeps the outbox row, and sets a bounded exponential next_attempt_at. A retry sends the same event ID and the sink must be idempotent on that ID. A relay crash leaves an expired lease for takeover; it never creates a second audit chain row.

Denied or failed requests are durable in audit_denied, which is append-only and not part of the chain. The API calls the separate audit-writer connection before returning a denial or a failed-submit response. request_id is unique. The writer has a three-second timeout; if its commit fails, the API returns no success or denial response and the request remains failed closed. audit_denied contains request_id, sub, jti, tenant, action, target, reason, server timestamp, outcome, and error detail.

## 10. UI and printout

The screens are:

1. Masuk
2. Dasbor Supervisor
3. Form Input Shift
4. Input DO dan Dip
5. Antrean Perubahan
6. Antrean Konfirmasi
7. Daftar Anomali
8. Laporan dan Cetak
9. Manajemen Pengguna, Dispenser, Nozzle, Harga, dan Atur Ulang Meter
10. Pengaturan Kebijakan
11. Jejak Audit

All buttons, validation messages, empty states, and print labels are Bahasa Indonesia. Printout and report screen use the same immutable report snapshot.

## 11. Acceptance criteria

1. Cross-midnight, late submit, daylight-saving, timezone change, and backfill tests produce the correct business_date from the shift timezone snapshot.
2. A report locks only after an active acknowledgement from a different data creator. Break-glass requires a reason, audit, and anomaly entry.
3. Loss, gain, and Rupiah variance are separate anomalies. A negative Rupiah variance is retained.
4. Report immutability is enforced by database triggers. Amendment uses only the allowlist, requester/approver separation, one pending amendment per version, atomic stale check, same-shift applied report, and re-acknowledgement.
5. Price, mapping, meter maximum, and modulus are server-resolved at shift open and frozen in the snapshot.
6. Chaining, rollover, reset approval, baseline revision, and cross-nozzle isolation pass concurrency tests.
7. Every tenant FK is composite and catalogued. RLS and negative tests protect API, export, and jobs.
8. Policy revisions are append-only, snapshots are complete and immutable, station scope overrides organization scope, and disabled revisions are tombstones.
9. Evidence wajib rejects missing per-loss evidence and rejects exceptions. Evidence opsional requires an exception when evidence is absent.
10. Audit hash verification reads the chain by org_sequence and finds no gap. A chain append failure rolls back the business mutation. Relay retries do not duplicate events.
11. At most one active shift exists per station; needs_correction does not block a new shift; scheduler abandonment is idempotent.
12. Failed submit recovery, idempotency takeover, same-key hash mismatch, and new-key retry rules pass.
13. Numeric boundary, half-up rounding, overflow 22003 -> 422, and JSON decimal-string tests pass.
14. UI copy is Bahasa Indonesia. Printout equals the report screen.

## 12. Technology and delivery

Technology: Next.js, TypeScript, PostgreSQL, Drizzle, and btree_gist exclusion constraints. Deployment can use VPS or Vercel with managed PostgreSQL. Use TDD and keep all code in git.

Phases:

- F1: schema, composite FKs, exclusion constraints, RLS, roles, JWT, procedure registry, and negative tests.
- F2: master data, price and mapping snapshot, drafts, lifecycle, station chronology, meter, baseline, reset, policy versioning, and atomic idempotent submit.
- F3: amendment, re-acknowledgement, break-glass, audit chain, and outbox relay.
- F4: dual-unit reconciliation, anomalies, and alert scheduler.
- F5: acknowledgement queue, backfill, reports, printout, and settings.
- F6: family-station pilot.

Pilot gates include 12 complete normal shifts, one amendment and re-ack cycle, one reasoned break-glass action, one reset, one required-evidence rejection, one optional-evidence exception, one price change between shifts, one starvation alert, three-iteration concurrency tests, JWT rotation tests, invalid transition tests, audit export verification, backup/restore with RTO under one hour and RPO at most five minutes, and a green tenant-isolation matrix. Rollback is return to Excel; application data remains available for later review.
