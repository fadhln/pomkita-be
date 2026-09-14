package gormstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaudit "github.com/pomkita/pomkita-be/internal/service/audit"
	appgovernance "github.com/pomkita/pomkita-be/internal/service/governance"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GovernanceRepository persists report acknowledgement decisions.
type GovernanceRepository struct {
	db *gorm.DB
}

// NewGovernanceRepository creates a governance repository.
func NewGovernanceRepository(store *Store) *GovernanceRepository {
	if store == nil {
		return &GovernanceRepository{}
	}
	return &GovernanceRepository{db: store.db}
}

// Acknowledge records one decision and advances the shift state atomically.
func (r *GovernanceRepository) Acknowledge(ctx context.Context, request appgovernance.AcknowledgeRequest, now time.Time) (appgovernance.Acknowledgement, error) {
	if r == nil || r.db == nil {
		return appgovernance.Acknowledgement{}, appgovernance.ErrDependencyUnavailable
	}
	var result appgovernance.Acknowledgement
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var station StationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ?", request.OrgID, request.StationID).First(&station).Error; err != nil {
			return fmt.Errorf("lock acknowledgement station: %w", err)
		}
		var shift ShiftModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ? and shift_id = ?", request.OrgID, request.StationID, request.ShiftID).First(&shift).Error; err != nil {
			return appgovernance.ErrAckReportNotFound
		}
		if shift.CurrentReportID == nil || *shift.CurrentReportID != request.ReportID || shift.Status != "awaiting_confirmation" {
			return appgovernance.ErrAckReportUnavailable
		}
		var report ShiftReportModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ? and version_no = ?", request.OrgID, request.StationID, request.ShiftID, request.ReportID, request.VersionNo).First(&report).Error; err != nil {
			return appgovernance.ErrAckReportNotFound
		}
		var head AckHeadModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ? and version_no = ?", request.OrgID, request.StationID, request.ShiftID, request.ReportID, request.VersionNo).First(&head).Error; err != nil {
			return appgovernance.ErrAckReportNotFound
		}
		var roleCount int64
		if err := tx.Table("user_station_roles").Where("org_id = ? and station_id = ? and user_id = ? and role = ?", request.OrgID, request.StationID, request.ActorID, request.Role).Count(&roleCount).Error; err != nil {
			return fmt.Errorf("check acknowledgement role: %w", err)
		}
		if roleCount != 1 || (request.Role != "Station Admin" && request.Role != "Owner" && request.Role != "Superadmin") {
			return appgovernance.ErrAckRoleRequired
		}
		if report.SubmittedBy == request.ActorID || r.createdReportDataByActor(tx, request, request.ActorID) {
			return appgovernance.ErrAckSeparationRequired
		}
		if head.ActiveAckID != nil {
			return appgovernance.ErrAckAlreadyDecided
		}
		rejectionReason, breakGlassReason, err := acknowledgementReasons(request)
		if err != nil {
			return err
		}
		var lastSeq int64
		if err := tx.Model(&AckDecisionModel{}).Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ? and version_no = ?", request.OrgID, request.StationID, request.ShiftID, request.ReportID, request.VersionNo).Select("coalesce(max(ack_seq), 0)").Scan(&lastSeq).Error; err != nil {
			return fmt.Errorf("load acknowledgement sequence: %w", err)
		}
		ack := AckDecisionModel{
			AckID:            uuid.New(),
			OrgID:            request.OrgID,
			StationID:        request.StationID,
			ShiftID:          request.ShiftID,
			ReportID:         request.ReportID,
			VersionNo:        request.VersionNo,
			AckSeq:           lastSeq + 1,
			Decision:         request.Decision,
			ActorUserID:      request.ActorID,
			DecidedAt:        now,
			RejectionReason:  rejectionReason,
			IsSuperadmin:     request.Role == "Superadmin",
			IsBreakGlass:     request.IsBreakGlass,
			BreakGlassReason: breakGlassReason,
		}
		if err := tx.Create(&ack).Error; err != nil {
			return fmt.Errorf("create acknowledgement: %w", err)
		}
		if err := tx.Model(&AckHeadModel{}).Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ? and version_no = ?", request.OrgID, request.StationID, request.ShiftID, request.ReportID, request.VersionNo).Update("active_ack_id", ack.AckID).Error; err != nil {
			return fmt.Errorf("activate acknowledgement: %w", err)
		}
		status := "needs_correction"
		if request.Decision == "acked" {
			status = "locked"
		}
		if err := tx.Model(&ShiftModel{}).Where("org_id = ? and station_id = ? and shift_id = ?", request.OrgID, request.StationID, request.ShiftID).Update("status", status).Error; err != nil {
			return fmt.Errorf("update acknowledged shift: %w", err)
		}
		auditPayload, err := json.Marshal(map[string]any{"ack_id": ack.AckID.String(), "report_id": request.ReportID.String(), "decision": request.Decision, "shift_status": status})
		if err != nil {
			return fmt.Errorf("encode acknowledgement audit payload: %w", err)
		}
		if _, err := appendAuditInTransaction(tx, appaudit.AppendRequest{OrgID: request.OrgID, EventID: ack.AckID, EventType: "report.acknowledged", Payload: auditPayload, Outcome: request.Decision}, now); err != nil {
			return fmt.Errorf("append acknowledgement audit event: %w", err)
		}
		result = appgovernance.Acknowledgement{AckID: ack.AckID, ReportID: request.ReportID, VersionNo: request.VersionNo, Decision: request.Decision, ShiftStatus: status, IsBreakGlass: request.IsBreakGlass}
		return nil
	})
	if err != nil {
		return appgovernance.Acknowledgement{}, err
	}
	return result, nil
}

