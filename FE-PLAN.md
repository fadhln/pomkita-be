# FE-PLAN.md — Frontend Implementation Plan (Next.js + TanStack Query + Base UI + Emotion)

Status: execution plan. Source of truth for requirements: PLAN.md (SSOT). This file breaks the frontend into phased, TDD-verified work.
Technical text uses ASD-STE100 Simplified Technical English.

## 0. Repository

New repository: `pomkita-fe`. Next.js (App Router), TypeScript, TanStack Query v5 for server state, Base UI for primitives, Emotion for styling. The FE is a pure client of the Gin API. It holds no business rules; it renders server state and posts canonical payloads.

Layout:

    src/app/            routes (App Router) per screen of PLAN.md §10
    src/features/       one module per domain: shift, ack, amendment, anomaly, report, admin, policy, audit
    src/lib/api/        typed API client: fetch wrapper, error mapping (401/403/404/409/422), request-id header
    src/lib/query/      TanStack Query client, query keys per domain, retry policy (idempotent GET only)
    src/lib/jwt/        token storage (httpOnly cookie set by BE preferred; memory fallback), silent re-login
    src/components/     Base UI wrappers (Button, Dialog, Table, Form fields), Emotion theme (light, ID-first)
    src/i18n/           Indonesian copy constants — ALL user-facing text; no inline strings in components
    test/               Playwright e2e + Vitest unit; msw for API mocking

Conventions:

- All user-facing copy is Bahasa Indonesia, from src/i18n constants only. A CI check greps components for hardcoded strings.
- Server state only through TanStack Query. Mutations call the API client; on 409 with a retryable cause, the mutation surfaces the server error, it never retries writes automatically.
- Rupiah and liter values render from decimal strings. No float parsing for money. Formatting: `Intl.NumberFormat("id-ID")`.
- Forms use server validation as the authority; client validation is convenience only (required, shape). On 422, field errors from the API body map to inputs.
- Time: render in the station timezone from the API payload (the payload carries the snapshot timezone); never local-device time.

## 1. Screens (PLAN.md §10) mapped to routes

