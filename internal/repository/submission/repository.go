package submission

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	auditrepository "github.com/fadhln/pomkita-be/internal/repository/audit"
	"github.com/fadhln/pomkita-be/internal/repository/store"
	appaudit "github.com/fadhln/pomkita-be/internal/service/audit"
	appgovernance "github.com/fadhln/pomkita-be/internal/service/governance"
	appreconciliation "github.com/fadhln/pomkita-be/internal/service/reconciliation"
	appsubmission "github.com/fadhln/pomkita-be/internal/service/submission"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SubmissionRepository creates immutable version-one reports and idempotency state.
type SubmissionRepository struct {
	db *gorm.DB
}

type catalogSnapshotItem struct {
	DispenserID string `json:"dispenser_id"`
	NozzleID    string `json:"nozzle_id"`
	MeterMax    string `json:"meter_max"`
	Modulus     string `json:"modulus"`
	Price       string `json:"price"`
}

type catalogSnapshot struct {
	HashVersion int                   `json:"hash_version"`
	Items       []catalogSnapshotItem `json:"items"`
}

// NewSubmissionRepository creates a submission repository.
func NewSubmissionRepository(store *store.Store) *SubmissionRepository {
	if store == nil {
		return &SubmissionRepository{}
	}
	return &SubmissionRepository{db: store.DB}
}

