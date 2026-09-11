# Database migrations

Migrations are ordered SQL files and are the only place for schema, functions, triggers, roles, grants, and RLS changes. Each migration must be reversible and must reference the relevant `PLAN.md` section in its header comment.

The first implementation migration will deliver the B0 foundation. Do not add application tables in the bootstrap commit.