| # | Screen (ID copy) | Route | Roles |
|---|---|---|---|
| 1 | Masuk | /login | all |
| 2 | Dasbor Supervisor | / | supervisor |
| 3 | Form Input Shift | /shift/aktif | supervisor |
| 4 | Input DO dan Dip | /shift/aktif/do-dip | supervisor |
| 5 | Antrean Perubahan | /perubahan | admin/owner |
| 6 | Antrean Konfirmasi | /konfirmasi | admin/owner |
| 7 | Daftar Anomali | /anomali | admin/owner |
| 8 | Laporan dan Cetak | /laporan, /laporan/[id] | all (scoped) |
| 9 | Manajemen Pengguna, Dispenser, Nozzle, Harga, Reset Meter | /pengaturan/* | admin/owner/superadmin |
| 10 | Pengaturan Kebijakan | /kebijakan | admin/owner |
| 11 | Jejak Audit | /audit | owner/superadmin |

## 2. Phases

Each phase ends with its tests green, a commit, and a status row vs this file. E2E runs against the real BE (docker compose) with seeded data.

### Phase F0 — App shell, auth, design system

Delivers:

1. Next.js scaffold, Emotion theme, Base UI wrappers, layout with role-aware navigation.
2. /login: email + password against the BE session endpoint; session cookie handling; 401 redirect to login; inactivity handling per PLAN.md §7: the session dies after 15 minutes without a request. The FE tracks last-activity time itself (pointer, keyboard, and network activity events); on the next request after the idle window it expects 401 and performs a logout redirect to Masuk. There are no refresh tokens: activity keeps the session alive server-side via last_active_at, so no FE refresh call exists.
3. API client: typed endpoints, error mapping, request-id propagation, decimal-string passthrough.
4. TanStack Query setup: query keys per domain, staleTime defaults, no global retry on mutations.
5. i18n constants module; CI string check.

Tests (watch red first):

- unit: API client maps 401/403/404/409/422 correctly; decimal strings pass through unharmed.
- e2e: login success, bad password error (ID copy), expired session redirects to Masuk.
- e2e: navigation shows only permitted screens per role (supervisor sees no /audit).

Gate: BE Phase B0 must expose sessions before F0 e2e runs against it.

### Phase F1 — Shift entry (the core screen)

Delivers:

1. Dasbor Supervisor: active shift card (status, business_date, draft state), start-shift action (calls fn_open_shift via API), list of my recent shifts.
2. Form Input Shift: nozzle readings (meter_start, meter_end) with per-nozzle delta and rollover display warning; sales per dispenser (cash, cashless) with Rupiah input masks; losses/gains editor (direction, reason_code from API list, liters, optional cash_amount, note) with evidence upload when the station policy is wajib, and loss_exception reason when opsional and evidence absent.
3. Draft autosave: mutation per section with revision from the server; stale-draft conflict screen (409 -> "Muat ulang" flow); claim-expiry countdown and re-claim.
4. Input DO dan Dip screen: DO list and dip readings per tank, informative display.
5. Submit flow: confirmation dialog with the computed variance preview (from server read, not client math), submit with idempotency key (generated once per form session), progress state, failed -> retry with a NEW key per BE rule, success -> status awaiting_confirmation.
6. Offline-tolerant behavior: disable submit while a heartbeat lease refresh is in flight; show lease state.

Tests:

- e2e happy path: open shift -> enter readings/sales/loss -> submit -> sees Antrean Konfirmasi entry (as admin).
- e2e: rollover input (99999.9 -> 0.5) shows correct delta; over-threshold rollover shows server 422 mapped to the field.
- e2e: concurrent draft (second tab) shows stale conflict, not silent overwrite.
- e2e: submit network failure then retry uses a new idempotency key (assert via BE idempotency rows in test hook).
- unit: Rupiah mask formatting; loss row validation.

Gate: BE Phase B1 (submit protocol) live.

### Phase F2 — Ack queue, amendment, break-glass

Delivers:

1. Antrean Konfirmasi: list of awaiting_confirmation reports (scoped stations), report detail (read-only snapshot view), Ack / Tolak with mandatory reason on reject; requester self-ack is hidden and, if forced via API, the server 403 renders as an ID error banner.
2. Antrean Perubahan: pending amendments list; amendment request flow for supervisors (from a report view: pick allowed fields only — sales cash/cashless, loss liters/cash_amount/note, DO/dip reference; meter fields are not selectable), old/new value diff display.
3. Amendment approval: diff review, stale-base detection (409 -> "Dasar sudah berubah; ajukan ulang"), approve -> new version appears; reject with reason.
4. Break-glass UX: only for Owner/Superadmin, reason textarea mandatory (client-required, server-enforced), red banner marking the action, action appears in Daftar Anomali.
5. Needs_correction report view: shows rejection reason and links to the amendment request flow.

Tests:

- e2e: full ack cycle (submit as supervisor, ack as admin -> shift locked).
- e2e: reject -> needs_correction -> amendment request -> approval -> new version awaiting; supersession visible.
- e2e: self-ack attempt (same user both roles via test user) shows the 403 error.
- e2e: stale amendment 409 flow.
- unit: diff renderer for amendment items.

Gate: BE Phase B2 live.

### Phase F3 — Anomalies, alerts, backfill, audit view

Delivers:

1. Daftar Anomali: table (union feed: variance, loss exceptions, break-glass, starvation) with filters (station, date, type), anomaly detail linking to the report snapshot.
2. Alert indicators in the dasbor (in_app channel): open starvation warning, unread alerts count.
3. Backfill flow (Owner): pick date + shift_ke, Owner approval step with reason, guarded entry form (non-meter claims only; meter shown as carried-forward read-only), out-of-order rejection message from 409.
4. Jejak Audit: filterable audit trail list, chain-verified badge from the BE verification endpoint, export button (CSV download from BE).
5. Pengaturan Kebijakan: threshold and evidence policy editors producing NEW revisions (never edits), with effective_from datetime, tombstone/disable action, and a preview of the resolution result (which revision applies now).

Tests:

- e2e: starvation alert appears after the BE scheduler marks a shift (test hook advances time).
- e2e: backfill creates a flagged report; retry does not duplicate (assert one report).
- e2e: audit list renders, verify badge present, CSV downloads.
- e2e: policy revision creates a new row; tombstone hides it from resolution.
- unit: variance display (negative Rupiah variance shown as negative, abs compared to threshold note).

Gate: BE Phase B3 live.

### Phase F4 — Reports, printout, admin screens, pilot polish

Delivers:

1. Laporan dan Cetak: report list + detail (immutable snapshot render), print stylesheet (@media print) so the printed page equals the screen data exactly (same snapshot payload, print-only CSS), print button uses window.print with a dedicated print route.
2. Pengaturan screens: users + user_station_roles management (Owner), dispenser/nozzle/tank master data with exclusion-conflict error display (409 overlap), dispenser_prices with effective periods and overlap 409 handling, meter reset request/approval flow (actor != approver enforced by server; UI enforces role split), baseline provisioning.
3. Empty states, loading skeletons, and ID error banners across all screens; accessible forms (labels, aria, Base UI semantics).
4. Lighthouse/perf pass: no blocking requests on shift entry; query prefetch on navigation.
5. Pilot runbook support: demo seed data toggle, "reset to seed" dev endpoint usage documented.

Tests:

- e2e: print output data equality (parse the print route DOM and compare fields to the API snapshot payload).
- e2e: price overlap 409 shows a conflict message naming the conflicting period.
- e2e: reset approval with the same actor shows the server 403.
- axe accessibility checks on the 11 screens.
- Full pilot-script e2e: the 12-shift day simulation from PLAN.md §12 gates, driven through the UI.

Gate: PLAN.md §11.14 (copy language, printout equality) plus pilot readiness.

## 3. Definition of done (every phase)

- Tests written first, watched red, then green (Vitest unit + Playwright e2e against the real BE).
- CI: lint (eslint + typescript strict), unit, e2e, i18n string check, build.
- No UI text outside src/i18n; no money math in client code (render only).
- All code committed; phase summary vs this file posted at the boundary.
- Any screen or behavior change that contradicts PLAN.md requires a PLAN.md (SSOT) update in the same commit.

## 4. Cross-cutting rules

- The client never computes authoritative values: meter deltas for display only (server recomputes), variance preview always from the server read, expected sale always from the server.
- Every mutation is idempotent-friendly: submit carries its idempotency key; all other mutations rely on server state-machine 409s and render them, never blind-retry.
- Tenant scoping is invisible: the API scopes by the session; the FE never sends org_id/station_id from client state for authorization (only for display).
- Sessions: BE-issued httpOnly cookie; no token in localStorage; CSRF protection per BE (same-site + custom header).
- Timezone: all timestamps sent as ISO 8601 UTC; display converts using the payload timezone snapshot.