// Submit performs the idempotency and report state changes in one transaction.
func (r *SubmissionRepository) Submit(ctx context.Context, request appsubmission.Request, requestHash []byte, payload []byte, now time.Time) (appsubmission.Result, error) {
	if r == nil || r.db == nil {
		return appsubmission.Result{}, appsubmission.ErrDependencyUnavailable
	}
	var result appsubmission.Result
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tookOver := false
		var station store.StationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ?", request.OrgID, request.StationID).First(&station).Error; err != nil {
			return fmt.Errorf("lock station for submit: %w", err)
		}
		var shift store.ShiftModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ? and shift_id = ?", request.OrgID, request.StationID, request.ShiftID).First(&shift).Error; err != nil {
			return fmt.Errorf("load shift for submit: %w", err)
		}
		var draft store.ShiftDraftModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ? and draft_id = ? and shift_id = ?", request.OrgID, request.StationID, request.DraftID, request.ShiftID).First(&draft).Error; err != nil {
			return fmt.Errorf("load draft for submit: %w", err)
		}
		var idem store.SubmitIdempotencyModel
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
			if idem.Status == "in_progress" {
				if idem.ClaimToken == nil || idem.LeaseExpiresAt == nil {
					return appsubmission.ErrIdempotencyConflict
				}
				oldClaimToken := *idem.ClaimToken
				claim := tx.Model(&store.SubmitIdempotencyModel{}).
					Where("idem_id = ? and claim_token = ? and lease_expires_at < ?", idem.IdemID, oldClaimToken, now).
					Updates(map[string]any{"status": "in_progress", "claim_token": request.ClaimToken, "attempt_count": idem.AttemptCount + 1, "lease_started_at": now, "lease_expires_at": now.Add(10 * time.Minute), "updated_at": now, "error_detail": nil})
				if claim.Error != nil {
					return fmt.Errorf("take over submit idempotency: %w", claim.Error)
				}
				if claim.RowsAffected != 1 {
					return appsubmission.ErrIdempotencyConflict
				}
				tookOver = true
			}
		} else if !errors.Is(idemErr, gorm.ErrRecordNotFound) {
			return fmt.Errorf("load submit idempotency: %w", idemErr)
		}
		if shift.Status != "open" {
			return appsubmission.ErrIdempotencyConflict
		}
		if draft.ClaimToken == nil || *draft.ClaimToken != request.ClaimToken || draft.OwnedBy == nil || *draft.OwnedBy != request.ActorID || draft.ClaimExpiresAt == nil || !draft.ClaimExpiresAt.After(now) {
			return fmt.Errorf("submit draft claim: %w", appsubmission.ErrIdempotencyConflict)
		}
		if draft.Revision != request.ExpectedRevision {
			return fmt.Errorf("submit draft revision: %w", appsubmission.ErrIdempotencyConflict)
		}
		if idemErr != nil {
			idem = store.SubmitIdempotencyModel{IdemID: uuid.New(), OrgID: request.OrgID, StationID: request.StationID, ShiftID: request.ShiftID, IdempotencyKey: request.IdempotencyKey, RequestHash: append([]byte(nil), requestHash...), Status: "in_progress", ClaimToken: &request.ClaimToken, AttemptCount: 1, LeaseStartedAt: &now, LeaseExpiresAt: submissionTimePointer(now.Add(10 * time.Minute)), CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&idem).Error; err != nil {
				return fmt.Errorf("create submit idempotency: %w", err)
			}
		} else if !tookOver {
			updates := map[string]any{"status": "in_progress", "claim_token": request.ClaimToken, "attempt_count": idem.AttemptCount + 1, "lease_started_at": now, "lease_expires_at": now.Add(10 * time.Minute), "updated_at": now, "error_detail": nil}
			if err := tx.Model(&store.SubmitIdempotencyModel{}).Where("idem_id = ?", idem.IdemID).Updates(updates).Error; err != nil {
				return fmt.Errorf("take over submit idempotency: %w", err)
			}
		}

		var policySet store.PolicySnapshotSetModel
		if err := tx.Where("org_id = ? and station_id = ? and shift_id = ?", request.OrgID, request.StationID, request.ShiftID).First(&policySet).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			policySet = store.PolicySnapshotSetModel{SetID: uuid.New(), OrgID: request.OrgID, StationID: request.StationID, ShiftID: &request.ShiftID, CreatedAt: now}
			if err := tx.Create(&policySet).Error; err != nil {
				return fmt.Errorf("create policy snapshot set: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("load policy snapshot set: %w", err)
		}
		rolloverThreshold, err := r.ensureThresholdSnapshot(tx, request, policySet, now)
		if err != nil {
			return err
		}
		if err := r.ensureEvidenceSnapshot(tx, request, policySet, now); err != nil {
			return err
		}
		reportID := uuid.New()
		report := store.ShiftReportModel{ReportID: reportID, OrgID: request.OrgID, StationID: request.StationID, ShiftID: request.ShiftID, VersionNo: 1, Status: "submitted", SubmittedBy: request.ActorID, SubmittedAt: now, PolicySnapshot: policySet.SetID}
		if err := tx.Create(&report).Error; err != nil {
			return fmt.Errorf("create shift report: %w", err)
		}
		head := store.AckHeadModel{OrgID: report.OrgID, StationID: report.StationID, ShiftID: report.ShiftID, ReportID: report.ReportID, VersionNo: report.VersionNo}
		if err := tx.Create(&head).Error; err != nil {
			return fmt.Errorf("create acknowledgement head: %w", err)
		}
		if err := r.promoteDraftChildren(tx, shift, draft, report, payload, rolloverThreshold, now); err != nil {
			return err
		}
		if err := r.evaluateVarianceAlerts(tx, report, policySet, now); err != nil {
			return err
		}
		if err := tx.Model(&store.ShiftModel{}).Where("shift_id = ?", request.ShiftID).Updates(map[string]any{"status": "awaiting_confirmation", "current_report_id": reportID, "closed_at": now}).Error; err != nil {
			return fmt.Errorf("move shift to confirmation: %w", err)
		}
		if err := tx.Model(&store.ShiftDraftModel{}).Where("draft_id = ?", request.DraftID).Updates(map[string]any{"status": "submitted", "claim_token": nil, "claim_expires_at": nil, "updated_at": now, "updated_by": request.ActorID}).Error; err != nil {
			return fmt.Errorf("complete draft: %w", err)
		}
		if err := tx.Model(&store.SubmitIdempotencyModel{}).Where("idem_id = ?", idem.IdemID).Updates(map[string]any{"status": "succeeded", "resulting_report_id": reportID, "lease_expires_at": nil, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("complete submit idempotency: %w", err)
		}
		auditPayload, err := json.Marshal(map[string]any{"report_id": reportID.String(), "shift_id": shift.ShiftID.String(), "version_no": report.VersionNo})
		if err != nil {
			return fmt.Errorf("encode submit audit payload: %w", err)
		}
		if _, err := auditrepository.AppendInTransaction(tx, appaudit.AppendRequest{OrgID: request.OrgID, EventID: reportID, EventType: "report.submitted", Payload: auditPayload, Outcome: "success"}, now); err != nil {
			return fmt.Errorf("append submit audit event: %w", err)
		}
		result = appsubmission.Result{ReportID: reportID}
		return nil
	})
	if err != nil {
		if !errors.Is(err, appsubmission.ErrIdempotencyConflict) {
			if markErr := r.markSubmitFailed(ctx, request, requestHash, now); markErr != nil {
				return appsubmission.Result{}, fmt.Errorf("mark failed submission: %v: %w", markErr, err)
			}
		}
		return appsubmission.Result{}, err
	}
	return result, nil
}

