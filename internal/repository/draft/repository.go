package draft

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fadhln/pomkita-be/internal/repository/scope"
	"github.com/fadhln/pomkita-be/internal/repository/store"
	appdraft "github.com/fadhln/pomkita-be/internal/service/draft"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const draftLeaseDuration = 10 * time.Minute

// DraftRepository persists draft leases and revision-fenced child rows.
type DraftRepository struct {
	db *gorm.DB
}

// NewDraftRepository creates a draft repository.
func NewDraftRepository(store *store.Store) *DraftRepository {
	if store == nil {
		return &DraftRepository{}
	}
	return &DraftRepository{db: store.DB}
}

// Claim acquires or takes over an expired draft lease.
func (r *DraftRepository) Claim(ctx context.Context, request appdraft.ClaimRequest, now time.Time) (appdraft.ClaimResult, error) {
	if r == nil || r.db == nil {
		return appdraft.ClaimResult{}, appdraft.ErrDependencyUnavailable
	}
	var result appdraft.ClaimResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var draft store.ShiftDraftModel
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("draft_id = ? and shift_id = ? and org_id is not null", request.DraftID, request.ShiftID)
		if err := query.First(&draft).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return appdraft.ErrDraftNotFound
			}
			return fmt.Errorf("load draft: %w", err)
		}
		if err := scope.RequireEnabled(tx, draft.OrgID, draft.StationID); err != nil {
			return err
		}
		if draft.ClaimExpiresAt != nil && draft.ClaimExpiresAt.After(now) && draft.OwnedBy != nil && *draft.OwnedBy != request.ActorID {
			return appdraft.ErrClaimExpired
		}
		token := uuid.New()
		expires := now.Add(draftLeaseDuration)
		updates := map[string]any{"owned_by": request.ActorID, "claim_token": token, "claim_expires_at": expires, "status": "editing", "updated_by": request.ActorID, "updated_at": now}
		if err := tx.Model(&store.ShiftDraftModel{}).Where("draft_id = ?", draft.DraftID).Updates(updates).Error; err != nil {
			return fmt.Errorf("claim draft: %w", err)
		}
		result = appdraft.ClaimResult{DraftID: draft.DraftID, ClaimToken: token, ClaimExpiresAt: expires, Revision: draft.Revision}
		return nil
	})
	if err != nil {
		return appdraft.ClaimResult{}, err
	}
	return result, nil
}

// Heartbeat extends a lease only for its current owner and token.
func (r *DraftRepository) Heartbeat(ctx context.Context, request appdraft.HeartbeatRequest, now time.Time) error {
	if r == nil || r.db == nil {
		return appdraft.ErrDependencyUnavailable
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var draft store.ShiftDraftModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("draft_id = ?", request.DraftID).First(&draft).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return appdraft.ErrDraftNotFound
			}
			return fmt.Errorf("load draft for heartbeat: %w", err)
		}
		if err := scope.RequireEnabled(tx, draft.OrgID, draft.StationID); err != nil {
			return err
		}
		if draft.OwnedBy == nil || *draft.OwnedBy != request.ActorID || draft.ClaimToken == nil || *draft.ClaimToken != request.ClaimToken || draft.ClaimExpiresAt == nil || !draft.ClaimExpiresAt.After(now) || draft.Status != "editing" {
			return appdraft.ErrClaimExpired
		}
		return tx.Model(&store.ShiftDraftModel{}).Where("draft_id = ?", request.DraftID).
			Updates(map[string]any{"claim_expires_at": now.Add(draftLeaseDuration), "updated_at": now, "updated_by": request.ActorID}).Error
	})
	if err != nil {
		return fmt.Errorf("heartbeat draft: %w", err)
	}
	return nil
}

// WriteReading inserts one reading after locking and fencing the draft revision.
func (r *DraftRepository) WriteReading(ctx context.Context, request appdraft.WriteReadingRequest, now time.Time) (int, error) {
	if r == nil || r.db == nil {
		return 0, appdraft.ErrDependencyUnavailable
	}
	var nextRevision int
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var draft store.ShiftDraftModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("draft_id = ?", request.DraftID).First(&draft).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return appdraft.ErrDraftNotFound
			}
			return fmt.Errorf("load draft for reading: %w", err)
		}
		if err := scope.RequireEnabled(tx, draft.OrgID, draft.StationID); err != nil {
			return err
		}
		if draft.ClaimExpiresAt == nil || !draft.ClaimExpiresAt.After(now) || draft.ClaimToken == nil || *draft.ClaimToken != request.ClaimToken || draft.OwnedBy == nil || *draft.OwnedBy != request.ActorID {
			return appdraft.ErrClaimExpired
		}
		if draft.Revision != request.Revision {
			return appdraft.ErrRevisionConflict
		}
		row := store.DraftReadingModel{RowID: uuid.New(), OrgID: draft.OrgID, StationID: draft.StationID, DraftID: draft.DraftID, NozzleID: request.NozzleID, MeterStart: store.Decimal(request.MeterStart), MeterEnd: store.Decimal(request.MeterEnd), CreatedBy: request.ActorID}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("create draft reading: %w", err)
		}
		nextRevision = draft.Revision + 1
		if err := tx.Model(&store.ShiftDraftModel{}).Where("draft_id = ? and revision = ?", draft.DraftID, draft.Revision).Updates(map[string]any{"revision": nextRevision, "updated_at": now, "updated_by": request.ActorID}).Error; err != nil {
			return fmt.Errorf("advance draft revision: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return nextRevision, nil
}

// WriteSales inserts one sales row after the lease and revision checks.
func (r *DraftRepository) WriteSales(ctx context.Context, request appdraft.WriteSalesRequest, now time.Time) (int, error) {
	if r == nil || r.db == nil {
		return 0, appdraft.ErrDependencyUnavailable
	}
	var next int
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		draft, err := r.lockDraftForMutation(tx, request.DraftID, request.ClaimToken, request.ActorID, request.Revision, now)
		if err != nil {
			return err
		}
		row := store.DraftSalesModel{RowID: uuid.New(), OrgID: draft.OrgID, StationID: draft.StationID, DraftID: draft.DraftID, DispenserID: request.DispenserID, CashAmount: store.Decimal(request.CashAmount), CashlessAmount: store.Decimal(request.CashlessAmount), CreatedBy: request.ActorID}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("create draft sales: %w", err)
		}
		next = draft.Revision + 1
		return r.advanceDraft(tx, draft, next, request.ActorID, now)
	})
	if err != nil {
		return 0, err
	}
	return next, nil
}

