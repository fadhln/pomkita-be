package gormstore

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
	"gorm.io/gorm"
)

// ReportingRepository reads tenant-scoped report and audit views.
type ReportingRepository struct {
	db *gorm.DB
}

// NewReportingRepository creates a reporting repository.
func NewReportingRepository(store *Store) *ReportingRepository {
	if store == nil {
		return &ReportingRepository{}
	}
	return &ReportingRepository{db: store.db}
}

// ReadReport loads one report and its immutable children in stable order.
func (r *ReportingRepository) ReadReport(ctx context.Context, orgID, stationID, reportID uuid.UUID) (appreporting.ReportView, error) {
	if r == nil || r.db == nil {
		return appreporting.ReportView{}, appreporting.ErrInvalidRequest
	}
	var report ShiftReportModel
	if err := r.db.WithContext(ctx).Where("org_id = ? and station_id = ? and report_id = ?", orgID, stationID, reportID).First(&report).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return appreporting.ReportView{}, appreporting.ErrNotFound
		}
		return appreporting.ReportView{}, fmt.Errorf("load report: %w", err)
	}
	view := appreporting.ReportView{ReportID: report.ReportID, StationID: report.StationID, ShiftID: report.ShiftID, VersionNo: report.VersionNo, Status: report.Status, SubmittedAt: report.SubmittedAt.UTC().Format(time.RFC3339Nano)}
	var readings []DispenserReadingModel
	if err := r.db.WithContext(ctx).Where("org_id = ? and station_id = ? and report_id = ?", orgID, stationID, reportID).Order("nozzle_id").Find(&readings).Error; err != nil {
		return appreporting.ReportView{}, fmt.Errorf("load report readings: %w", err)
	}
	for _, row := range readings {
		view.Readings = append(view.Readings, appreporting.ReadingView{NozzleID: row.NozzleID, MeterStart: row.MeterStart.String(), MeterEnd: row.MeterEnd.String(), ExpectedSale: row.ExpectedSale.String()})
	}
	var sales []SalesDeclaredModel
	if err := r.db.WithContext(ctx).Where("org_id = ? and station_id = ? and report_id = ?", orgID, stationID, reportID).Order("dispenser_id").Find(&sales).Error; err != nil {
		return appreporting.ReportView{}, fmt.Errorf("load report sales: %w", err)
	}
	for _, row := range sales {
		view.Sales = append(view.Sales, appreporting.SalesView{DispenserID: row.DispenserID, CashAmount: row.CashAmount.String(), CashlessAmount: row.CashlessAmount.String()})
	}
	var losses []LossEntryModel
	if err := r.db.WithContext(ctx).Where("org_id = ? and station_id = ? and report_id = ?", orgID, stationID, reportID).Order("row_id").Find(&losses).Error; err != nil {
		return appreporting.ReportView{}, fmt.Errorf("load report losses: %w", err)
	}
	for _, row := range losses {
		var cash *string
		if row.CashAmount != nil {
			value := row.CashAmount.String()
			cash = &value
		}
		view.Losses = append(view.Losses, appreporting.LossView{RowID: row.RowID, LossID: row.LossID, Direction: row.Direction, Liters: row.Liters.String(), CashAmount: cash, Note: row.Note})
	}
	return view, nil
}

// ExportAudit reads one organization's audit chain in sequence order.
func (r *ReportingRepository) ExportAudit(ctx context.Context, orgID uuid.UUID) ([]appreporting.AuditRow, error) {
	if r == nil || r.db == nil || orgID == uuid.Nil {
		return nil, appreporting.ErrInvalidRequest
	}
	var rows []AuditLogModel
	if err := r.db.WithContext(ctx).Where("org_id = ?", orgID).Order("org_sequence").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load audit export: %w", err)
	}
	result := make([]appreporting.AuditRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, appreporting.AuditRow{EventID: row.EventID, OrgSequence: row.OrgSequence, EventType: row.EventType, Payload: append([]byte(nil), row.Payload...), Outcome: row.Outcome, CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano), PrevHash: append([]byte(nil), row.PrevHash...), RowHash: append([]byte(nil), row.RowHash...)})
	}
	return result, nil
}

