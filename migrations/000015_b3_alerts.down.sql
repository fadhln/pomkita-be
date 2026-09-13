-- PLAN.md sections 4.2, 6, 7, and 8; BE-PLAN.md Phase B3.

delete from public.procedure_registry
 where name in ('fn_record_alert_occurrence', 'fn_run_starvation_alerts',
                'trg_alert_event_guard', 'trg_alert_event_check');
drop trigger if exists alert_events_invariants on public.alert_events;
drop trigger if exists alert_events_guard on public.alert_events;
drop function if exists public.fn_transition_shift(uuid, public.shift_status, text);
drop function if exists public.fn_run_starvation_alerts(timestamptz);
drop function if exists public.fn_record_alert_occurrence(uuid, public.alert_subject_kind, uuid, public.alert_event_type, timestamptz, public.alert_source_kind, uuid, integer, timestamptz, uuid, uuid);
drop function if exists public.trg_alert_event_check();
drop function if exists public.trg_alert_event_guard();
drop table if exists public.alert_events;
drop table if exists public.alert_rules;
drop type if exists public.alert_source_kind;
drop type if exists public.alert_event_type;
drop type if exists public.alert_subject_kind;
drop type if exists public.alert_channel;
drop type if exists public.alert_rule_type;
