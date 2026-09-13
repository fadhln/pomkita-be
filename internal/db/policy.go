package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// EvidencePolicyType is one accepted evidence type in a policy revision.
type EvidencePolicyType struct {
	EvidenceType          string   `json:"evidence_type"`
	MinimumCountPerLoss   int      `json:"minimum_count_per_loss"`
	AcceptedMIMETypeNames []string `json:"accepted_mime_types"`
}

// CreatePolicyRevisionInput contains an owner policy revision request.
type CreatePolicyRevisionInput struct {
	PolicyKind              string
	PolicyID                uuid.UUID
	StationID               *uuid.UUID
	ValidFrom               string
	SupersedesOrgID         *uuid.UUID
	SupersedesRevisionID    *uuid.UUID
	LossLiterThreshold      string
	GainLiterThreshold      string
	LossRupiahThreshold     string
	GainRupiahThreshold     string
	VarianceRupiahThreshold string
	RolloverThreshold       string
	Mode                    string
	Types                   []EvidencePolicyType
}

// TombstonePolicyRevisionInput contains a reasoned policy disable request.
type TombstonePolicyRevisionInput struct {
	PolicyKind string
	RevisionID uuid.UUID
	Reason     string
}

// PolicyRevisionResult is the result of a policy revision write.
type PolicyRevisionResult struct {
	RevisionID uuid.UUID  `json:"revision_id"`
	PolicyKind string     `json:"policy_kind"`
	PolicyID   uuid.UUID  `json:"policy_id"`
	StationID  *uuid.UUID `json:"station_id"`
	ValidFrom  string     `json:"valid_from"`
	Disabled   bool       `json:"disabled"`
}

// PolicyManager calls the B5 policy revision procedures.
type PolicyManager struct{ db *DB }

// NewPolicyManager creates a policy procedure manager.
func NewPolicyManager(database *DB) *PolicyManager { return &PolicyManager{db: database} }

func (s *PolicyManager) procedure(ctx context.Context, rawToken string, call func(pgx.Tx) error) error {
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
		return fmt.Errorf("commit policy procedure: %w", err)
	}
	return nil
}

// CreatePolicyRevision calls fn_create_policy_revision.
func (s *PolicyManager) CreatePolicyRevision(ctx context.Context, rawToken string, input CreatePolicyRevisionInput) (PolicyRevisionResult, error) {
	var result PolicyRevisionResult
	types, err := json.Marshal(input.Types)
	if err != nil {
		return result, fmt.Errorf("encode policy types: %w", err)
	}
	err = s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		var station pgtype.UUID
		var validFrom time.Time
		err := tx.QueryRow(ctx, `
			select revision_id, policy_kind, policy_id, station_id, valid_from, disabled
			from public.fn_create_policy_revision(
				$1::text,$2::uuid,$3::uuid,$4::timestamptz,$5::uuid,$6::uuid,
				$7::numeric,$8::numeric,$9::numeric,$10::numeric,$11::numeric,$12::numeric,
				$13::text,$14::jsonb)
		`, input.PolicyKind, input.PolicyID, nullablePolicyUUID(input.StationID), input.ValidFrom,
			nullablePolicyUUID(input.SupersedesOrgID), nullablePolicyUUID(input.SupersedesRevisionID),
			input.LossLiterThreshold, input.GainLiterThreshold, input.LossRupiahThreshold,
			input.GainRupiahThreshold, input.VarianceRupiahThreshold, nullablePolicyText(input.RolloverThreshold),
			nullablePolicyText(input.Mode), types).Scan(&result.RevisionID, &result.PolicyKind, &result.PolicyID, &station, &validFrom, &result.Disabled)
		if err != nil {
			return err
		}
		result.StationID = policyUUIDPointer(station)
		result.ValidFrom = apiTimestamp(validFrom)
		return nil
	})
	if err != nil {
		return PolicyRevisionResult{}, fmt.Errorf("create policy revision: %w", err)
	}
	return result, nil
}

// TombstonePolicyRevision calls fn_tombstone_policy_revision.
func (s *PolicyManager) TombstonePolicyRevision(ctx context.Context, rawToken string, input TombstonePolicyRevisionInput) (PolicyRevisionResult, error) {
	var result PolicyRevisionResult
	err := s.procedure(ctx, rawToken, func(tx pgx.Tx) error {
		var station pgtype.UUID
		var validFrom time.Time
		err := tx.QueryRow(ctx, `
			select revision_id, policy_kind, policy_id, station_id, valid_from, disabled
			from public.fn_tombstone_policy_revision($1::text,$2::uuid,$3::text)
		`, input.PolicyKind, input.RevisionID, input.Reason).Scan(&result.RevisionID, &result.PolicyKind, &result.PolicyID, &station, &validFrom, &result.Disabled)
		if err != nil {
			return err
		}
		result.StationID = policyUUIDPointer(station)
		result.ValidFrom = apiTimestamp(validFrom)
		return nil
	})
	if err != nil {
		return PolicyRevisionResult{}, fmt.Errorf("tombstone policy revision: %w", err)
	}
	return result, nil
}

func nullablePolicyUUID(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullablePolicyText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func policyUUIDPointer(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	result := uuid.UUID(value.Bytes)
	return &result
}
