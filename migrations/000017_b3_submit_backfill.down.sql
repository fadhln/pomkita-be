-- PLAN.md sections 3.1, 4.2, 5.1, 5.4, 6, 7, and 8; BE-PLAN.md Phase B3.

-- The paired down migration removes the B3 replacement functions. The next
-- down migration removes their original B1 definitions with their tables.
drop function if exists public.fn_submit_shift(uuid,uuid,uuid,integer,text,jsonb);
drop function if exists public.fn_open_shift(uuid,uuid,timestamptz,boolean,date,integer,uuid,text);