func (r *GovernanceRepository) createdReportDataByActor(tx *gorm.DB, request appgovernance.AcknowledgeRequest, actorID uuid.UUID) bool {
	var count int64
	if tx.Table("sales_declared").Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ? and created_by = ?", request.OrgID, request.StationID, request.ShiftID, request.ReportID, actorID).Count(&count).Error == nil && count > 0 {
		return true
	}
	if tx.Table("loss_entries").Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ? and created_by = ?", request.OrgID, request.StationID, request.ShiftID, request.ReportID, actorID).Count(&count).Error == nil && count > 0 {
		return true
	}
	return false
}

func acknowledgementReasons(request appgovernance.AcknowledgeRequest) (*string, *string, error) {
	var rejectionReason *string
	var breakGlassReason *string
	if request.Decision == "rejected" {
		reason := strings.TrimSpace(request.RejectionReason)
		if reason == "" {
			return nil, nil, appgovernance.ErrRejectionReasonRequired
		}
		rejectionReason = &reason
	} else if strings.TrimSpace(request.RejectionReason) != "" {
		return nil, nil, appgovernance.ErrUnexpectedRejectionReason
	}
	if request.IsBreakGlass {
		if request.Role != "Owner" && request.Role != "Superadmin" {
			return nil, nil, appgovernance.ErrAckRoleRequired
		}
		reason := strings.TrimSpace(request.BreakGlassReason)
		if reason == "" {
			return nil, nil, appgovernance.ErrBreakGlassReasonRequired
		}
		breakGlassReason = &reason
	} else if strings.TrimSpace(request.BreakGlassReason) != "" {
		return nil, nil, appgovernance.ErrUnexpectedBreakGlassReason
	}
	return rejectionReason, breakGlassReason, nil
}

