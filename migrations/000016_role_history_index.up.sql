-- BA4 supports role history lookup by target user in the audit payload.

CREATE INDEX audit_log_role_history_target
    ON audit_log ((payload ->> 'target'))
    WHERE event_type IN ('role.assigned', 'role.revoked');
