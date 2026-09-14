package gormstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
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
