package shift

import (
	"context"

	"github.com/google/uuid"
)

// Summary is a tenant-scoped shift list row.
type Summary struct {
	ShiftID         uuid.UUID  `json:"shift_id"`
	StationID       uuid.UUID  `json:"station_id"`
	StationSeq      int64      `json:"station_seq"`
	SupervisorID    uuid.UUID  `json:"supervisor_id"`
	OpenedAt        string     `json:"opened_at"`
	BusinessDate    string     `json:"business_date"`
	Status          string     `json:"status"`
	CurrentReportID *uuid.UUID `json:"current_report_id,omitempty"`
}

// Detail is one tenant-scoped shift with its draft state.
type Detail struct {
	Summary
	DraftID  *uuid.UUID `json:"draft_id,omitempty"`
	Revision *int       `json:"revision,omitempty"`
}

// ReadRepository reads shifts within the verified tenant scope.
type ReadRepository interface {
	List(context.Context, uuid.UUID, *uuid.UUID) ([]Summary, error)
	Detail(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (Detail, error)
}

// List returns shifts visible to an organization and optional station.
func (s *Service) List(ctx context.Context, orgID uuid.UUID, stationID *uuid.UUID) ([]Summary, error) {
	if s == nil || s.repository == nil {
		return nil, ErrDependencyUnavailable
	}
	if orgID == uuid.Nil || (stationID != nil && *stationID == uuid.Nil) {
		return nil, ErrInvalidRequest
	}
	repository, ok := s.repository.(ReadRepository)
	if !ok {
		return nil, ErrDependencyUnavailable
	}
	return repository.List(ctx, orgID, stationID)
}

// Detail returns one shift visible to its organization and station.
func (s *Service) Detail(ctx context.Context, orgID, stationID, shiftID uuid.UUID) (Detail, error) {
	if s == nil || s.repository == nil {
		return Detail{}, ErrDependencyUnavailable
	}
	if orgID == uuid.Nil || stationID == uuid.Nil || shiftID == uuid.Nil {
		return Detail{}, ErrInvalidRequest
	}
	repository, ok := s.repository.(ReadRepository)
	if !ok {
		return Detail{}, ErrDependencyUnavailable
	}
	return repository.Detail(ctx, orgID, stationID, shiftID)
}
