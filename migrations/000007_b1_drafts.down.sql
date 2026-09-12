-- PLAN.md sections 4.2, 4.3, 5.1, 7, and 8; BE-PLAN.md Phase B1.

do $$
begin
  if to_regclass('public.procedure_registry') is not null then
    delete from public.procedure_registry where name in (
      'fn_claim_draft', 'fn_heartbeat_draft', 'fn_fence_draft',
      'fn_write_draft_reading', 'fn_write_draft_sales', 'fn_write_draft_loss',
      'fn_stage_draft_evidence', 'read_draft'
    );
  end if;
end
$$;
drop function if exists public.read_draft(uuid);
drop function if exists public.fn_stage_draft_evidence(uuid, uuid, integer, uuid, text, text, bytea, bigint, text);
drop function if exists public.fn_write_draft_loss(uuid, uuid, integer, uuid, text, text, numeric, numeric, text);
drop function if exists public.fn_write_draft_sales(uuid, uuid, integer, uuid, numeric, numeric);
drop function if exists public.fn_write_draft_reading(uuid, uuid, integer, uuid, numeric, numeric);
drop function if exists public.fn_fence_draft(uuid, uuid, integer);
drop function if exists public.fn_heartbeat_draft(uuid, uuid);
drop function if exists public.fn_claim_draft(uuid);
alter table if exists public.draft_evidence_staging drop constraint if exists draft_evidence_loss_fk;
alter table if exists public.draft_losses drop constraint if exists draft_losses_row_scope_key;