func (r *SubmissionRepository) markSubmitFailed(ctx context.Context, request appsubmission.Request, requestHash []byte, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var idem store.SubmitIdempotencyModel
		err := tx.Where("org_id = ? and station_id = ? and shift_id = ? and idempotency_key = ?", request.OrgID, request.StationID, request.ShiftID, request.IdempotencyKey).First(&idem).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			idem = store.SubmitIdempotencyModel{IdemID: uuid.New(), OrgID: request.OrgID, StationID: request.StationID, ShiftID: request.ShiftID, IdempotencyKey: request.IdempotencyKey, RequestHash: append([]byte(nil), requestHash...), Status: "failed", AttemptCount: 1, CreatedAt: now, UpdatedAt: now}
			return tx.Create(&idem).Error
		}
		if err != nil {
			return err
		}
		if string(idem.RequestHash) != string(requestHash) || idem.Status == "succeeded" {
			return nil
		}
		return tx.Model(&store.SubmitIdempotencyModel{}).Where("idem_id = ?", idem.IdemID).Updates(map[string]any{"status": "failed", "error_detail": "submit_failed", "lease_expires_at": nil, "updated_at": now}).Error
	})
}

type submitPayload struct {
	Readings []submitReading `json:"readings"`
	Sales    []submitSale    `json:"sales"`
	Losses   []submitLoss    `json:"losses"`
}

type submitReading struct {
	NozzleID   uuid.UUID `json:"nozzle_id"`
	MeterStart string    `json:"meter_start"`
	MeterEnd   string    `json:"meter_end"`
	Observed   *bool     `json:"observed"`
}

type submitSale struct {
	DispenserID uuid.UUID `json:"dispenser_id"`
}

type submitLoss struct {
	LossID   uuid.UUID `json:"loss_id"`
	NozzleID uuid.UUID `json:"nozzle_id"`
}

