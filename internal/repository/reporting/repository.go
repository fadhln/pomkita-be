package reporting

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
	"gorm.io/gorm"
)

// ReportingRepository reads tenant-scoped report and audit views.
type ReportingRepository struct {
	db *gorm.DB
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

// NewReportingRepository creates a reporting repository.
func NewReportingRepository(store *Store) *ReportingRepository {
	if store == nil {
		return &ReportingRepository{}
	}
	return &ReportingRepository{db: store.DB}
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
	numericAnomalies, err := r.numericAnomalies(ctx, orgID, stationID, rows)
	if err != nil {
		return nil, err
	}
	result = append(result, numericAnomalies...)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].HappenedAt != result[j].HappenedAt {
			return result[i].HappenedAt < result[j].HappenedAt
		}
		return result[i].EventID.String() < result[j].EventID.String()
	})
	return result, nil
}

func (r *ReportingRepository) numericAnomalies(ctx context.Context, orgID uuid.UUID, stationID *uuid.UUID, alertRows []AlertEventModel) ([]appreporting.AnomalyView, error) {
	query := r.db.WithContext(ctx).Where("org_id = ?", orgID)
	if stationID != nil {
		query = query.Where("station_id = ?", *stationID)
	}
	var reports []ShiftReportModel
	if err := query.Order("submitted_at, report_id").Find(&reports).Error; err != nil {
		return nil, fmt.Errorf("load reports for numeric anomalies: %w", err)
	}
	varianceAlerted := make(map[uuid.UUID]bool)
	for _, row := range alertRows {
		if row.EventType == "fired" && row.SourceKind == "report" {
			varianceAlerted[row.SubjectID] = true
		}
	}
	result := make([]appreporting.AnomalyView, 0)
	for _, report := range reports {
		var snapshot PolicySnapshotItemModel
		if err := r.db.WithContext(ctx).Where("org_id = ? and station_id = ? and shift_id = ? and set_id = ? and policy_kind = ?", report.OrgID, report.StationID, report.ShiftID, report.PolicySnapshot, "threshold").First(&snapshot).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				continue
			}
			return nil, fmt.Errorf("load threshold snapshot for numeric anomalies: %w", err)
		}
		var policy thresholdSnapshotPayload
		if err := json.Unmarshal(snapshot.Payload, &policy); err != nil {
			return nil, fmt.Errorf("decode threshold snapshot for numeric anomalies: %w", err)
		}
		lossThreshold, err := decimalRat(policy.LossLiterThreshold)
		if err != nil {
			return nil, fmt.Errorf("parse loss threshold: %w", err)
		}
		gainThreshold, err := decimalRat(policy.GainLiterThreshold)
		if err != nil {
			return nil, fmt.Errorf("parse gain threshold: %w", err)
		}
		lossRupiahThreshold, err := decimalRat(policy.LossRupiahThreshold)
		if err != nil {
			return nil, fmt.Errorf("parse loss Rupiah threshold: %w", err)
		}
		gainRupiahThreshold, err := decimalRat(policy.GainRupiahThreshold)
		if err != nil {
			return nil, fmt.Errorf("parse gain Rupiah threshold: %w", err)
		}
		varianceThreshold, err := decimalRat(policy.VarianceRupiah)
		if err != nil {
			return nil, fmt.Errorf("parse variance threshold: %w", err)
		}
		var losses []LossEntryModel
		if err := r.db.WithContext(ctx).Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ?", report.OrgID, report.StationID, report.ShiftID, report.ReportID).Find(&losses).Error; err != nil {
			return nil, fmt.Errorf("load losses for numeric anomalies: %w", err)
		}
		lossLiters := new(big.Rat)
		gainLiters := new(big.Rat)
		lossRupiah := new(big.Rat)
		gainRupiah := new(big.Rat)
		for _, loss := range losses {
			liters, err := decimalRat(loss.Liters.String())
			if err != nil {
				return nil, fmt.Errorf("parse loss liters: %w", err)
			}
			cash := new(big.Rat)
			if loss.CashAmount != nil {
				cash, err = decimalRat(loss.CashAmount.String())
				if err != nil {
					return nil, fmt.Errorf("parse loss Rupiah: %w", err)
				}
			}
			if loss.Direction == "loss" {
				lossLiters.Add(lossLiters, liters)
				lossRupiah.Add(lossRupiah, cash)
			} else if loss.Direction == "gain" {
				gainLiters.Add(gainLiters, liters)
				gainRupiah.Add(gainRupiah, cash)
			}
		}
		if lossLiters.Cmp(lossThreshold) > 0 || lossRupiah.Cmp(lossRupiahThreshold) > 0 {
			result = append(result, numericAnomaly(report, "loss_threshold"))
		}
		if gainLiters.Cmp(gainThreshold) > 0 || gainRupiah.Cmp(gainRupiahThreshold) > 0 {
			result = append(result, numericAnomaly(report, "gain_threshold"))
		}
		if !varianceAlerted[report.ReportID] {
			variance, err := r.reportVariance(ctx, report)
			if err != nil {
				return nil, err
			}
			if new(big.Rat).Abs(variance).Cmp(varianceThreshold) > 0 {
				result = append(result, numericAnomaly(report, "variance"))
			}
		}
	}
	return result, nil
}

func (r *ReportingRepository) reportVariance(ctx context.Context, report ShiftReportModel) (*big.Rat, error) {
	var readings []DispenserReadingModel
	if err := r.db.WithContext(ctx).Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ?", report.OrgID, report.StationID, report.ShiftID, report.ReportID).Find(&readings).Error; err != nil {
		return nil, fmt.Errorf("load readings for variance anomaly: %w", err)
	}
	var sales []SalesDeclaredModel
	if err := r.db.WithContext(ctx).Where("org_id = ? and station_id = ? and shift_id = ? and report_id = ?", report.OrgID, report.StationID, report.ShiftID, report.ReportID).Find(&sales).Error; err != nil {
		return nil, fmt.Errorf("load sales for variance anomaly: %w", err)
	}
	expected := new(big.Rat)
	declared := new(big.Rat)
	for _, reading := range readings {
		value, err := decimalRat(reading.ExpectedSale.String())
		if err != nil {
			return nil, fmt.Errorf("parse expected sale: %w", err)
		}
		expected.Add(expected, value)
	}
	for _, sale := range sales {
		cash, err := decimalRat(sale.CashAmount.String())
		if err != nil {
			return nil, fmt.Errorf("parse declared cash: %w", err)
		}
		cashless, err := decimalRat(sale.CashlessAmount.String())
		if err != nil {
			return nil, fmt.Errorf("parse declared cashless: %w", err)
		}
		declared.Add(declared, cash)
		declared.Add(declared, cashless)
	}
	return new(big.Rat).Sub(expected, declared), nil
}

func decimalRat(value string) (*big.Rat, error) {
	rat, ok := new(big.Rat).SetString(strings.TrimSpace(value))
	if !ok {
		return nil, fmt.Errorf("invalid decimal %q", value)
	}
	return rat, nil
}

func numericAnomaly(report ShiftReportModel, source string) appreporting.AnomalyView {
	eventID := uuid.NewSHA1(uuid.Nil, []byte(report.ReportID.String()+":"+source))
	version := report.VersionNo
	return appreporting.AnomalyView{EventID: eventID, StationID: report.StationID, SubjectKind: "report", SubjectID: report.ReportID, EventType: "fired", SourceKind: source, SourceID: report.ReportID, SourceVersionNo: &version, HappenedAt: report.SubmittedAt.UTC().Format(time.RFC3339Nano)}
}
