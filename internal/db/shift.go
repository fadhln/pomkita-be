package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// OpenShiftResult is the result returned by fn_open_shift.
type OpenShiftResult struct {
	ShiftID               uuid.UUID       `json:"shift_id"`
	StationSeq            string          `json:"station_seq"`
	BusinessDate          string          `json:"business_date"`
	ShiftPriceMapSnapshot json.RawMessage `json:"shift_price_map_snapshot"`
	ShiftPriceMapHash     string          `json:"shift_price_map_hash"`
}

// ClaimDraftResult is the result returned by fn_claim_draft.
type ClaimDraftResult struct {
	DraftID        uuid.UUID `json:"draft_id"`
	ClaimToken     uuid.UUID `json:"claim_token"`
	ClaimExpiresAt string    `json:"claim_expires_at"`
	Revision       int       `json:"revision"`
}

// SubmitShiftResult is the result returned by fn_submit_shift.
type SubmitShiftResult struct {
	ReportID    uuid.UUID `json:"report_id"`
	Replay      bool      `json:"replay"`
	RequestHash string    `json:"request_hash"`
}

// ShiftManager calls the shift and draft procedures.
type ShiftManager struct {
	db *DB
}

// NewShiftManager creates a shift and draft procedure manager.
func NewShiftManager(database *DB) *ShiftManager { return &ShiftManager{db: database} }

func (s *ShiftManager) procedure(ctx context.Context, rawToken string, call func(pgx.Tx) error) error {
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
		return fmt.Errorf("commit shift procedure: %w", err)
	}
	return nil
}

// OpenShift calls fn_open_shift for the verified actor.
func (s *ShiftManager) OpenShift(ctx context.Context, rawToken string, actor, station uuid.UUID, openedAt time.Time, backfilled bool, eventDate, reason *string, shiftKE *int) (OpenShiftResult, error) {
	var result OpenShiftResult
	var approver *uuid.UUID
	if backfilled {
		approver = &actor
	}
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		var snapshot, hash []byte
		err := tx.QueryRow(ctx, `
			select shift_id, station_seq::text, business_date::text,
			       shift_price_map_snapshot, encode(shift_price_map_hash, 'hex')
			from public.fn_open_shift($1::uuid,$2::uuid,$3::timestamptz,$4::boolean,$5::date,$6::integer,$7::uuid,$8::text)
		`, station, actor, openedAt, backfilled, eventDate, shiftKE, approver, reason).Scan(
			&result.ShiftID, &result.StationSeq, &result.BusinessDate, &snapshot, &hash,
		)
		if err == nil {
			result.ShiftPriceMapSnapshot = json.RawMessage(snapshot)
			result.ShiftPriceMapHash = string(hash)
		}
		return err
	})
	if err != nil {
		return OpenShiftResult{}, fmt.Errorf("open shift: %w", err)
	}
	return result, nil
}

// ClaimDraft calls fn_claim_draft.
func (s *ShiftManager) ClaimDraft(ctx context.Context, rawToken string, shift uuid.UUID) (ClaimDraftResult, error) {
	var result ClaimDraftResult
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		var expires time.Time
		err := tx.QueryRow(ctx, `select draft_id, claim_token, claim_expires_at, revision from public.fn_claim_draft($1::uuid)`, shift).
			Scan(&result.DraftID, &result.ClaimToken, &expires, &result.Revision)
		result.ClaimExpiresAt = apiTimestamp(expires)
		return err
	})
	if err != nil {
		return ClaimDraftResult{}, fmt.Errorf("claim draft: %w", err)
	}
	return result, nil
}

// HeartbeatDraft calls fn_heartbeat_draft.
func (s *ShiftManager) HeartbeatDraft(ctx context.Context, rawToken string, draft, claim uuid.UUID) (bool, error) {
	var renewed bool
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select public.fn_heartbeat_draft($1::uuid,$2::uuid)`, draft, claim).Scan(&renewed)
	})
	if err != nil {
		return false, fmt.Errorf("heartbeat draft: %w", err)
	}
	return renewed, nil
}

// WriteDraftReading calls fn_write_draft_reading.
func (s *ShiftManager) WriteDraftReading(ctx context.Context, rawToken string, draft, claim, nozzle uuid.UUID, revision int, start, end string) (int, error) {
	var next int
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select public.fn_write_draft_reading($1::uuid,$2::uuid,$3::integer,$4::uuid,$5::numeric,$6::numeric)`, draft, claim, revision, nozzle, start, end).Scan(&next)
	})
	if err != nil {
		return 0, fmt.Errorf("write draft reading: %w", err)
	}
	return next, nil
}

