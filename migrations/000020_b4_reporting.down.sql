-- PLAN.md sections 7, 9, 10, and 11; BE-PLAN.md Phase B4.

delete from public.procedure_registry
 where name in ('read_report_printout', 'read_anomaly_export',
                'read_audit_export', 'read_policy_history');

revoke all on function public.read_report_printout(uuid), public.read_anomaly_export(),
  public.read_audit_export(), public.read_policy_history() from public, pomkita_app,
  report_writer, audit_owner, relay;

drop function if exists public.read_report_printout(uuid);
drop function if exists public.read_anomaly_export();
drop function if exists public.read_audit_export();
drop function if exists public.read_policy_history();