func (r *SubmissionRepository) promoteDraftChildren(tx *gorm.DB, shift store.ShiftModel, draft store.ShiftDraftModel, report store.ShiftReportModel, payload []byte, rolloverThreshold string, now time.Time) error {
	var input submitPayload
	if err := json.Unmarshal(payload, &input); err != nil {
		return fmt.Errorf("decode submit payload: %w", appsubmission.ErrInvalidRequest)
	}
	var snapshot catalogSnapshot
	if err := json.Unmarshal(shift.PriceMapSnapshot, &snapshot); err != nil {
		return fmt.Errorf("decode shift catalog snapshot: %w", appsubmission.ErrInvalidRequest)
	}
	readingInput := make(map[uuid.UUID]submitReading, len(input.Readings))
	for _, item := range input.Readings {
		if item.NozzleID != uuid.Nil {
			readingInput[item.NozzleID] = item
		}
	}
	lossInput := make(map[uuid.UUID]submitLoss, len(input.Losses))
	for _, item := range input.Losses {
		if item.LossID != uuid.Nil {
			lossInput[item.LossID] = item
		}
	}
	var draftReadings []store.DraftReadingModel
	if err := tx.Where("org_id = ? and station_id = ? and draft_id = ?", draft.OrgID, draft.StationID, draft.DraftID).Find(&draftReadings).Error; err != nil {
		return fmt.Errorf("load draft readings: %w", err)
	}
	if shift.Backfilled {
		for _, item := range snapshot.Items {
			nozzleID, err := uuid.Parse(item.NozzleID)
			if err != nil {
				return fmt.Errorf("backfill nozzle ID: %w", appsubmission.ErrInvalidRequest)
			}
			var previous store.ShiftModel
			if err := tx.Where("org_id = ? and station_id = ? and station_seq < ? and status = ? and current_report_id is not null", shift.OrgID, shift.StationID, shift.StationSeq, "locked").Order("station_seq desc").First(&previous).Error; err != nil {
				return fmt.Errorf("backfill meter source shift: %w", appsubmission.ErrInvalidRequest)
			}
			var source store.DispenserReadingModel
			if err := tx.Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ? and nozzle_id = ?", shift.OrgID, shift.StationID, previous.ShiftID, *previous.CurrentReportID, nozzleID).First(&source).Error; err != nil {
				return fmt.Errorf("backfill meter source reading: %w", appsubmission.ErrInvalidRequest)
			}
			model := store.DispenserReadingModel{ReadingID: uuid.New(), OrgID: shift.OrgID, StationID: shift.StationID, ShiftID: shift.ShiftID, ReportID: report.ReportID, NozzleID: source.NozzleID, MeterStart: source.MeterStart, MeterEnd: source.MeterEnd, PriceUsed: source.PriceUsed, ExpectedSale: source.ExpectedSale, Observed: false, IsCarriedForward: true, SourceShiftID: &source.ShiftID, SourceReportID: &source.ReportID, SourceReadingID: &source.ReadingID}
			if err := tx.Create(&model).Error; err != nil {
				return fmt.Errorf("create carried-forward reading: %w", err)
			}
		}
	} else {
		for _, row := range draftReadings {
			item, ok := snapshotItem(snapshot, row.NozzleID)
			if !ok {
				return fmt.Errorf("catalog snapshot nozzle %s: %w", row.NozzleID, appsubmission.ErrInvalidRequest)
			}
			meterStart := row.MeterStart.String()
			meterEnd := row.MeterEnd.String()
			if submitted, exists := readingInput[row.NozzleID]; exists {
				if submitted.MeterStart != "" {
					meterStart = submitted.MeterStart
				}
				if submitted.MeterEnd != "" {
					meterEnd = submitted.MeterEnd
				}
			}
			predecessorEnd, resetValue, baselineValue, err := r.meterContinuity(tx, shift, row.NozzleID)
			if err != nil {
				return err
			}
			if err := appreconciliation.ValidateMeterStart(meterStart, predecessorEnd, resetValue, baselineValue); err != nil {
				return fmt.Errorf("validate nozzle %s meter start: %w", row.NozzleID, err)
			}
			delta, err := appreconciliation.CalculateMeterDelta(meterStart, meterEnd, item.Modulus, rolloverThreshold)
			if err != nil {
				return fmt.Errorf("calculate nozzle %s delta: %w", row.NozzleID, err)
			}
			expected, err := appreconciliation.CalculateExpectedSaleRupiah(delta, item.Price)
			if err != nil {
				return fmt.Errorf("calculate nozzle %s sale: %w", row.NozzleID, err)
			}
			observed := true
			if input, exists := readingInput[row.NozzleID]; exists && input.Observed != nil {
				observed = *input.Observed
			}
			model := store.DispenserReadingModel{ReadingID: uuid.New(), OrgID: draft.OrgID, StationID: draft.StationID, ShiftID: shift.ShiftID, ReportID: report.ReportID, NozzleID: row.NozzleID, MeterStart: store.Decimal(meterStart), MeterEnd: store.Decimal(meterEnd), PriceUsed: store.Decimal(item.Price), ExpectedSale: store.Decimal(expected), Observed: observed, IsCarriedForward: false}
			if err := tx.Create(&model).Error; err != nil {
				return fmt.Errorf("create report reading: %w", err)
			}
		}
	}
	var draftSales []store.DraftSalesModel
	if err := tx.Where("org_id = ? and station_id = ? and draft_id = ?", draft.OrgID, draft.StationID, draft.DraftID).Find(&draftSales).Error; err != nil {
		return fmt.Errorf("load draft sales: %w", err)
	}
	for _, row := range draftSales {
		model := store.SalesDeclaredModel{SalesID: uuid.New(), OrgID: draft.OrgID, StationID: draft.StationID, ShiftID: shift.ShiftID, ReportID: report.ReportID, DispenserID: row.DispenserID, CashAmount: row.CashAmount, CashlessAmount: row.CashlessAmount, CreatedBy: row.CreatedBy}
		if err := tx.Create(&model).Error; err != nil {
			return fmt.Errorf("create report sales: %w", err)
		}
	}
	var draftLosses []store.DraftLossModel
	if err := tx.Where("org_id = ? and station_id = ? and draft_id = ?", draft.OrgID, draft.StationID, draft.DraftID).Find(&draftLosses).Error; err != nil {
		return fmt.Errorf("load draft losses: %w", err)
	}
	for _, row := range draftLosses {
		input, ok := lossInput[row.LossID]
		if !ok || input.NozzleID == uuid.Nil {
			return fmt.Errorf("loss %s nozzle: %w", row.LossID, appsubmission.ErrInvalidRequest)
		}
		identity := store.LossIdentityModel{LossID: row.LossID, OrgID: draft.OrgID, StationID: draft.StationID, CreatedBy: row.CreatedBy, CreatedAt: now}
		if err := tx.Create(&identity).Error; err != nil {
			return fmt.Errorf("create loss identity: %w", err)
		}
		entry := store.LossEntryModel{RowID: uuid.New(), OrgID: draft.OrgID, StationID: draft.StationID, ShiftID: shift.ShiftID, ReportID: report.ReportID, VersionNo: report.VersionNo, LossID: row.LossID, NozzleID: input.NozzleID, Direction: row.Direction, ReasonCode: row.ReasonCode, Liters: row.Liters, CashAmount: row.CashAmount, Note: row.Note, CreatedBy: row.CreatedBy}
		if err := tx.Create(&entry).Error; err != nil {
			return fmt.Errorf("create report loss: %w", err)
		}
	}
	var stagedEvidence []store.DraftEvidenceStagingModel
	if err := tx.Where("org_id = ? and station_id = ? and draft_id = ?", draft.OrgID, draft.StationID, draft.DraftID).Find(&stagedEvidence).Error; err != nil {
		return fmt.Errorf("load staged evidence: %w", err)
	}
	if err := r.validateStagedEvidence(tx, draft, draftLosses, stagedEvidence); err != nil {
		return err
	}
	for _, staged := range stagedEvidence {
		var draftLoss store.DraftLossModel
		if err := tx.Where("row_id = ? and draft_id = ?", staged.LossRowID, draft.DraftID).First(&draftLoss).Error; err != nil {
			return fmt.Errorf("load staged evidence loss: %w", err)
		}
		var reportLoss store.LossEntryModel
		if err := tx.Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ? and loss_id = ?", draft.OrgID, draft.StationID, shift.ShiftID, report.ReportID, draftLoss.LossID).First(&reportLoss).Error; err != nil {
			return fmt.Errorf("load report evidence loss: %w", err)
		}
		event := store.EvidenceEventModel{EvidenceEventID: uuid.New(), EvidenceID: staged.RowID, OrgID: draft.OrgID, StationID: draft.StationID, ShiftID: shift.ShiftID, ReportID: report.ReportID, LossRowID: reportLoss.RowID, EventSeq: 1, EventType: staged.Status, EvidenceType: staged.EvidenceType, ObjectKey: staged.ObjectKey, ContentHash: append([]byte(nil), staged.ContentHash...), SizeBytes: staged.SizeBytes, MIME: staged.MIME, ActorUserID: staged.UploadedBy, At: now}
		if err := tx.Create(&event).Error; err != nil {
			return fmt.Errorf("create evidence event: %w", err)
		}
	}
	return nil
}

