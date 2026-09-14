# Database migrations

Migrations are ordered SQL files. Production uses the table-only sequence in
`migrations/clean`; it contains schema, constraints, indexes, and seed support.
Each migration must be reversible and must reference the relevant `PLAN.md`
section in its header comment.