// RequestAmendment creates one pending amendment and stores its base hash.
func (r *GovernanceRepository) RequestAmendment(ctx context.Context, request appgovernance.AmendmentRequest, now time.Time) (appgovernance.Amendment, error) {
	if r == nil || r.db == nil {
		return appgovernance.Amendment{}, appgovernance.ErrDependencyUnavailable
	}
	var result appgovernance.Amendment
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var station StationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ?", request.OrgID, request.StationID).First(&station).Error; err != nil {
			return appgovernance.ErrAmendmentNotFound
		}
		var shift ShiftModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ? and shift_id = ?", request.OrgID, request.StationID, request.ShiftID).First(&shift).Error; err != nil {
			return appgovernance.ErrAmendmentBaseUnavailable
		}
		if shift.CurrentReportID == nil || *shift.CurrentReportID != request.BaseReportID || (shift.Status != "locked" && shift.Status != "awaiting_confirmation" && shift.Status != "needs_correction") {
			return appgovernance.ErrAmendmentBaseUnavailable
		}
		var report ShiftReportModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ?", request.OrgID, request.StationID, request.ShiftID, request.BaseReportID).First(&report).Error; err != nil {
			return appgovernance.ErrAmendmentBaseUnavailable
		}
		var roleCount int64
		if err := tx.Table("user_station_roles").Where("org_id = ? and station_id = ? and user_id = ? and role = ?", request.OrgID, request.StationID, request.RequesterID, "Supervisor").Count(&roleCount).Error; err != nil {
			return fmt.Errorf("check amendment requester role: %w", err)
		}
		if roleCount != 1 {
			return appgovernance.ErrAmendmentRoleRequired
		}
		var pendingCount int64
		if err := tx.Model(&AmendmentModel{}).Where("org_id = ? and station_id = ? and base_report_id = ? and status = ?", request.OrgID, request.StationID, request.BaseReportID, "pending").Count(&pendingCount).Error; err != nil {
			return fmt.Errorf("check pending amendment: %w", err)
		}
		if pendingCount != 0 {
			return appgovernance.ErrAmendmentPendingExists
		}
		for _, item := range request.Items {
			if item.TargetLogicalID == uuid.Nil || !repositoryAmendmentFieldAllowed(item.TargetKind, item.Field) || len(item.OldValue) == 0 {
				return appgovernance.ErrAmendmentFieldForbidden
			}
		}
		hash, err := r.reportSnapshotHash(tx, report)
		if err != nil {
			return err
		}
		amendment := AmendmentModel{AmendmentID: uuid.New(), OrgID: request.OrgID, StationID: request.StationID, ShiftID: request.ShiftID, BaseReportID: request.BaseReportID, Reason: strings.TrimSpace(request.Reason), Status: "pending", RequesterUserID: request.RequesterID, RequestedAt: now, StaleCheckHash: hash, IsBreakGlass: request.IsBreakGlass}
		if strings.TrimSpace(request.BreakGlassReason) != "" {
			reason := strings.TrimSpace(request.BreakGlassReason)
			amendment.BreakGlassReason = &reason
		}
		if err := tx.Create(&amendment).Error; err != nil {
			return fmt.Errorf("create amendment: %w", err)
		}
		for _, item := range request.Items {
			model := AmendmentItemModel{ItemID: uuid.New(), AmendmentID: amendment.AmendmentID, OrgID: request.OrgID, StationID: request.StationID, ShiftID: request.ShiftID, TargetKind: item.TargetKind, TargetLogicalID: item.TargetLogicalID, Field: item.Field, OldValue: append([]byte(nil), item.OldValue...), NewValue: append([]byte(nil), item.NewValue...)}
			if err := tx.Create(&model).Error; err != nil {
				return fmt.Errorf("create amendment item: %w", err)
			}
		}
		auditPayload, err := json.Marshal(map[string]any{"amendment_id": amendment.AmendmentID.String(), "base_report_id": amendment.BaseReportID.String(), "item_count": len(request.Items), "break_glass": amendment.IsBreakGlass})
		if err != nil {
			return fmt.Errorf("encode amendment audit payload: %w", err)
		}
		if _, err := appendAuditInTransaction(tx, appaudit.AppendRequest{OrgID: request.OrgID, EventID: amendment.AmendmentID, EventType: "amendment.requested", Payload: auditPayload, Outcome: "success"}, now); err != nil {
			return fmt.Errorf("append amendment audit event: %w", err)
		}
		result = appgovernance.Amendment{AmendmentID: amendment.AmendmentID, BaseReportID: amendment.BaseReportID, Status: amendment.Status, StaleCheckHash: append([]byte(nil), hash...), RequestedAt: now}
		return nil
	})
	if err != nil {
		return appgovernance.Amendment{}, err
	}
	return result, nil
}