type thresholdSnapshotPayload struct {
	HashVersion         int    `json:"hash_version"`
	LossLiterThreshold  string `json:"loss_liter_threshold"`
	GainLiterThreshold  string `json:"gain_liter_threshold"`
	LossRupiahThreshold string `json:"loss_rupiah_threshold"`
	GainRupiahThreshold string `json:"gain_rupiah_threshold"`
	VarianceRupiah      string `json:"variance_rupiah_threshold"`
	RolloverThreshold   string `json:"rollover_threshold"`
}

type evidenceSnapshotPayload struct {
	HashVersion int                    `json:"hash_version"`
	Mode        string                 `json:"mode"`
	Types       []evidenceSnapshotType `json:"types"`
}

type evidenceSnapshotType struct {
	Type                string   `json:"type"`
	MinimumCountPerLoss int      `json:"minimum_count_per_loss"`
	AcceptedMIMETypes   []string `json:"accepted_mime_types"`
}

func (r *SubmissionRepository) ensureEvidenceSnapshot(tx *gorm.DB, request appsubmission.Request, policySet store.PolicySnapshotSetModel, now time.Time) error {
	var item store.PolicySnapshotItemModel
	err := tx.Where("org_id = ? and station_id = ? and shift_id = ? and set_id = ? and policy_kind = ?", request.OrgID, request.StationID, request.ShiftID, policySet.SetID, "evidence").First(&item).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("load evidence policy snapshot: %w", err)
	}

	var revision store.EvidencePolicyRevisionModel
	revisionErr := tx.Where("org_id = ? and disabled = false and valid_from <= ? and (station_id = ? or station_id is null)", request.OrgID, now, request.StationID).
		Order("station_id is not null desc").Order("valid_from desc").First(&revision).Error
	if errors.Is(revisionErr, gorm.ErrRecordNotFound) {
		return nil
	}
	if revisionErr != nil {
		return fmt.Errorf("resolve evidence policy: %w", revisionErr)
	}
	var policyTypes []store.EvidencePolicyTypeModel
	if err := tx.Where("org_id = ? and rev_id = ?", revision.OrgID, revision.RevID).Order("evidence_type").Find(&policyTypes).Error; err != nil {
		return fmt.Errorf("load evidence policy types: %w", err)
	}
	if len(policyTypes) == 0 {
		return fmt.Errorf("evidence policy has no types: %w", appsubmission.ErrInvalidRequest)
	}
	payload := evidenceSnapshotPayload{HashVersion: 1, Mode: revision.Mode, Types: make([]evidenceSnapshotType, 0, len(policyTypes))}
	for _, policyType := range policyTypes {
		accepted := append([]string(nil), policyType.AcceptedMIMETypes...)
		sort.Strings(accepted)
		payload.Types = append(payload.Types, evidenceSnapshotType{Type: policyType.EvidenceType, MinimumCountPerLoss: policyType.MinimumCountPerLoss, AcceptedMIMETypes: accepted})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode evidence policy snapshot: %w", err)
	}
	hash := sha256.Sum256(encoded)
	scope := "organization"
	if revision.StationID != nil {
		scope = "station"
	}
	item = store.PolicySnapshotItemModel{ItemID: uuid.New(), OrgID: request.OrgID, StationID: request.StationID, ShiftID: request.ShiftID, SetID: policySet.SetID, PolicyKind: "evidence", PolicyID: revision.PolicyID, RevID: revision.RevID, Scope: scope, Payload: encoded, PayloadHash: hash[:]}
	if err := tx.Create(&item).Error; err != nil {
		return fmt.Errorf("create evidence policy snapshot: %w", err)
	}
	return nil
}