// WriteDraftSales calls fn_write_draft_sales.
func (s *ShiftManager) WriteDraftSales(ctx context.Context, rawToken string, draft, claim, dispenser uuid.UUID, revision int, cash, cashless string) (int, error) {
	var next int
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select public.fn_write_draft_sales($1::uuid,$2::uuid,$3::integer,$4::uuid,$5::numeric,$6::numeric)`, draft, claim, revision, dispenser, cash, cashless).Scan(&next)
	})
	if err != nil {
		return 0, fmt.Errorf("write draft sales: %w", err)
	}
	return next, nil
}

// WriteDraftLoss calls fn_write_draft_loss.
func (s *ShiftManager) WriteDraftLoss(ctx context.Context, rawToken string, draft, claim, loss uuid.UUID, revision int, direction, reason, liters, cash, note string) (int, error) {
	var next int
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select public.fn_write_draft_loss($1::uuid,$2::uuid,$3::integer,$4::uuid,$5::text,$6::text,$7::numeric,nullif($8::text,'')::numeric,$9::text)`, draft, claim, revision, loss, direction, reason, liters, cash, note).Scan(&next)
	})
	if err != nil {
		return 0, fmt.Errorf("write draft loss: %w", err)
	}
	return next, nil
}

// StageDraftEvidence calls fn_stage_draft_evidence.
func (s *ShiftManager) StageDraftEvidence(ctx context.Context, rawToken string, draft, claim, loss uuid.UUID, revision int, evidenceType, objectKey string, contentHash []byte, size int64, mime string) (int, error) {
	var next int
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select public.fn_stage_draft_evidence($1::uuid,$2::uuid,$3::integer,$4::uuid,$5::text,$6::text,$7::bytea,$8::bigint,$9::text)`, draft, claim, revision, loss, evidenceType, objectKey, contentHash, size, mime).Scan(&next)
	})
	if err != nil {
		return 0, fmt.Errorf("stage draft evidence: %w", err)
	}
	return next, nil
}

// ReadDraft calls read_draft and returns its JSONB payload.
func (s *ShiftManager) ReadDraft(ctx context.Context, rawToken string, shift uuid.UUID) (json.RawMessage, error) {
	var result []byte
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select public.read_draft($1::uuid)`, shift).Scan(&result)
	})
	if err != nil {
		return nil, fmt.Errorf("read draft: %w", err)
	}
	return json.RawMessage(result), nil
}

// ReadShiftList calls read_shift_list and returns each JSONB row.
func (s *ShiftManager) ReadShiftList(ctx context.Context, rawToken string, station *uuid.UUID) ([]json.RawMessage, error) {
	var result []json.RawMessage
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select item from public.read_shift_list($1::uuid) item`, nullableUUIDArg(station))
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
		return nil, fmt.Errorf("read shift list: %w", err)
	}
	return result, nil
}

// ReadShiftDetail calls read_shift_detail and returns its JSONB payload.
func (s *ShiftManager) ReadShiftDetail(ctx context.Context, rawToken string, shift uuid.UUID) (json.RawMessage, error) {
	return s.readJSON(ctx, rawToken, `select public.read_shift_detail($1::uuid)`, shift, "read shift detail")
}

// ReadReport calls read_report and returns its JSONB payload.
func (s *ShiftManager) ReadReport(ctx context.Context, rawToken string, report uuid.UUID) (json.RawMessage, error) {
	return s.readJSON(ctx, rawToken, `select public.read_report($1::uuid)`, report, "read report")
}

func (s *ShiftManager) readJSON(ctx context.Context, rawToken, query string, id uuid.UUID, action string) (json.RawMessage, error) {
	var result []byte
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error { return tx.QueryRow(ctx, query, id).Scan(&result) })
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	return json.RawMessage(result), nil
}

// SubmitShift calls fn_submit_shift with the supplied idempotency key and payload.
func (s *ShiftManager) SubmitShift(ctx context.Context, rawToken string, shift, draft, claim uuid.UUID, revision int, idempotencyKey string, request json.RawMessage) (SubmitShiftResult, error) {
	var result SubmitShiftResult
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select report_id, replay, encode(request_hash, 'hex') from public.fn_submit_shift($1::uuid,$2::uuid,$3::uuid,$4::integer,$5::text,$6::jsonb)`, shift, draft, claim, revision, idempotencyKey, request).Scan(&result.ReportID, &result.Replay, &result.RequestHash)
	})
	if err != nil {
		return SubmitShiftResult{}, fmt.Errorf("submit shift: %w", err)
	}
	return result, nil
}

func apiTimestamp(value time.Time) string { return value.UTC().Format("2006-01-02T15:04:05.000000Z") }

func nullableUUIDArg(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return *value
}