// RejectAmendment marks one pending amendment as rejected.
func (r *GovernanceRepository) RejectAmendment(ctx context.Context, request appgovernance.RejectAmendmentRequest, now time.Time) error {
	if r == nil || r.db == nil {
		return appgovernance.ErrDependencyUnavailable
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var station StationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ?", request.OrgID, request.StationID).First(&station).Error; err != nil {
			return appgovernance.ErrAmendmentNotFound
		}
		var amendment AmendmentModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ? and amendment_id = ?", request.OrgID, request.StationID, request.AmendmentID).First(&amendment).Error; err != nil {
			return appgovernance.ErrAmendmentNotFound
		}
		var roleCount int64
		if err := tx.Table("user_station_roles").Where("org_id = ? and station_id = ? and user_id = ? and role = ?", request.OrgID, request.StationID, request.ApproverID, request.Role).Count(&roleCount).Error; err != nil {
			return fmt.Errorf("check amendment approver role: %w", err)
		}
		if roleCount != 1 || (request.Role != "Station Admin" && request.Role != "Owner" && request.Role != "Superadmin") {
			return appgovernance.ErrAmendmentRoleRequired
		}
		if amendment.Status != "pending" {
			return appgovernance.ErrAmendmentNotPending
		}
		if amendment.RequesterUserID == request.ApproverID {
			return appgovernance.ErrAmendmentSeparationRequired
		}
		reason := strings.TrimSpace(request.RejectionReason)
		if reason == "" {
			return appgovernance.ErrAmendmentReasonRequired
		}
		decidedAt := now
		updates := map[string]any{"status": "rejected", "approver_user_id": request.ApproverID, "decided_at": decidedAt, "rejection_reason": reason}
		if err := tx.Model(&AmendmentModel{}).Where("org_id = ? and station_id = ? and amendment_id = ?", request.OrgID, request.StationID, request.AmendmentID).Updates(updates).Error; err != nil {
			return fmt.Errorf("reject amendment: %w", err)
		}
		auditPayload, err := json.Marshal(map[string]any{"amendment_id": request.AmendmentID.String(), "reason": reason})
		if err != nil {
			return fmt.Errorf("encode amendment rejection audit payload: %w", err)
		}
		if _, err := appendAuditInTransaction(tx, appaudit.AppendRequest{OrgID: request.OrgID, EventID: uuid.New(), EventType: "amendment.rejected", Payload: auditPayload, Outcome: "rejected"}, now); err != nil {
			return fmt.Errorf("append amendment rejection audit event: %w", err)
		}
		return nil
	})
}