func (r *SubmissionRepository) validateStagedEvidence(tx *gorm.DB, draft store.ShiftDraftModel, losses []store.DraftLossModel, staged []store.DraftEvidenceStagingModel) error {
	var item store.PolicySnapshotItemModel
	err := tx.Where("org_id = ? and station_id = ? and shift_id = ? and policy_kind = ?", draft.OrgID, draft.StationID, draft.ShiftID, "evidence").First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load evidence policy for validation: %w", err)
	}
	var payload evidenceSnapshotPayload
	if err := json.Unmarshal(item.Payload, &payload); err != nil || payload.Mode == "" {
		return fmt.Errorf("decode evidence policy snapshot: %w", appsubmission.ErrInvalidRequest)
	}
	types := make(map[string]evidenceSnapshotType, len(payload.Types))
	for _, policyType := range payload.Types {
		types[policyType.Type] = policyType
	}
	counts := make(map[uuid.UUID]map[string]int)
	for _, evidence := range staged {
		policyType, ok := types[evidence.EvidenceType]
		if !ok {
			return fmt.Errorf("evidence type %q: %w", evidence.EvidenceType, appgovernance.ErrEvidenceInvalid)
		}
		if !containsEvidenceMIME(policyType.AcceptedMIMETypes, evidence.MIME) {
			return fmt.Errorf("evidence MIME %q: %w", evidence.MIME, appgovernance.ErrEvidenceInvalid)
		}
		if evidence.Status == "finalized" {
			if counts[evidence.LossRowID] == nil {
				counts[evidence.LossRowID] = make(map[string]int)
			}
			counts[evidence.LossRowID][evidence.EvidenceType]++
		}
	}
	for _, loss := range losses {
		for _, policyType := range payload.Types {
			finalized := 0
			if counts[loss.RowID] != nil {
				finalized = counts[loss.RowID][policyType.Type]
			}
			if err := appgovernance.ValidateEvidence(appgovernance.EvidenceValidationRequest{Mode: payload.Mode, MinimumCount: policyType.MinimumCountPerLoss, FinalizedCount: finalized}); err != nil {
				return fmt.Errorf("loss %s evidence type %s: %w", loss.LossID, policyType.Type, err)
			}
		}
	}
	return nil
}

