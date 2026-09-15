# Database migrations

Migrations are ordered SQL files in this directory. Production uses this
sequence. It contains schema, constraints, indexes, and seed support.
Each migration must be reversible and must reference the relevant `PLAN.md`
section in its header comment.