// ApproveAmendment applies an allowlisted amendment as a new report version.
func (r *GovernanceRepository) ApproveAmendment(ctx context.Context, request appgovernance.ApproveAmendmentRequest, now time.Time) (appgovernance.Amendment, error) {
	if r == nil || r.db == nil {
		return appgovernance.Amendment{}, appgovernance.ErrDependencyUnavailable
	}
	var result appgovernance.Amendment
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var station StationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ?", request.OrgID, request.StationID).First(&station).Error; err != nil {
			return appgovernance.ErrAmendmentNotFound
		}
		var amendment AmendmentModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ? and amendment_id = ?", request.OrgID, request.StationID, request.AmendmentID).First(&amendment).Error; err != nil {
			return appgovernance.ErrAmendmentNotFound
		}
		var roleCount int64
		if err := tx.Table("user_station_roles").Where("org_id = ? and station_id = ? and user_id = ? and role = ?", request.OrgID, request.StationID, request.ApproverID, request.Role).Count(&roleCount).Error; err != nil {
			return fmt.Errorf("check amendment approver role: %w", err)
		}
		if roleCount != 1 || (request.Role != "Station Admin" && request.Role != "Owner" && request.Role != "Superadmin") {
			return appgovernance.ErrAmendmentRoleRequired
		}
		if amendment.Status != "pending" {
			return appgovernance.ErrAmendmentNotPending
		}
		if amendment.RequesterUserID == request.ApproverID && !amendment.IsBreakGlass {
			return appgovernance.ErrAmendmentSeparationRequired
		}
		var shift ShiftModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ? and shift_id = ?", amendment.OrgID, amendment.StationID, amendment.ShiftID).First(&shift).Error; err != nil {
			return appgovernance.ErrAmendmentBaseUnavailable
		}
		if shift.CurrentReportID == nil || *shift.CurrentReportID != amendment.BaseReportID {
			return appgovernance.ErrAmendmentBaseUnavailable
		}
		var base ShiftReportModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ?", amendment.OrgID, amendment.StationID, amendment.ShiftID, amendment.BaseReportID).First(&base).Error; err != nil {
			return appgovernance.ErrAmendmentBaseUnavailable
		}
		if base.Status != "submitted" && base.Status != "locked" {
			return appgovernance.ErrAmendmentBaseUnavailable
		}
		currentHash, err := r.reportSnapshotHash(tx, base)
		if err != nil {
			return err
		}
		if len(request.StaleCheckHash) != 32 || !bytes.Equal(request.StaleCheckHash, currentHash) || !bytes.Equal(amendment.StaleCheckHash, currentHash) {
			return appgovernance.ErrAmendmentStale
		}
		var items []AmendmentItemModel
		if err := tx.Where("org_id = ? and station_id = ? and amendment_id = ?", amendment.OrgID, amendment.StationID, amendment.AmendmentID).Order("item_id").Find(&items).Error; err != nil {
			return fmt.Errorf("load amendment items: %w", err)
		}
		if err := r.validateAmendmentItems(tx, base, items); err != nil {
			return err
		}
		newReportID := uuid.New()
		newVersion := base.VersionNo + 1
		newReport := ShiftReportModel{ReportID: newReportID, OrgID: base.OrgID, StationID: base.StationID, ShiftID: base.ShiftID, VersionNo: newVersion, SupersedesID: &base.ReportID, Status: "submitted", SubmittedBy: request.ApproverID, SubmittedAt: now, PolicySnapshot: base.PolicySnapshot}
		if err := tx.Create(&newReport).Error; err != nil {
			return fmt.Errorf("create amended report: %w", err)
		}
		if err := r.copyAmendedReportChildren(tx, base, newReport, items); err != nil {
			return err
		}
		if err := tx.Create(&AckHeadModel{OrgID: newReport.OrgID, StationID: newReport.StationID, ShiftID: newReport.ShiftID, ReportID: newReport.ReportID, VersionNo: newReport.VersionNo}).Error; err != nil {
			return fmt.Errorf("create amended acknowledgement head: %w", err)
		}
		var oldHead AckHeadModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ? and version_no = ?", base.OrgID, base.StationID, base.ShiftID, base.ReportID, base.VersionNo).First(&oldHead).Error; err != nil {
			return appgovernance.ErrAmendmentBaseUnavailable
		}
		if oldHead.ActiveAckID != nil {
			supersession := AckSupersessionModel{SupersessionID: uuid.New(), OldOrgID: base.OrgID, OldStationID: base.StationID, OldShiftID: base.ShiftID, OldReportID: base.ReportID, OldVersionNo: base.VersionNo, SupersededAckID: *oldHead.ActiveAckID, ReplacementOrgID: newReport.OrgID, ReplacementStationID: newReport.StationID, ReplacementShiftID: newReport.ShiftID, ReplacementReportID: newReport.ReportID, ReplacementVersionNo: newReport.VersionNo, Reason: "amendment", CreatedAt: now}
			if err := tx.Create(&supersession).Error; err != nil {
				return fmt.Errorf("create acknowledgement supersession: %w", err)
			}
			if err := tx.Model(&AckHeadModel{}).Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ? and version_no = ?", base.OrgID, base.StationID, base.ShiftID, base.ReportID, base.VersionNo).Update("active_ack_id", nil).Error; err != nil {
				return fmt.Errorf("clear old acknowledgement head: %w", err)
			}
		}
		if err := tx.Model(&ShiftModel{}).Where("org_id = ? and station_id = ? and shift_id = ?", base.OrgID, base.StationID, base.ShiftID).Updates(map[string]any{"current_report_id": newReportID, "status": "awaiting_confirmation"}).Error; err != nil {
			return fmt.Errorf("point amended report: %w", err)
		}
		decidedAt := now
		if err := tx.Model(&AmendmentModel{}).Where("org_id = ? and station_id = ? and amendment_id = ?", amendment.OrgID, amendment.StationID, amendment.AmendmentID).Updates(map[string]any{"status": "approved", "approver_user_id": request.ApproverID, "decided_at": decidedAt, "applied_report_id": newReportID}).Error; err != nil {
			return fmt.Errorf("approve amendment: %w", err)
		}
		auditPayload, err := json.Marshal(map[string]any{"amendment_id": amendment.AmendmentID.String(), "base_report_id": base.ReportID.String(), "applied_report_id": newReportID.String(), "version_no": newVersion})
		if err != nil {
			return fmt.Errorf("encode amendment approval audit payload: %w", err)
		}
		if _, err := appendAuditInTransaction(tx, appaudit.AppendRequest{OrgID: amendment.OrgID, EventID: newReportID, EventType: "amendment.approved", Payload: auditPayload, Outcome: "approved"}, now); err != nil {
			return fmt.Errorf("append amendment approval audit event: %w", err)
		}
		result = appgovernance.Amendment{AmendmentID: amendment.AmendmentID, BaseReportID: base.ReportID, AppliedReportID: newReportID, Status: "approved", VersionNo: newVersion, StaleCheckHash: append([]byte(nil), currentHash...), RequestedAt: amendment.RequestedAt, DecidedAt: now}
		return nil
	})
	if err != nil {
		return appgovernance.Amendment{}, err
	}
	return result, nil
}