func containsEvidenceMIME(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func (r *SubmissionRepository) evaluateVarianceAlerts(tx *gorm.DB, report store.ShiftReportModel, policySet store.PolicySnapshotSetModel, now time.Time) error {
	var snapshot store.PolicySnapshotItemModel
	if err := tx.Where("org_id = ? and station_id = ? and shift_id = ? and set_id = ? and policy_kind = ?", report.OrgID, report.StationID, report.ShiftID, policySet.SetID, "threshold").First(&snapshot).Error; err != nil {
		return fmt.Errorf("load variance policy snapshot: %w", err)
	}
	var policy thresholdSnapshotPayload
	if err := json.Unmarshal(snapshot.Payload, &policy); err != nil || policy.VarianceRupiah == "" {
		return fmt.Errorf("decode variance policy snapshot: %w", appsubmission.ErrInvalidRequest)
	}
	threshold, ok := new(big.Rat).SetString(policy.VarianceRupiah)
	if !ok || threshold.Sign() < 0 {
		return fmt.Errorf("invalid variance threshold: %w", appsubmission.ErrInvalidRequest)
	}
	var readings []store.DispenserReadingModel
	if err := tx.Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ?", report.OrgID, report.StationID, report.ShiftID, report.ReportID).Find(&readings).Error; err != nil {
		return fmt.Errorf("load report readings for variance: %w", err)
	}
	var sales []store.SalesDeclaredModel
	if err := tx.Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ?", report.OrgID, report.StationID, report.ShiftID, report.ReportID).Find(&sales).Error; err != nil {
		return fmt.Errorf("load report sales for variance: %w", err)
	}
	expected := new(big.Rat)
	for _, reading := range readings {
		value, ok := new(big.Rat).SetString(reading.ExpectedSale.String())
		if !ok {
			return fmt.Errorf("invalid expected sale: %w", appsubmission.ErrInvalidRequest)
		}
		expected.Add(expected, value)
	}
	declared := new(big.Rat)
	for _, sale := range sales {
		cash, ok := new(big.Rat).SetString(sale.CashAmount.String())
		if !ok {
			return fmt.Errorf("invalid cash amount: %w", appsubmission.ErrInvalidRequest)
		}
		cashless, ok := new(big.Rat).SetString(sale.CashlessAmount.String())
		if !ok {
			return fmt.Errorf("invalid cashless amount: %w", appsubmission.ErrInvalidRequest)
		}
		declared.Add(declared, new(big.Rat).Add(cash, cashless))
	}
	variance := new(big.Rat).Sub(expected, declared)
	absolute := new(big.Rat).Abs(variance)
	if absolute.Cmp(threshold) <= 0 {
		return nil
	}
	var rules []store.AlertRuleModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ? and rule_type = ? and enabled = true", report.OrgID, report.StationID, "variance").Order("rule_id").Find(&rules).Error; err != nil {
		return fmt.Errorf("load variance alert rules: %w", err)
	}
	version := report.VersionNo
	for _, rule := range rules {
		ruleThreshold, ok := new(big.Rat).SetString(rule.Threshold.String())
		if !ok || ruleThreshold.Sign() < 0 {
			return fmt.Errorf("invalid alert threshold: %w", appsubmission.ErrInvalidRequest)
		}
		if absolute.Cmp(ruleThreshold) <= 0 {
			continue
		}
		eventID := uuid.New()
		event := store.AlertEventModel{EventID: eventID, OrgID: report.OrgID, StationID: report.StationID, RuleID: rule.RuleID, SubjectKind: "report", SubjectID: report.ReportID, EventType: "fired", PeriodStart: now, RelatedFiredID: &eventID, SourceKind: "report", SourceID: report.ReportID, SourceVersionNo: &version, SourceAt: now, CreatedBy: &report.SubmittedBy, CreatedAt: now}
		if err := tx.Create(&event).Error; err != nil {
			return fmt.Errorf("create variance alert: %w", err)
		}
	}
	return nil
}

