package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pomkita/pomkita-be/internal/relay"
)

// Claim implements the relay.Store claim operation.
func (d *DB) Claim(ctx context.Context, orgID, eventID uuid.UUID) (relay.Event, uuid.UUID, bool, error) {
	var event relay.Event
	var lease uuid.UUID
	var payload []byte
	var createdAt time.Time
	err := d.pool.QueryRow(ctx, `
		select event_id, event_type, payload, created_at, lease_token
		from public.fn_relay_claim_event($1::uuid,$2::uuid)
	`, orgID, eventID).Scan(&event.EventID, &event.EventType, &payload, &createdAt, &lease)
	if errors.Is(err, pgx.ErrNoRows) {
		return relay.Event{}, uuid.Nil, false, nil
	}
	if err != nil {
		return relay.Event{}, uuid.Nil, false, fmt.Errorf("claim relay event: %w", err)
	}
	event.Payload = payload
	return event, lease, true, nil
}

// Finish implements the relay.Store finish operation.
func (d *DB) Finish(ctx context.Context, orgID, eventID, lease uuid.UUID, success bool, failure string) error {
	_, err := d.pool.Exec(ctx, `
		select public.fn_relay_finish_event($1::uuid,$2::uuid,$3::uuid,$4::boolean,$5::text)
	`, orgID, eventID, lease, success, failure)
	if err != nil {
		return fmt.Errorf("finish relay event: %w", err)
	}
	return nil
}

// AuditDeniedRecord contains safe metadata for a denied or failed request.
type AuditDeniedRecord struct {
	RequestID   uuid.UUID
	SubjectID   *uuid.UUID
	JTI         *uuid.UUID
	OrgID       *uuid.UUID
	StationID   *uuid.UUID
	Action      string
	Target      string
	Reason      string
	Outcome     string
	ErrorDetail string
}

// RecordAuditDenied writes a denial record through the separate audit pool.
func (d *DB) RecordAuditDenied(ctx context.Context, record AuditDeniedRecord) error {
	if d == nil || d.auditPool == nil {
		return errors.New("audit database pool is not configured")
	}
	writeContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := d.auditPool.Exec(writeContext, `
		select public.fn_record_audit_denied(
			$1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,
			$6::text,$7::text,$8::text,$9::text,$10::text)
	`, record.RequestID, nullableUUID(record.SubjectID), nullableUUID(record.JTI),
		nullableUUID(record.OrgID), nullableUUID(record.StationID), record.Action,
		record.Target, record.Reason, record.Outcome, record.ErrorDetail)
	if err != nil {
		return fmt.Errorf("record denied request: %w", err)
	}
	return nil
}

func nullableUUID(value *uuid.UUID) pgtype.UUID {
	if value == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *value, Valid: true}
}

var _ relay.Store = (*DB)(nil)