func (r *GovernanceRepository) validateAmendmentItems(tx *gorm.DB, report ShiftReportModel, items []AmendmentItemModel) error {
	for _, item := range items {
		if !repositoryAmendmentFieldAllowed(item.TargetKind, item.Field) {
			return appgovernance.ErrAmendmentFieldForbidden
		}
		switch item.TargetKind {
		case "sales_declared":
			var sale SalesDeclaredModel
			if err := tx.Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ? and sales_id = ?", report.OrgID, report.StationID, report.ShiftID, report.ReportID, item.TargetLogicalID).First(&sale).Error; err != nil {
				return appgovernance.ErrAmendmentTargetNotFound
			}
			oldValue := sale.CashAmount.String()
			if item.Field == "cashless_amount" {
				oldValue = sale.CashlessAmount.String()
			}
			if !jsonValueEquals(item.OldValue, oldValue) {
				return appgovernance.ErrAmendmentStale
			}
		case "loss_entry":
			var loss LossEntryModel
			if err := tx.Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ? and row_id = ?", report.OrgID, report.StationID, report.ShiftID, report.ReportID, item.TargetLogicalID).First(&loss).Error; err != nil {
				return appgovernance.ErrAmendmentTargetNotFound
			}
			var oldValue string
			switch item.Field {
			case "liters":
				oldValue = loss.Liters.String()
			case "cash_amount":
				if loss.CashAmount != nil {
					oldValue = loss.CashAmount.String()
				}
			case "note":
				if loss.Note != nil {
					oldValue = *loss.Note
				}
			}
			if !jsonValueEquals(item.OldValue, oldValue) {
				return appgovernance.ErrAmendmentStale
			}
		}
	}
	return nil
}

func (r *GovernanceRepository) copyAmendedReportChildren(tx *gorm.DB, base, replacement ShiftReportModel, items []AmendmentItemModel) error {
	var readings []DispenserReadingModel
	if err := tx.Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ?", base.OrgID, base.StationID, base.ShiftID, base.ReportID).Find(&readings).Error; err != nil {
		return fmt.Errorf("load report readings: %w", err)
	}
	for _, reading := range readings {
		reading.ReadingID = uuid.New()
		reading.ReportID = replacement.ReportID
		if err := tx.Create(&reading).Error; err != nil {
			return fmt.Errorf("copy report reading: %w", err)
		}
	}
	var sales []SalesDeclaredModel
	if err := tx.Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ?", base.OrgID, base.StationID, base.ShiftID, base.ReportID).Find(&sales).Error; err != nil {
		return fmt.Errorf("load report sales: %w", err)
	}
	for _, sale := range sales {
		for _, item := range items {
			if item.TargetKind != "sales_declared" || item.TargetLogicalID != sale.SalesID {
				continue
			}
			value, err := amendmentDecimalValue(item.NewValue)
			if err != nil {
				return appgovernance.ErrAmendmentFieldForbidden
			}
			if item.Field == "cash_amount" {
				sale.CashAmount = value
			} else if item.Field == "cashless_amount" {
				sale.CashlessAmount = value
			}
		}
		sale.SalesID = uuid.New()
		sale.ReportID = replacement.ReportID
		if err := tx.Create(&sale).Error; err != nil {
			return fmt.Errorf("copy report sale: %w", err)
		}
	}
	var losses []LossEntryModel
	if err := tx.Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ?", base.OrgID, base.StationID, base.ShiftID, base.ReportID).Find(&losses).Error; err != nil {
		return fmt.Errorf("load report losses: %w", err)
	}
	for _, loss := range losses {
		for _, item := range items {
			if item.TargetKind != "loss_entry" || item.TargetLogicalID != loss.RowID {
				continue
			}
			switch item.Field {
			case "liters":
				value, err := amendmentDecimalValue(item.NewValue)
				if err != nil {
					return appgovernance.ErrAmendmentFieldForbidden
				}
				loss.Liters = value
			case "cash_amount":
				value, err := amendmentDecimalValue(item.NewValue)
				if err != nil {
					return appgovernance.ErrAmendmentFieldForbidden
				}
				loss.CashAmount = &value
			case "note":
				if string(item.NewValue) == "null" {
					loss.Note = nil
				} else {
					var value string
					if err := json.Unmarshal(item.NewValue, &value); err != nil {
						return appgovernance.ErrAmendmentFieldForbidden
					}
					loss.Note = &value
				}
			}
		}
		loss.RowID = uuid.New()
		loss.ReportID = replacement.ReportID
		loss.VersionNo = replacement.VersionNo
		if err := tx.Create(&loss).Error; err != nil {
			return fmt.Errorf("copy report loss: %w", err)
		}
	}
	return nil
}

func jsonValueEquals(raw []byte, value string) bool {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text == value
	}
	return string(raw) == value
}