func (r *SubmissionRepository) ensureThresholdSnapshot(tx *gorm.DB, request appsubmission.Request, policySet store.PolicySnapshotSetModel, now time.Time) (string, error) {
	var item store.PolicySnapshotItemModel
	itemErr := tx.Where("org_id = ? and station_id = ? and shift_id = ? and set_id = ? and policy_kind = ?", request.OrgID, request.StationID, request.ShiftID, policySet.SetID, "threshold").First(&item).Error
	if itemErr == nil {
		var payload thresholdSnapshotPayload
		if err := json.Unmarshal(item.Payload, &payload); err != nil || payload.RolloverThreshold == "" {
			return "", fmt.Errorf("decode threshold policy snapshot: %w", appsubmission.ErrInvalidRequest)
		}
		return payload.RolloverThreshold, nil
	}
	if !errors.Is(itemErr, gorm.ErrRecordNotFound) {
		return "", fmt.Errorf("load threshold policy snapshot: %w", itemErr)
	}

	var revision store.ThresholdPolicyRevisionModel
	revisionErr := tx.Where("org_id = ? and disabled = false and valid_from <= ? and (station_id = ? or station_id is null)", request.OrgID, now, request.StationID).
		Order("station_id is not null desc").Order("valid_from desc").First(&revision).Error
	if revisionErr == gorm.ErrRecordNotFound || errors.Is(revisionErr, gorm.ErrRecordNotFound) || revisionErr != nil && strings.Contains(revisionErr.Error(), gorm.ErrRecordNotFound.Error()) {
		revision = store.ThresholdPolicyRevisionModel{
			RevID:               uuid.New(),
			PolicyID:            uuid.New(),
			OrgID:               request.OrgID,
			ValidFrom:           now,
			LossLiterThreshold:  store.Decimal("10.00"),
			GainLiterThreshold:  store.Decimal("0.00"),
			LossRupiahThreshold: store.Decimal("0"),
			GainRupiahThreshold: store.Decimal("0"),
			VarianceThreshold:   store.Decimal("0"),
			RolloverThreshold:   store.Decimal("10.0"),
			CreatedBy:           request.ActorID,
			CreatedAt:           now,
		}
		if err := tx.Create(&revision).Error; err != nil {
			return "", fmt.Errorf("create default threshold policy: %w", err)
		}
		revisionErr = nil
	}
	if revisionErr != nil {
		return "", fmt.Errorf("resolve threshold policy: %w", revisionErr)
	}
	payload := thresholdSnapshotPayload{
		HashVersion:         1,
		LossLiterThreshold:  revision.LossLiterThreshold.String(),
		GainLiterThreshold:  revision.GainLiterThreshold.String(),
		LossRupiahThreshold: revision.LossRupiahThreshold.String(),
		GainRupiahThreshold: revision.GainRupiahThreshold.String(),
		VarianceRupiah:      revision.VarianceThreshold.String(),
		RolloverThreshold:   revision.RolloverThreshold.String(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode threshold policy snapshot: %w", err)
	}
	hash := sha256.Sum256(encoded)
	scope := "organization"
	if revision.StationID != nil {
		scope = "station"
	}
	item = store.PolicySnapshotItemModel{ItemID: uuid.New(), OrgID: request.OrgID, StationID: request.StationID, ShiftID: request.ShiftID, SetID: policySet.SetID, PolicyKind: "threshold", PolicyID: revision.PolicyID, RevID: revision.RevID, Scope: scope, Payload: encoded, PayloadHash: hash[:]}
	if err := tx.Create(&item).Error; err != nil {
		return "", fmt.Errorf("create threshold policy snapshot: %w", err)
	}
	return payload.RolloverThreshold, nil
}

func (r *SubmissionRepository) meterContinuity(tx *gorm.DB, shift store.ShiftModel, nozzleID uuid.UUID) (string, string, string, error) {
	var reset store.MeterResetEventModel
	resetErr := tx.Where("org_id = ? and station_id = ? and nozzle_id = ? and effective_shift_id = ? and status = ?", shift.OrgID, shift.StationID, nozzleID, shift.ShiftID, "approved").First(&reset).Error
	if resetErr != nil && !errors.Is(resetErr, gorm.ErrRecordNotFound) {
		return "", "", "", fmt.Errorf("load meter reset: %w", resetErr)
	}
	resetValue := ""
	if resetErr == nil {
		resetValue = reset.NewValue.String()
	}
	var baseline struct {
		InitialMeterValue store.Decimal
	}
	baselineErr := tx.Table("nozzle_baseline_revisions r").Select("r.initial_meter_value").Joins("join nozzle_baseline_current c on c.org_id = r.org_id and c.station_id = r.station_id and c.nozzle_id = r.nozzle_id and c.current_baseline_rev_id = r.baseline_rev_id").Where("r.org_id = ? and r.station_id = ? and r.nozzle_id = ? and r.effective_station_seq <= ?", shift.OrgID, shift.StationID, nozzleID, shift.StationSeq).Order("r.effective_station_seq desc").First(&baseline).Error
	if baselineErr != nil && !errors.Is(baselineErr, gorm.ErrRecordNotFound) {
		return "", "", "", fmt.Errorf("load meter baseline: %w", baselineErr)
	}
	baselineValue := ""
	if baselineErr == nil {
		baselineValue = baseline.InitialMeterValue.String()
	}
	var previous store.ShiftModel
	previousErr := tx.Where("org_id = ? and station_id = ? and station_seq < ? and status = ?", shift.OrgID, shift.StationID, shift.StationSeq, "locked").Order("station_seq desc").First(&previous).Error
	if previousErr != nil && !errors.Is(previousErr, gorm.ErrRecordNotFound) {
		return "", "", "", fmt.Errorf("load predecessor shift: %w", previousErr)
	}
	predecessorEnd := ""
	if previousErr == nil && previous.CurrentReportID != nil {
		var reading store.DispenserReadingModel
		readingErr := tx.Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ? and nozzle_id = ?", shift.OrgID, shift.StationID, previous.ShiftID, *previous.CurrentReportID, nozzleID).First(&reading).Error
		if readingErr != nil && !errors.Is(readingErr, gorm.ErrRecordNotFound) {
			return "", "", "", fmt.Errorf("load predecessor reading: %w", readingErr)
		}
		if readingErr == nil {
			predecessorEnd = reading.MeterEnd.String()
		}
	}
	return predecessorEnd, resetValue, baselineValue, nil
}

func snapshotItem(snapshot catalogSnapshot, nozzleID uuid.UUID) (catalogSnapshotItem, bool) {
	for _, item := range snapshot.Items {
		if item.NozzleID == nozzleID.String() {
			return item, true
		}
	}
	return catalogSnapshotItem{}, false
}

func submissionTimePointer(value time.Time) *time.Time {
	return &value
}