// Anomalies reads alert occurrences in created order within a tenant scope.
func (r *ReportingRepository) Anomalies(ctx context.Context, orgID uuid.UUID, stationID *uuid.UUID) ([]appreporting.AnomalyView, error) {
	if r == nil || r.db == nil || orgID == uuid.Nil {
		return nil, appreporting.ErrInvalidRequest
	}
	query := r.db.WithContext(ctx).Where("org_id = ?", orgID)
	if stationID != nil {
		query = query.Where("station_id = ?", *stationID)
	}
	var rows []AlertEventModel
	if err := query.Order("created_at, event_id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load anomalies: %w", err)
	}
	result := make([]appreporting.AnomalyView, 0, len(rows))
	for _, row := range rows {
		result = append(result, appreporting.AnomalyView{EventID: row.EventID, StationID: row.StationID, RuleID: row.RuleID, SubjectKind: row.SubjectKind, SubjectID: row.SubjectID, EventType: row.EventType, SourceKind: row.SourceKind, SourceID: row.SourceID, SourceVersionNo: row.SourceVersionNo, HappenedAt: row.SourceAt.UTC().Format(time.RFC3339Nano)})
	}
	var decisions []AckDecisionModel
	decisionQuery := r.db.WithContext(ctx).Where("org_id = ? and is_break_glass = ?", orgID, true)
	if stationID != nil {
		decisionQuery = decisionQuery.Where("station_id = ?", *stationID)
	}
	if err := decisionQuery.Order("decided_at, ack_id").Find(&decisions).Error; err != nil {
		return nil, fmt.Errorf("load break-glass anomalies: %w", err)
	}
	for _, decision := range decisions {
		version := decision.VersionNo
		result = append(result, appreporting.AnomalyView{EventID: decision.AckID, StationID: decision.StationID, SubjectKind: "report", SubjectID: decision.ReportID, EventType: "fired", SourceKind: "break_glass", SourceID: decision.AckID, SourceVersionNo: &version, HappenedAt: decision.DecidedAt.UTC().Format(time.RFC3339Nano)})
	}
	var amendments []AmendmentModel
	amendmentQuery := r.db.WithContext(ctx).Where("org_id = ? and is_break_glass = ?", orgID, true)
	if stationID != nil {
		amendmentQuery = amendmentQuery.Where("station_id = ?", *stationID)
	}
	if err := amendmentQuery.Order("requested_at, amendment_id").Find(&amendments).Error; err != nil {
		return nil, fmt.Errorf("load break-glass amendment anomalies: %w", err)
	}
	for _, amendment := range amendments {
		result = append(result, appreporting.AnomalyView{EventID: amendment.AmendmentID, StationID: amendment.StationID, SubjectKind: "report", SubjectID: amendment.BaseReportID, EventType: "fired", SourceKind: "break_glass", SourceID: amendment.AmendmentID, HappenedAt: amendment.RequestedAt.UTC().Format(time.RFC3339Nano)})
	}
	var exceptions []LossExceptionModel
	exceptionQuery := r.db.WithContext(ctx).Where("org_id = ?", orgID)
	if stationID != nil {
		exceptionQuery = exceptionQuery.Where("station_id = ?", *stationID)
	}
	if err := exceptionQuery.Order("created_at, exception_id").Find(&exceptions).Error; err != nil {
		return nil, fmt.Errorf("load loss exception anomalies: %w", err)
	}
	for _, exception := range exceptions {
		result = append(result, appreporting.AnomalyView{EventID: exception.ExceptionID, StationID: exception.StationID, SubjectKind: "report", SubjectID: exception.ReportID, EventType: "fired", SourceKind: "loss_exception", SourceID: exception.ExceptionID, HappenedAt: exception.CreatedAt.UTC().Format(time.RFC3339Nano)})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].HappenedAt != result[j].HappenedAt {
			return result[i].HappenedAt < result[j].HappenedAt
		}
		return result[i].EventID.String() < result[j].EventID.String()
	})
	return result, nil
}
