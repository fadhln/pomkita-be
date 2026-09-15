# Instructions for AI agents

Use ASD-STE100 Simplified Technical English in code comments, documentation, commit messages, and handoff notes.

## Read first

Read these files before you change code:

1. `../PLAN.md` for product, security, and data rules.
2. `../BE-PLAN.md` for backend phases and acceptance criteria.
3. `docs/CONVENTIONS.md` for repository conventions.

If two instructions conflict, use this order:

1. The current user instruction.
2. `../PLAN.md`.
3. `../BE-PLAN.md`.
4. `docs/CONVENTIONS.md`.

## Mandatory rules

- Always use test-driven development for an implementation or a defect fix.
- Do not write production code before you observe the applicable test fail for the expected reason.
- Keep the PostgreSQL database as the enforcement layer.
- Keep HTTP code in `internal/httpapi`.
- Keep database calls in `internal/repository`.
- Do not query a table from an HTTP handler.
- Do not give table DML privileges to the application role.
- Do not use `float32` or `float64` for money or volume.
- Do not log secrets, raw tokens, evidence content, or personal data.
- Do not change a lifecycle outside the rules in `../PLAN.md`.
- Do not add a table, procedure, role, or grant outside the source plan.
- Change the API contract only when planned behavior requires the change.
- Preserve unrelated user changes.

## Work sequence

1. Check `git status`.
2. Find the applicable plan section and acceptance criterion.
3. State the API, migration, security, and test impact.
4. Add the smallest test that specifies the behavior.
5. Run the focused test. Confirm that it fails for the expected reason. This is the red step.
6. Add the smallest production change that can pass the test. This is the green step.
7. Run the focused test again.
8. Refactor only while the tests are green.
9. Run `make check`.
10. Review the diff for scope, secrets, generated files, and unsafe grants.
11. Report the red test, changed files, checks, risks, and the next dependency.

Use a small commit. Start the commit subject with the backend phase when applicable. Example: `B0: add session context validation`.

## Completion gate

For an implementation or a defect fix, do not mark work complete when the red step did not occur. Do not mark work complete when a required check fails. Do not hide a failed check. State the failure and its cause.