func amendmentDecimalValue(raw []byte) (Decimal, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		text = strings.TrimSpace(string(raw))
	}
	return NewDecimal(text)
}

// RecordAlertOccurrence inserts one alert event or returns its existing key.
func (r *GovernanceRepository) RecordAlertOccurrence(ctx context.Context, request appgovernance.AlertOccurrenceRequest, now time.Time) (appgovernance.AlertEvent, error) {
	if r == nil || r.db == nil {
		return appgovernance.AlertEvent{}, appgovernance.ErrDependencyUnavailable
	}
	var result appgovernance.AlertEvent
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rule AlertRuleModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ? and rule_id = ?", request.OrgID, request.StationID, request.RuleID).First(&rule).Error; err != nil {
			return appgovernance.ErrAlertInvalidRequest
		}
		if !rule.Enabled && request.EventType == "fired" {
			result = appgovernance.AlertEvent{}
			return nil
		}
		if request.EventType == "cleared" && request.RelatedFiredEventID == nil {
			return appgovernance.ErrAlertRelatedRequired
		}
		periodBucket := request.PeriodStart.UTC().Truncate(time.Hour)
		var existing AlertEventModel
		query := tx.Where("org_id = ? and station_id = ? and rule_id = ? and subject_kind = ? and subject_id = ? and event_type = ? and period_bucket = ?", request.OrgID, request.StationID, request.RuleID, request.SubjectKind, request.SubjectID, request.EventType, periodBucket)
		if request.EventType == "cleared" {
			query = tx.Where("org_id = ? and station_id = ? and related_fired_event_id = ? and event_type = ?", request.OrgID, request.StationID, *request.RelatedFiredEventID, request.EventType)
		}
		if err := query.First(&existing).Error; err == nil {
			result = appgovernance.AlertEvent{EventID: existing.EventID, Inserted: false, RelatedFiredID: existing.RelatedFiredID}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("load existing alert event: %w", err)
		}
		eventID := uuid.New()
		related := request.RelatedFiredEventID
		if request.EventType == "fired" {
			related = &eventID
		}
		model := AlertEventModel{EventID: eventID, OrgID: request.OrgID, StationID: request.StationID, RuleID: request.RuleID, SubjectKind: request.SubjectKind, SubjectID: request.SubjectID, EventType: request.EventType, PeriodStart: request.PeriodStart, RelatedFiredID: related, SourceKind: request.SourceKind, SourceID: request.SourceID, SourceVersionNo: request.SourceVersionNo, SourceAt: request.SourceAt, CreatedBy: request.CreatedBy, CreatedAt: now}
		if err := tx.Create(&model).Error; err != nil {
			return fmt.Errorf("create alert event: %w", err)
		}
		result = appgovernance.AlertEvent{EventID: eventID, Inserted: true, RelatedFiredID: related}
		return nil
	})
	if err != nil {
		return appgovernance.AlertEvent{}, err
	}
	return result, nil
}

