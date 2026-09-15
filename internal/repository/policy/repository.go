package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	auditrepository "github.com/pomkita/pomkita-be/internal/repository/audit"
	appaudit "github.com/pomkita/pomkita-be/internal/service/audit"
	apppolicy "github.com/pomkita/pomkita-be/internal/service/policy"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PolicyRepository persists append-only policy revisions.
type PolicyRepository struct {
	db *gorm.DB
}

// History reads threshold and evidence revisions in stable valid-from order.
func (r *PolicyRepository) History(ctx context.Context, orgID uuid.UUID, stationID *uuid.UUID) ([]apppolicy.RevisionView, error) {
	if r == nil || r.db == nil {
		return nil, apppolicy.ErrInvalidRequest
	}
	var thresholds []ThresholdPolicyRevisionModel
	thresholdQuery := r.db.WithContext(ctx).Where("org_id = ?", orgID)
	if stationID != nil {
		thresholdQuery = thresholdQuery.Where("station_id = ? or station_id is null", *stationID)
	}
	if err := thresholdQuery.Find(&thresholds).Error; err != nil {
		return nil, fmt.Errorf("load threshold policy history: %w", err)
	}
	var evidence []EvidencePolicyRevisionModel
	evidenceQuery := r.db.WithContext(ctx).Where("org_id = ?", orgID)
	if stationID != nil {
		evidenceQuery = evidenceQuery.Where("station_id = ? or station_id is null", *stationID)
	}
	if err := evidenceQuery.Find(&evidence).Error; err != nil {
		return nil, fmt.Errorf("load evidence policy history: %w", err)
	}
	result := make([]apppolicy.RevisionView, 0, len(thresholds)+len(evidence))
	for _, row := range thresholds {
		lossLiters, gainLiters := row.LossLiterThreshold.String(), row.GainLiterThreshold.String()
		lossRupiah, gainRupiah := row.LossRupiahThreshold.String(), row.GainRupiahThreshold.String()
		variance, rollover := row.VarianceThreshold.String(), row.RolloverThreshold.String()
		result = append(result, apppolicy.RevisionView{RevisionID: row.RevID, PolicyID: row.PolicyID, PolicyKind: "threshold", StationID: row.StationID, ValidFrom: row.ValidFrom.UTC().Format(time.RFC3339Nano), Disabled: row.Disabled, TombstoneReason: row.TombstoneReason, LossLiterThreshold: &lossLiters, GainLiterThreshold: &gainLiters, LossRupiahThreshold: &lossRupiah, GainRupiahThreshold: &gainRupiah, VarianceThreshold: &variance, RolloverThreshold: &rollover})
	}
	for _, row := range evidence {
		mode := row.Mode
		result = append(result, apppolicy.RevisionView{RevisionID: row.RevID, PolicyID: row.PolicyID, PolicyKind: "evidence", StationID: row.StationID, ValidFrom: row.ValidFrom.UTC().Format(time.RFC3339Nano), Disabled: row.Disabled, TombstoneReason: row.TombstoneReason, EvidenceMode: &mode})
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].ValidFrom < result[j].ValidFrom })
	return result, nil
}

// NewPolicyRepository creates a policy repository.
func NewPolicyRepository(store *Store) *PolicyRepository {
	if store == nil {
		return &PolicyRepository{}
	}
	return &PolicyRepository{db: store.DB}
}

