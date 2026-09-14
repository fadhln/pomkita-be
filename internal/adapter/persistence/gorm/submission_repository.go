package gormstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	appsubmission "github.com/pomkita/pomkita-be/internal/service/submission"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SubmissionRepository creates immutable version-one reports and idempotency state.
type SubmissionRepository struct {
	db *gorm.DB
}

// NewSubmissionRepository creates a submission repository.
func NewSubmissionRepository(store *Store) *SubmissionRepository {
	if store == nil {
		return &SubmissionRepository{}
	}
	return &SubmissionRepository{db: store.db}
}

// Submit performs the idempotency and report state changes in one transaction.
func (r *SubmissionRepository) Submit(ctx context.Context, request appsubmission.Request, requestHash []byte, payload []byte, now time.Time) (appsubmission.Result, error) {
	if r == nil || r.db == nil {
		return appsubmission.Result{}, appsubmission.ErrDependencyUnavailable
	}
	var result appsubmission.Result
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var idem SubmitIdempotencyModel
		idemErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ? and shift_id = ? and idempotency_key = ?", request.OrgID, request.StationID, request.ShiftID, request.IdempotencyKey).First(&idem).Error
		if idemErr == nil {
			if string(idem.RequestHash) != string(requestHash) || idem.Status == "failed" {
				return appsubmission.ErrIdempotencyConflict
			}
			if idem.Status == "succeeded" && idem.ResultingReport != nil {
				result = appsubmission.Result{ReportID: *idem.ResultingReport, Replay: true}
				return nil
			}
			if idem.Status == "in_progress" && idem.LeaseExpiresAt != nil && idem.LeaseExpiresAt.After(now) {
				return appsubmission.ErrIdempotencyConflict
			}
		} else if !errors.Is(idemErr, gorm.ErrRecordNotFound) {
			return fmt.Errorf("load submit idempotency: %w", idemErr)
		}

		var shift ShiftModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ? and shift_id = ?", request.OrgID, request.StationID, request.ShiftID).First(&shift).Error; err != nil {
			return fmt.Errorf("load shift for submit: %w", err)
		}
		if shift.Status != "open" {
			return appsubmission.ErrIdempotencyConflict
		}
		var draft ShiftDraftModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("draft_id = ? and shift_id = ?", request.DraftID, request.ShiftID).First(&draft).Error; err != nil {
			return fmt.Errorf("load draft for submit: %w", err)
		}
		if draft.ClaimToken == nil || *draft.ClaimToken != request.ClaimToken || draft.OwnedBy == nil || *draft.OwnedBy != request.ActorID || draft.ClaimExpiresAt == nil || !draft.ClaimExpiresAt.After(now) {
			return fmt.Errorf("submit draft claim: %w", appsubmission.ErrIdempotencyConflict)
		}
		if draft.Revision != request.ExpectedRevision {
			return fmt.Errorf("submit draft revision: %w", appsubmission.ErrIdempotencyConflict)
		}
		if idemErr != nil {
			idem = SubmitIdempotencyModel{IdemID: uuid.New(), OrgID: request.OrgID, StationID: request.StationID, ShiftID: request.ShiftID, IdempotencyKey: request.IdempotencyKey, RequestHash: append([]byte(nil), requestHash...), Status: "in_progress", ClaimToken: &request.ClaimToken, AttemptCount: 1, LeaseStartedAt: &now, LeaseExpiresAt: submissionTimePointer(now.Add(10 * time.Minute)), CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&idem).Error; err != nil {
				return fmt.Errorf("create submit idempotency: %w", err)
			}
		} else {
			updates := map[string]any{"status": "in_progress", "claim_token": request.ClaimToken, "attempt_count": idem.AttemptCount + 1, "lease_started_at": now, "lease_expires_at": now.Add(10 * time.Minute), "updated_at": now, "error_detail": nil}
			if err := tx.Model(&SubmitIdempotencyModel{}).Where("idem_id = ?", idem.IdemID).Updates(updates).Error; err != nil {
				return fmt.Errorf("take over submit idempotency: %w", err)
			}
		}

		var policySet PolicySnapshotSetModel
		if err := tx.Where("org_id = ? and station_id = ? and shift_id = ?", request.OrgID, request.StationID, request.ShiftID).First(&policySet).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			policySet = PolicySnapshotSetModel{SetID: uuid.New(), OrgID: request.OrgID, StationID: request.StationID, ShiftID: &request.ShiftID, CreatedAt: now}
			if err := tx.Create(&policySet).Error; err != nil {
				return fmt.Errorf("create policy snapshot set: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("load policy snapshot set: %w", err)
		}
		reportID := uuid.New()
		report := ShiftReportModel{ReportID: reportID, OrgID: request.OrgID, StationID: request.StationID, ShiftID: request.ShiftID, VersionNo: 1, Status: "submitted", SubmittedBy: request.ActorID, SubmittedAt: now, PolicySnapshot: policySet.SetID}
		if err := tx.Create(&report).Error; err != nil {
			return fmt.Errorf("create shift report: %w", err)
		}
		if err := tx.Model(&ShiftModel{}).Where("shift_id = ?", request.ShiftID).Updates(map[string]any{"status": "awaiting_confirmation", "current_report_id": reportID, "closed_at": now}).Error; err != nil {
			return fmt.Errorf("move shift to confirmation: %w", err)
		}
		if err := tx.Model(&ShiftDraftModel{}).Where("draft_id = ?", request.DraftID).Updates(map[string]any{"status": "submitted", "claim_token": nil, "claim_expires_at": nil, "updated_at": now, "updated_by": request.ActorID}).Error; err != nil {
			return fmt.Errorf("complete draft: %w", err)
		}
		if err := tx.Model(&SubmitIdempotencyModel{}).Where("idem_id = ?", idem.IdemID).Updates(map[string]any{"status": "succeeded", "resulting_report_id": reportID, "lease_expires_at": nil, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("complete submit idempotency: %w", err)
		}
		result = appsubmission.Result{ReportID: reportID}
		_ = payload
		return nil
	})
	if err != nil {
		return appsubmission.Result{}, err
	}
	return result, nil
}

func submissionTimePointer(value time.Time) *time.Time {
	return &value
}
