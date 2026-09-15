// Package reporting contains read-only report and audit export use cases.
package reporting

import (
	"context"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/domain"
)

var (
	// ErrInvalidRequest identifies a missing report or organization scope.
	ErrInvalidRequest = domain.NewError(domain.CategoryValidation, "invalid_reporting_request")
	// ErrNotFound identifies a report outside the organization scope.
	ErrNotFound = domain.NewError(domain.CategoryNotFound, "report_not_found")
)

// ReadingView is a decimal-safe report reading.
type ReadingView struct {
	NozzleID     uuid.UUID `json:"nozzle_id"`
	MeterStart   string    `json:"meter_start"`
	MeterEnd     string    `json:"meter_end"`
	ExpectedSale string    `json:"expected_sale"`
}

// SalesView is a decimal-safe declared sales row.
type SalesView struct {
	DispenserID    uuid.UUID `json:"dispenser_id"`
	CashAmount     string    `json:"cash_amount"`
	CashlessAmount string    `json:"cashless_amount"`
}

// LossView is a decimal-safe report loss row.
type LossView struct {
	RowID      uuid.UUID `json:"row_id"`
	LossID     uuid.UUID `json:"loss_id"`
	Direction  string    `json:"direction"`
	Liters     string    `json:"liters"`
	CashAmount *string   `json:"cash_amount,omitempty"`
	Note       *string   `json:"note,omitempty"`
}

// ReportView is the common source for reporting and printout adapters.
type ReportView struct {
	ReportID    uuid.UUID     `json:"report_id"`
	StationID   uuid.UUID     `json:"station_id"`
	ShiftID     uuid.UUID     `json:"shift_id"`
	VersionNo   int           `json:"version_no"`
	Status      string        `json:"status"`
	SubmittedAt string        `json:"submitted_at"`
	Readings    []ReadingView `json:"readings"`
	Sales       []SalesView   `json:"sales"`
	Losses      []LossView    `json:"losses"`
}

// AuditRow is one ordered audit export row.
type AuditRow struct {
	EventID     uuid.UUID `json:"event_id"`
	OrgSequence int64     `json:"org_sequence"`
	EventType   string    `json:"event_type"`
	Payload     []byte    `json:"payload"`
	Outcome     string    `json:"outcome"`
	CreatedAt   string    `json:"created_at"`
	PrevHash    []byte    `json:"prev_hash"`
	RowHash     []byte    `json:"row_hash"`
}

// AnomalyView is one immutable anomaly source row.
type AnomalyView struct {
	EventID         uuid.UUID `json:"event_id"`
	StationID       uuid.UUID `json:"station_id"`
	RuleID          uuid.UUID `json:"rule_id"`
	SubjectKind     string    `json:"subject_kind"`
	SubjectID       uuid.UUID `json:"subject_id"`
	EventType       string    `json:"event_type"`
	SourceKind      string    `json:"source_kind"`
	SourceID        uuid.UUID `json:"source_id"`
	SourceVersionNo *int      `json:"source_version_no,omitempty"`
	HappenedAt      string    `json:"happened_at"`
}

// Repository reads report and audit data within a tenant scope.
type Repository interface {
	ReadReport(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (ReportView, error)
	ExportAudit(context.Context, uuid.UUID) ([]AuditRow, error)
	Anomalies(context.Context, uuid.UUID, *uuid.UUID) ([]AnomalyView, error)
}

// Service owns reporting read use cases.
type Service struct{ repository Repository }

// NewService creates a reporting service.
func NewService(repository Repository) *Service { return &Service{repository: repository} }

// ReadReport reads one immutable report view.
func (s *Service) ReadReport(ctx context.Context, orgID, stationID, reportID uuid.UUID) (ReportView, error) {
	if s == nil || s.repository == nil {
		return ReportView{}, ErrInvalidRequest
	}
	if orgID == uuid.Nil || stationID == uuid.Nil || reportID == uuid.Nil {
		return ReportView{}, ErrInvalidRequest
	}
	return s.repository.ReadReport(ctx, orgID, stationID, reportID)
}

// ExportAudit returns ordered audit rows for one organization.
func (s *Service) ExportAudit(ctx context.Context, orgID uuid.UUID) ([]AuditRow, error) {
	if s == nil || s.repository == nil || orgID == uuid.Nil {
		return nil, ErrInvalidRequest
	}
	return s.repository.ExportAudit(ctx, orgID)
}

// Anomalies reads alert occurrences within an organization and optional station.
func (s *Service) Anomalies(ctx context.Context, orgID uuid.UUID, stationID *uuid.UUID) ([]AnomalyView, error) {
	if s == nil || s.repository == nil || orgID == uuid.Nil || (stationID != nil && *stationID == uuid.Nil) {
		return nil, ErrInvalidRequest
	}
	return s.repository.Anomalies(ctx, orgID, stationID)
}
