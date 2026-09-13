package db

import (
	"context"
	"encoding/json"
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

// AckShiftResult is the result returned by fn_ack_shift.
type AckShiftResult struct {
	AckID     uuid.UUID `json:"ack_id"`
	ReportID  uuid.UUID `json:"report_id"`
	VersionNo int       `json:"version_no"`
	Decision  string    `json:"decision"`
}

// ApproveAmendmentResult is the result returned by fn_approve_amendment.
type ApproveAmendmentResult struct {
	AmendmentID     uuid.UUID `json:"amendment_id"`
	AppliedReportID uuid.UUID `json:"applied_report_id"`
	VersionNo       int       `json:"version_no"`
}

// GovernanceManager calls the B2 governance procedures.
type GovernanceManager struct {
	db *DB
}

// NewGovernanceManager creates a governance procedure manager.
func NewGovernanceManager(database *DB) *GovernanceManager { return &GovernanceManager{db: database} }

func (s *GovernanceManager) procedure(ctx context.Context, rawToken string, call func(pgx.Tx) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // commit is the successful path
	if err := s.db.SetRequestContext(ctx, tx, rawToken); err != nil {
		return err
	}
	if err := call(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit governance procedure: %w", err)
	}
	return nil
}

// AckShift calls fn_ack_shift.
func (s *GovernanceManager) AckShift(ctx context.Context, rawToken string, shift, report uuid.UUID, version int, decision string, rejectionReason *string, breakGlass bool, breakGlassReason *string) (AckShiftResult, error) {
	var result AckShiftResult
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			select ack_id, report_id, version_no, decision::text
			from public.fn_ack_shift($1::uuid,$2::uuid,$3::integer,$4::public.ack_decision,$5::text,$6::boolean,$7::text)
		`, shift, report, version, decision, nullableText(rejectionReason), breakGlass, nullableText(breakGlassReason)).Scan(
			&result.AckID, &result.ReportID, &result.VersionNo, &result.Decision,
		)
	})
	if err != nil {
		return AckShiftResult{}, fmt.Errorf("ack shift: %w", err)
	}
	return result, nil
}

// ApproveAmendment calls fn_approve_amendment.
func (s *GovernanceManager) ApproveAmendment(ctx context.Context, rawToken string, amendment uuid.UUID, staleHash []byte) (ApproveAmendmentResult, error) {
	var result ApproveAmendmentResult
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			select amendment_id, applied_report_id, version_no
			from public.fn_approve_amendment($1::uuid,$2::bytea)
		`, amendment, staleHash).Scan(&result.AmendmentID, &result.AppliedReportID, &result.VersionNo)
	})
	if err != nil {
		return ApproveAmendmentResult{}, fmt.Errorf("approve amendment: %w", err)
	}
	return result, nil
}

// ReadGovernanceAnomalies calls read_governance_anomalies.
func (s *GovernanceManager) ReadGovernanceAnomalies(ctx context.Context, rawToken string) ([]json.RawMessage, error) {
	var result []json.RawMessage
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select anomaly from public.read_governance_anomalies() anomaly`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item []byte
			if err := rows.Scan(&item); err != nil {
				return err
			}
			result = append(result, json.RawMessage(item))
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("read governance anomalies: %w", err)
	}
	return result, nil
}

func nullableText(value *string) any {
	if value == nil {
		return nil
	}
	return *value
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
