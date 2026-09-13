-- PLAN.md section 4.4; BE-PLAN.md Phase B5.

delete from public.procedure_registry where name in ('fn_create_policy_revision', 'fn_tombstone_policy_revision', 'trg_policy_revision_guard', 'trg_evidence_policy_type_guard');
drop trigger if exists threshold_policy_revisions_guard on public.threshold_policy_revisions;
drop trigger if exists evidence_policy_revisions_guard on public.evidence_policy_revisions;
drop trigger if exists evidence_policy_types_guard on public.evidence_policy_types;
drop function if exists public.fn_tombstone_policy_revision(text,uuid,text);
drop function if exists public.fn_create_policy_revision(text,uuid,uuid,timestamptz,uuid,uuid,numeric,numeric,numeric,numeric,numeric,numeric,text,jsonb);
drop function if exists public.trg_policy_revision_guard();
drop function if exists public.trg_evidence_policy_type_guard();