// WriteLoss inserts one loss row after the lease and revision checks.
func (r *DraftRepository) WriteLoss(ctx context.Context, request appdraft.WriteLossRequest, now time.Time) (int, error) {
	if r == nil || r.db == nil {
		return 0, appdraft.ErrDependencyUnavailable
	}
	var next int
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		draft, err := r.lockDraftForMutation(tx, request.DraftID, request.ClaimToken, request.ActorID, request.Revision, now)
		if err != nil {
			return err
		}
		var cash *store.Decimal
		if request.CashAmount != "" {
			value := store.Decimal(request.CashAmount)
			cash = &value
		}
		row := store.DraftLossModel{RowID: uuid.New(), OrgID: draft.OrgID, StationID: draft.StationID, DraftID: draft.DraftID, LossID: request.LossID, Direction: request.Direction, ReasonCode: request.ReasonCode, Liters: store.Decimal(request.Liters), CashAmount: cash, CreatedBy: request.ActorID}
		if request.Note != "" {
			row.Note = &request.Note
		}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("create draft loss: %w", err)
		}
		next = draft.Revision + 1
		return r.advanceDraft(tx, draft, next, request.ActorID, now)
	})
	if err != nil {
		return 0, err
	}
	return next, nil
}

// StageEvidence inserts one staged object after the lease and revision checks.
func (r *DraftRepository) StageEvidence(ctx context.Context, request appdraft.StageEvidenceRequest, now time.Time) (int, error) {
	if r == nil || r.db == nil {
		return 0, appdraft.ErrDependencyUnavailable
	}
	var next int
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		draft, err := r.lockDraftForMutation(tx, request.DraftID, request.ClaimToken, request.ActorID, request.Revision, now)
		if err != nil {
			return err
		}
		row := store.DraftEvidenceStagingModel{RowID: uuid.New(), OrgID: draft.OrgID, StationID: draft.StationID, DraftID: draft.DraftID, LossRowID: request.LossRowID, EvidenceType: request.EvidenceType, ObjectKey: request.ObjectKey, ContentHash: append([]byte(nil), request.ContentHash...), SizeBytes: request.SizeBytes, MIME: request.MIME, Status: "uploaded", UploadedBy: request.ActorID}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("stage draft evidence: %w", err)
		}
		next = draft.Revision + 1
		return r.advanceDraft(tx, draft, next, request.ActorID, now)
	})
	if err != nil {
		return 0, err
	}
	return next, nil
}

func (r *DraftRepository) lockDraftForMutation(tx *gorm.DB, draftID, claimToken, actorID uuid.UUID, revision int, now time.Time) (store.ShiftDraftModel, error) {
	var draft store.ShiftDraftModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("draft_id = ?", draftID).First(&draft).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return store.ShiftDraftModel{}, appdraft.ErrDraftNotFound
		}
		return store.ShiftDraftModel{}, fmt.Errorf("load draft child: %w", err)
	}
	if err := scope.RequireEnabled(tx, draft.OrgID, draft.StationID); err != nil {
		return store.ShiftDraftModel{}, err
	}
	if draft.ClaimExpiresAt == nil || !draft.ClaimExpiresAt.After(now) || draft.ClaimToken == nil || *draft.ClaimToken != claimToken || draft.OwnedBy == nil || *draft.OwnedBy != actorID {
		return store.ShiftDraftModel{}, appdraft.ErrClaimExpired
	}
	if draft.Revision != revision {
		return store.ShiftDraftModel{}, appdraft.ErrRevisionConflict
	}
	return draft, nil
}

func (r *DraftRepository) advanceDraft(tx *gorm.DB, draft store.ShiftDraftModel, revision int, actorID uuid.UUID, now time.Time) error {
	if err := tx.Model(&store.ShiftDraftModel{}).Where("draft_id = ? and revision = ?", draft.DraftID, draft.Revision).Updates(map[string]any{"revision": revision, "updated_at": now, "updated_by": actorID}).Error; err != nil {
		return fmt.Errorf("advance draft revision: %w", err)
	}
	return nil
}