// EvaluateStarvation fires overdue shift alerts and clears them after lock.
func (r *GovernanceRepository) EvaluateStarvation(ctx context.Context, now time.Time, window time.Duration) (int, error) {
	if r == nil || r.db == nil {
		return 0, appgovernance.ErrDependencyUnavailable
	}
	if now.IsZero() || window != 24*time.Hour {
		return 0, appgovernance.ErrAlertInvalidRequest
	}
	inserted := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		cutoff := now.Add(-window)
		var shifts []ShiftModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("status in ? and opened_at <= ?", []string{"open", "awaiting_confirmation", "locked"}, now).Order("org_id, station_id, shift_id").Find(&shifts).Error; err != nil {
			return fmt.Errorf("load starvation shifts: %w", err)
		}
		var rules []AlertRuleModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("rule_type = ?", "starvation").Order("station_id, rule_id").Find(&rules).Error; err != nil {
			return fmt.Errorf("load starvation rules: %w", err)
		}
		rulesByStation := make(map[uuid.UUID][]AlertRuleModel)
		for _, rule := range rules {
			rulesByStation[rule.StationID] = append(rulesByStation[rule.StationID], rule)
		}
		for _, shift := range shifts {
			for _, rule := range rulesByStation[shift.StationID] {
				if shift.Status == "open" || shift.Status == "awaiting_confirmation" {
					if shift.OpenedAt.After(cutoff) || !rule.Enabled {
						continue
					}
					period := shift.OpenedAt.UTC().Truncate(time.Hour)
					var existing AlertEventModel
					err := tx.Where("org_id = ? and station_id = ? and rule_id = ? and subject_kind = ? and subject_id = ? and event_type = ? and period_bucket = ?", shift.OrgID, shift.StationID, rule.RuleID, "shift", shift.ShiftID, "fired", period).First(&existing).Error
					if err == nil {
						continue
					}
					if !errors.Is(err, gorm.ErrRecordNotFound) {
						return fmt.Errorf("load starvation alert: %w", err)
					}
					eventID := uuid.New()
					event := AlertEventModel{EventID: eventID, OrgID: shift.OrgID, StationID: shift.StationID, RuleID: rule.RuleID, SubjectKind: "shift", SubjectID: shift.ShiftID, EventType: "fired", PeriodStart: shift.OpenedAt, RelatedFiredID: &eventID, SourceKind: "scheduler", SourceID: shift.ShiftID, SourceAt: now, CreatedAt: now}
					if err := tx.Create(&event).Error; err != nil {
						return fmt.Errorf("create starvation alert: %w", err)
					}
					inserted++
				}
				if shift.Status != "locked" {
					continue
				}
				var fired []AlertEventModel
				if err := tx.Where("org_id = ? and station_id = ? and rule_id = ? and subject_kind = ? and subject_id = ? and event_type = ?", shift.OrgID, shift.StationID, rule.RuleID, "shift", shift.ShiftID, "fired").Order("created_at, event_id").Find(&fired).Error; err != nil {
					return fmt.Errorf("load fired starvation alerts: %w", err)
				}
				for _, firedEvent := range fired {
					var cleared AlertEventModel
					err := tx.Where("org_id = ? and station_id = ? and related_fired_event_id = ? and event_type = ?", shift.OrgID, shift.StationID, firedEvent.EventID, "cleared").First(&cleared).Error
					if err == nil {
						continue
					}
					if !errors.Is(err, gorm.ErrRecordNotFound) {
						return fmt.Errorf("load cleared starvation alert: %w", err)
					}
					related := firedEvent.EventID
					clear := AlertEventModel{EventID: uuid.New(), OrgID: shift.OrgID, StationID: shift.StationID, RuleID: rule.RuleID, SubjectKind: "shift", SubjectID: shift.ShiftID, EventType: "cleared", PeriodStart: firedEvent.PeriodStart, RelatedFiredID: &related, SourceKind: "shift_transition", SourceID: shift.ShiftID, SourceAt: now, CreatedAt: now}
					if err := tx.Create(&clear).Error; err != nil {
						return fmt.Errorf("create starvation clear: %w", err)
					}
					inserted++
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return inserted, nil
}

func (r *GovernanceRepository) reportSnapshotHash(tx *gorm.DB, report ShiftReportModel) ([]byte, error) {
	var readings []DispenserReadingModel
	if err := tx.Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ?", report.OrgID, report.StationID, report.ShiftID, report.ReportID).Order("reading_id").Find(&readings).Error; err != nil {
		return nil, fmt.Errorf("load amendment readings: %w", err)
	}
	var sales []SalesDeclaredModel
	if err := tx.Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ?", report.OrgID, report.StationID, report.ShiftID, report.ReportID).Order("sales_id").Find(&sales).Error; err != nil {
		return nil, fmt.Errorf("load amendment sales: %w", err)
	}
	var losses []LossEntryModel
	if err := tx.Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ?", report.OrgID, report.StationID, report.ShiftID, report.ReportID).Order("row_id").Find(&losses).Error; err != nil {
		return nil, fmt.Errorf("load amendment losses: %w", err)
	}
	payload, err := json.Marshal(struct {
		ReportID string
		Version  int
		Readings []DispenserReadingModel
		Sales    []SalesDeclaredModel
		Losses   []LossEntryModel
	}{ReportID: report.ReportID.String(), Version: report.VersionNo, Readings: readings, Sales: sales, Losses: losses})
	if err != nil {
		return nil, fmt.Errorf("encode amendment base: %w", err)
	}
	digest := sha256.Sum256(payload)
	return digest[:], nil
}

func repositoryAmendmentFieldAllowed(targetKind, field string) bool {
	switch targetKind {
	case "sales_declared":
		return field == "cash_amount" || field == "cashless_amount"
	case "loss_entry":
		return field == "liters" || field == "cash_amount" || field == "note"
	case "delivery":
		return field == "reference" || field == "delivery_id"
	case "dip_reading":
		return field == "reference" || field == "dip_id"
	default:
		return false
	}
}
