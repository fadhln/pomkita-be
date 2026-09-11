# Agent development guide

## Scope

This repository contains the PomKita backend. `../PLAN.md` is the product and security source of truth. `../BE-PLAN.md` is the backend delivery plan. Read both before changing behavior.

## Boundaries

- Keep HTTP transport in `internal/httpapi`.
- Keep database access in `internal/db`; handlers must not query tables directly.
- Put schema, functions, triggers, roles, grants, and RLS in ordered migrations.
- Treat PostgreSQL procedures as the enforcement layer. The Go service is transport and orchestration.
- Never use floating point for money or volume.
- Never log tokens, credentials, raw evidence, or personal data.
- Every state-changing endpoint must carry a request ID and map database errors to the API error contract.

## Agent workflow

1. Read the relevant SSOT section and identify the acceptance criterion.
2. State the contract and migration impact before editing.
3. Make one focused change. Do not mix refactors with behavior changes.
4. Write or update a failing test first when behavior is involved.
5. Run `make check` before handoff. Report any unavailable external dependency.
6. Keep commits small and named by slice, for example `B0: add session context`.

Do not add tables, procedures, roles, grants, or API fields outside the SSOT without updating the SSOT first.