// CreateRevision appends one threshold or evidence policy revision.
func (r *PolicyRepository) CreateRevision(ctx context.Context, request apppolicy.PolicyRevisionRequest, now time.Time) (apppolicy.PolicyRevision, error) {
	if r == nil || r.db == nil {
		return apppolicy.PolicyRevision{}, apppolicy.ErrInvalidRequest
	}
	var result apppolicy.PolicyRevision
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if request.StationID != uuid.Nil {
			var station StationModel
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ?", request.OrgID, request.StationID).First(&station).Error; err != nil {
				return apppolicy.ErrInvalidRequest
			}
		}
		roleQuery := tx.Table("user_station_roles").Where("org_id = ? and user_id = ? and role = ?", request.OrgID, request.ActorID, request.Role)
		if request.StationID != uuid.Nil {
			roleQuery = roleQuery.Where("station_id = ?", request.StationID)
		}
		var roleCount int64
		if err := roleQuery.Count(&roleCount).Error; err != nil {
			return fmt.Errorf("check policy role: %w", err)
		}
		if roleCount == 0 || (request.Role != "Owner" && request.Role != "Superadmin") {
			return apppolicy.ErrPolicyRoleRequired
		}
		if request.SupersedesRevisionID != nil {
			validFrom, err := r.supersededValidFrom(tx, request, *request.SupersedesRevisionID)
			if err != nil {
				return err
			}
			if !request.ValidFrom.After(validFrom) {
				return apppolicy.ErrPolicyOverlap
			}
		}
		var duplicateCount int64
		if request.PolicyKind == "threshold" {
			query := tx.Model(&ThresholdPolicyRevisionModel{}).Where("org_id = ? and policy_id = ? and valid_from = ?", request.OrgID, request.PolicyID, request.ValidFrom)
			if request.StationID == uuid.Nil {
				query = query.Where("station_id is null")
			} else {
				query = query.Where("station_id = ?", request.StationID)
			}
			if err := query.Count(&duplicateCount).Error; err != nil {
				return fmt.Errorf("check threshold revision overlap: %w", err)
			}
		} else {
			query := tx.Model(&EvidencePolicyRevisionModel{}).Where("org_id = ? and policy_id = ? and valid_from = ?", request.OrgID, request.PolicyID, request.ValidFrom)
			if request.StationID == uuid.Nil {
				query = query.Where("station_id is null")
			} else {
				query = query.Where("station_id = ?", request.StationID)
			}
			if err := query.Count(&duplicateCount).Error; err != nil {
				return fmt.Errorf("check evidence revision overlap: %w", err)
			}
		}
		if duplicateCount != 0 {
			return apppolicy.ErrPolicyOverlap
		}
		revisionID := uuid.New()
		var supersedesOrgID *uuid.UUID
		if request.SupersedesRevisionID != nil {
			supersedesOrgID = &request.OrgID
		}
		if request.PolicyKind == "threshold" {
			model := ThresholdPolicyRevisionModel{RevID: revisionID, PolicyID: request.PolicyID, OrgID: request.OrgID, StationID: optionalUUID(request.StationID), ValidFrom: request.ValidFrom, SupersedesOrgID: supersedesOrgID, SupersedesRevID: request.SupersedesRevisionID, Disabled: request.Disabled, TombstoneReason: optionalString(request.TombstoneReason), LossLiterThreshold: Decimal(request.LossLiterThreshold), GainLiterThreshold: Decimal(request.GainLiterThreshold), LossRupiahThreshold: Decimal(request.LossRupiahThreshold), GainRupiahThreshold: Decimal(request.GainRupiahThreshold), VarianceThreshold: Decimal(request.VarianceRupiahThreshold), RolloverThreshold: Decimal(request.RolloverThreshold), CreatedBy: request.ActorID, CreatedAt: now}
			if err := tx.Create(&model).Error; err != nil {
				return fmt.Errorf("create threshold policy revision: %w", err)
			}
		} else {
			model := EvidencePolicyRevisionModel{RevID: revisionID, PolicyID: request.PolicyID, OrgID: request.OrgID, StationID: optionalUUID(request.StationID), ValidFrom: request.ValidFrom, SupersedesOrgID: supersedesOrgID, SupersedesRevID: request.SupersedesRevisionID, Mode: request.EvidenceMode, Disabled: request.Disabled, TombstoneReason: optionalString(request.TombstoneReason), CreatedBy: request.ActorID, CreatedAt: now}
			if err := tx.Create(&model).Error; err != nil {
				return fmt.Errorf("create evidence policy revision: %w", err)
			}
		}
		station := ""
		if request.StationID != uuid.Nil {
			station = request.StationID.String()
		}
		auditPayload, err := json.Marshal(map[string]any{"revision_id": revisionID.String(), "policy_id": request.PolicyID.String(), "policy_kind": request.PolicyKind, "station_id": station, "disabled": request.Disabled})
		if err != nil {
			return fmt.Errorf("encode policy audit payload: %w", err)
		}
		if _, err := auditrepository.AppendInTransaction(tx, appaudit.AppendRequest{OrgID: request.OrgID, EventID: revisionID, EventType: "policy.revision.created", Payload: auditPayload, Outcome: "success"}, now); err != nil {
			return fmt.Errorf("append policy audit event: %w", err)
		}
		result = apppolicy.PolicyRevision{RevisionID: revisionID, PolicyID: request.PolicyID, PolicyKind: request.PolicyKind, ValidFrom: request.ValidFrom, Disabled: request.Disabled}
		return nil
	})
	if err != nil {
		return apppolicy.PolicyRevision{}, err
	}
	return result, nil
}

func (r *PolicyRepository) supersededValidFrom(tx *gorm.DB, request apppolicy.PolicyRevisionRequest, revisionID uuid.UUID) (time.Time, error) {
	if request.PolicyKind == "threshold" {
		var revision ThresholdPolicyRevisionModel
		if err := tx.Where("org_id = ? and rev_id = ?", request.OrgID, revisionID).First(&revision).Error; err != nil {
			return time.Time{}, apppolicy.ErrInvalidRequest
		}
		return revision.ValidFrom, nil
	}
	var revision EvidencePolicyRevisionModel
	if err := tx.Where("org_id = ? and rev_id = ?", request.OrgID, revisionID).First(&revision).Error; err != nil {
		return time.Time{}, apppolicy.ErrInvalidRequest
	}
	return revision.ValidFrom, nil
}

func optionalUUID(value uuid.UUID) *uuid.UUID {
	if value == uuid.Nil {
		return nil
	}
	return &value
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
