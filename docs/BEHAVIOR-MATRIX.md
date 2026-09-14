# Backend behavior matrix

This matrix records the current contract and the planned service boundary. It is
the Phase 0 baseline for the migration to Go services and repository ports.

| Area | Actor and rule | Success result | Denial or conflict | Current proof |
| --- | --- | --- | --- | --- |
| Authentication | A valid session token identifies the actor. | Session claims are available to the handler. | Missing, malformed, expired, revoked, or idle sessions return `401`. | JWT unit tests and HTTP middleware tests |
| Login | An enabled user sends valid credentials with CSRF protection. | An httpOnly session cookie is created. | Invalid credentials return one stable `401` code. | HTTP session tests and B0 integration tests |
| Logout | An authenticated actor requests logout with CSRF protection. | The session is revoked and the cookie is cleared. | An invalid session returns `401`. | HTTP session tests |
| Shifts | A permitted supervisor opens one active shift per station. | A shift sequence and price snapshot are created. | Duplicate active shifts and invalid state changes return a conflict. | B1 shift integration tests |
| Drafts | A claimant writes with an active lease and expected revision. | The draft child is stored and the revision increases. | An expired lease or stale revision is rejected. | B1 draft integration tests |
| Submission | A claimant submits a valid draft with an idempotency key. | One immutable report is created. | A changed request for the same key conflicts; invalid values return `422`. | B1 submit integration tests |
| Governance | Station Admin or Owner acknowledges; requester and approver are separate. | The report and shift move to the permitted next state. | Wrong role, self-approval, stale report, or invalid transition is denied. | B2 governance integration tests |
| Policies | Owner creates ordered policy revisions. | A revision is stored and is available to later snapshots. | Overlap, invalid ordering, and tombstone errors are rejected. | B5 policy integration tests |
| Evidence | Submission uses the report's evidence policy snapshot. | Required evidence is finalized and validated. | Missing wajib evidence or an invalid opsional exception is rejected. | B3 evidence integration tests |
| Alerts | A scheduler evaluates a rule for a shift state. | One fired or cleared event is recorded for a source version. | Repeating the evaluation does not duplicate the event. | B3 alert integration tests |
| Reporting | An authenticated actor reads only permitted tenant data. | Report, printout, audit, anomaly, and policy views are returned. | Missing context or another tenant's data is denied. | B4 reporting and isolation tests |
| Audit | A business operation completes its audit append in the same transaction. | The ordered audit chain and outbox entry are stored. | A chain or outbox failure rolls back the business operation. | B2 audit integration tests |

The PostgreSQL integration tests are the enforcement proof for database rules.
The service migration must keep these observable results while moving business
decisions out of stored procedures.
