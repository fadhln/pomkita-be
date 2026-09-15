package recoveryrepository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	auditrepository "github.com/pomkita/pomkita-be/internal/repository/audit"
	appaudit "github.com/pomkita/pomkita-be/internal/service/audit"
	apprecovery "github.com/pomkita/pomkita-be/internal/service/recovery"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecoveryRepository recovers stale submitting drafts and their shifts.
type RecoveryRepository struct {
	db *gorm.DB
}

// NewRecoveryRepository creates a recovery repository.
func NewRecoveryRepository(store *Store) *RecoveryRepository {
	if store == nil {
		return &RecoveryRepository{}
	}
	return &RecoveryRepository{db: store.DB}
}

// RecoverStale reopens drafts without reports and completes drafts with reports.
func (r *RecoveryRepository) RecoverStale(ctx context.Context, now time.Time, maxAge time.Duration) (int, error) {
	if r == nil || r.db == nil {
		return 0, apprecovery.ErrDependencyUnavailable
	}
	cutoff := now.Add(-maxAge)
	count := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var drafts []ShiftDraftModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("status = ? and updated_at < ?", "submitting", cutoff).Order("updated_at, draft_id").Find(&drafts).Error; err != nil {
			return fmt.Errorf("load stale drafts: %w", err)
		}
		for _, draft := range drafts {
			if err := tx.Model(&ShiftDraftModel{}).Where("draft_id = ? and status = ?", draft.DraftID, "submitting").Updates(map[string]any{"status": "recovering", "recovery_count": gorm.Expr("recovery_count + 1"), "updated_at": now}).Error; err != nil {
				return fmt.Errorf("mark draft recovering: %w", err)
			}
			var shift ShiftModel
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("shift_id = ?", draft.ShiftID).First(&shift).Error; err != nil {
				return fmt.Errorf("load stale shift: %w", err)
			}
			hasReport := shift.CurrentReportID != nil
			if !hasReport {
				if err := tx.Model(&ShiftDraftModel{}).Where("draft_id = ?", draft.DraftID).Updates(map[string]any{"status": "editing", "updated_at": now}).Error; err != nil {
					return fmt.Errorf("reopen draft: %w", err)
				}
				if err := tx.Model(&ShiftModel{}).Where("shift_id = ? and status = ?", shift.ShiftID, "submitting").Updates(map[string]any{"status": "open", "closed_at": nil}).Error; err != nil {
					return fmt.Errorf("reopen shift: %w", err)
				}
			} else {
				if err := tx.Model(&ShiftDraftModel{}).Where("draft_id = ?", draft.DraftID).Updates(map[string]any{"status": "submitted", "updated_at": now}).Error; err != nil {
					return fmt.Errorf("complete recovered draft: %w", err)
				}
				if err := tx.Model(&ShiftModel{}).Where("shift_id = ? and status = ?", shift.ShiftID, "submitting").Updates(map[string]any{"status": "awaiting_confirmation"}).Error; err != nil {
					return fmt.Errorf("complete recovered shift: %w", err)
				}
			}
			auditPayload, err := json.Marshal(map[string]any{
				"draft_id":       draft.DraftID.String(),
				"shift_id":       shift.ShiftID.String(),
				"shift_status":   recoveryShiftStatus(hasReport),
				"draft_status":   recoveryDraftStatus(hasReport),
				"recovery_count": draft.RecoveryCount + 1,
			})
			if err != nil {
				return fmt.Errorf("encode recovery audit payload: %w", err)
			}
			if _, err := auditrepository.AppendInTransaction(tx, appaudit.AppendRequest{OrgID: draft.OrgID, EventID: draft.DraftID, EventType: "shift.recovered", Payload: auditPayload, Outcome: recoveryOutcome(hasReport)}, now); err != nil {
				return fmt.Errorf("append recovery audit event: %w", err)
			}
			count++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func recoveryShiftStatus(hasReport bool) string {
	if hasReport {
		return "awaiting_confirmation"
	}
	return "open"
}

func recoveryDraftStatus(hasReport bool) string {
	if hasReport {
		return "submitted"
	}
	return "editing"
}

func recoveryOutcome(hasReport bool) string {
	if hasReport {
		return "completed"
	}
	return "reopened"
}
