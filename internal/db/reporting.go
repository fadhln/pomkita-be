package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AnomalyExportRow is one flat anomaly row returned by the database.
type AnomalyExportRow struct {
	Kind           string
	Source         string
	SourceID       string
	OrgID          string
	StationID      string
	ShiftID        string
	ReportID       string
	VersionNo      string
	Reason         string
	VarianceRupiah string
	Threshold      string
	HappenedAt     string
}

// AuditExportRow is one flat audit row returned by the database.
type AuditExportRow struct {
	EventID      string
	OrgSequence  string
	EventType    string
	Payload      json.RawMessage
	Outcome      string
	OutcomeError string
	CreatedAt    string
	PreviousHash string
	RowHash      string
}

// AuditVerifyResult is the database result of an audit-chain verification.
type AuditVerifyResult struct {
	Verified    bool   `json:"verified"`
	OrgSequence string `json:"org_sequence"`
	Result      string `json:"result"`
}

// ReportingManager calls the B4 reporting procedures.
type ReportingManager struct{ db *DB }

// NewReportingManager creates a reporting procedure manager.
func NewReportingManager(database *DB) *ReportingManager { return &ReportingManager{db: database} }

func (s *ReportingManager) procedure(ctx context.Context, rawToken string, call func(pgx.Tx) error) error {
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
		return fmt.Errorf("commit reporting procedure: %w", err)
	}
	return nil
}

// ReadReportPrintout calls read_report_printout.
func (s *ReportingManager) ReadReportPrintout(ctx context.Context, rawToken string, report uuid.UUID) (json.RawMessage, error) {
	var result []byte
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select public.read_report_printout($1::uuid)`, report).Scan(&result)
	})
	if err != nil {
		return nil, fmt.Errorf("read report printout: %w", err)
	}
	return json.RawMessage(result), nil
}

// ReadAnomalyExport calls read_anomaly_export.
func (s *ReportingManager) ReadAnomalyExport(ctx context.Context, rawToken string) ([]AnomalyExportRow, error) {
	var result []AnomalyExportRow
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select kind, source, source_id::text, org_id::text, station_id::text, shift_id::text, report_id::text, version_no::text, coalesce(reason, ''), coalesce(variance_rupiah::text, ''), coalesce(threshold::text, ''), coalesce(to_char(happened_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'), '') from public.read_anomaly_export() order by happened_at, source_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row AnomalyExportRow
			if err := rows.Scan(&row.Kind, &row.Source, &row.SourceID, &row.OrgID, &row.StationID, &row.ShiftID, &row.ReportID, &row.VersionNo, &row.Reason, &row.VarianceRupiah, &row.Threshold, &row.HappenedAt); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("read anomaly export: %w", err)
	}
	return result, nil
}

// ReadAuditExport calls read_audit_export.
func (s *ReportingManager) ReadAuditExport(ctx context.Context, rawToken string) ([]AuditExportRow, error) {
	var result []AuditExportRow
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select event_id::text, org_sequence::text, event_type, payload, outcome, coalesce(outcome_error, ''), to_char(created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'), prev_hash, row_hash from public.read_audit_export() order by org_sequence`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row AuditExportRow
			if err := rows.Scan(&row.EventID, &row.OrgSequence, &row.EventType, &row.Payload, &row.Outcome, &row.OutcomeError, &row.CreatedAt, &row.PreviousHash, &row.RowHash); err != nil {
				return err
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("read audit export: %w", err)
	}
	return result, nil
}

// VerifyAuditChain calls fn_verify_audit_chain and reads the verified sequence.
func (s *ReportingManager) VerifyAuditChain(ctx context.Context, rawToken string) (AuditVerifyResult, error) {
	var result AuditVerifyResult
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `select public.fn_verify_audit_chain()`).Scan(&result.Verified); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select coalesce(max(org_sequence), 0)::text from public.read_audit_chain()`).Scan(&result.OrgSequence)
	})
	if err != nil {
		return AuditVerifyResult{}, fmt.Errorf("verify audit chain: %w", err)
	}
	result.Result = "chain_verified"
	return result, nil
}

// ReadPolicyHistory calls read_policy_history.
func (s *ReportingManager) ReadPolicyHistory(ctx context.Context, rawToken string) ([]json.RawMessage, error) {
	var result []json.RawMessage
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select item from public.read_policy_history() item`)
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
		return nil, fmt.Errorf("read policy history: %w", err)
	}
	return result, nil
}
