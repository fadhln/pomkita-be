-- PLAN.md sections 3.3, 4.2, 5.3, 6, 7, and 8; BE-PLAN.md Phase B3.

delete from public.procedure_registry
 where name in ('fn_evaluate_variance_alert','trg_report_variance_alert',
                'trg_amendment_evidence_clone','read_anomalies','read_alerts','read_ack_queue');
drop trigger if exists shift_report_variance_alert on public.shift_reports;
drop trigger if exists loss_entries_amendment_evidence on public.loss_entries;
drop function if exists public.trg_report_variance_alert();
drop function if exists public.trg_amendment_evidence_clone();
drop function if exists public.fn_evaluate_variance_alert(uuid);
drop function if exists public.read_anomalies();
drop function if exists public.read_alerts();
drop function if exists public.read_ack_queue();
