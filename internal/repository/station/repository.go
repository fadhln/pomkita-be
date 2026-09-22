// Package station persists station administration state.
package station

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	auditrepository "github.com/fadhln/pomkita-be/internal/repository/audit"
	store "github.com/fadhln/pomkita-be/internal/repository/store"
	appaudit "github.com/fadhln/pomkita-be/internal/service/audit"
	appstation "github.com/fadhln/pomkita-be/internal/service/station"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository persists stations and their audit events.
type Repository struct{ db *gorm.DB }

// NewRepository creates a station repository.
func NewRepository(database *store.Store) *Repository {
	if database == nil {
		return &Repository{}
	}
	return &Repository{db: database.DB}
}

// List reads stations in one organization.
func (r *Repository) List(ctx context.Context, orgID uuid.UUID) ([]appstation.Station, error) {
	if r == nil || r.db == nil {
		return nil, appstation.ErrDependencyUnavailable
	}
	var rows []store.StationModel
	if err := r.db.WithContext(ctx).Where("org_id = ?", orgID).Order("station_id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list stations: %w", err)
	}
	result := make([]appstation.Station, 0, len(rows))
	for _, row := range rows {
		result = append(result, station(row))
	}
	return result, nil
}

// Read reads one station in one organization.
func (r *Repository) Read(ctx context.Context, orgID, stationID uuid.UUID) (appstation.Station, error) {
	if r == nil || r.db == nil {
		return appstation.Station{}, appstation.ErrDependencyUnavailable
	}
	var row store.StationModel
	if err := r.db.WithContext(ctx).Where("org_id = ? and station_id = ?", orgID, stationID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return appstation.Station{}, appstation.ErrStationNotFound
		}
		return appstation.Station{}, fmt.Errorf("read station: %w", err)
	}
	return station(row), nil
}

// Create creates a station and appends its audit event atomically.
func (r *Repository) Create(ctx context.Context, orgID uuid.UUID, request appstation.CreateRequest, actorID uuid.UUID, now time.Time) (appstation.Station, error) {
	if r == nil || r.db == nil {
		return appstation.Station{}, appstation.ErrDependencyUnavailable
	}
	var result appstation.Station
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := rejectDuplicateCode(tx, orgID, request.Code, uuid.Nil); err != nil {
			return err
		}
		row := store.StationModel{OrgID: orgID, StationID: uuid.New(), Name: request.Name, Code: optionalString(request.Code), Address: optionalString(request.Address), Timezone: request.Timezone, Enabled: true, CreatedAt: now.UTC()}
		if err := tx.Create(&row).Error; err != nil {
			if isUniqueViolation(err) {
				return appstation.ErrCodeConflict
			}
			return fmt.Errorf("create station: %w", err)
		}
		if err := appendAudit(tx, orgID, row.StationID, actorID, "station.created", nil, station(row), now); err != nil {
			return err
		}
		result = station(row)
		return nil
	})
	return result, err
}

// Update changes a station and appends its audit event atomically.
func (r *Repository) Update(ctx context.Context, orgID, stationID uuid.UUID, request appstation.UpdateRequest, actorID uuid.UUID, now time.Time) (appstation.Station, error) {
	if r == nil || r.db == nil {
		return appstation.Station{}, appstation.ErrDependencyUnavailable
	}
	var result appstation.Station
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row store.StationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ?", orgID, stationID).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return appstation.ErrStationNotFound
			}
			return fmt.Errorf("lock station: %w", err)
		}
		before := station(row)
		updates := map[string]any{"updated_at": now.UTC()}
		if request.Name != nil {
			row.Name, updates["name"] = *request.Name, *request.Name
		}
		if request.Code != nil {
			if err := rejectDuplicateCode(tx, orgID, *request.Code, stationID); err != nil {
				return err
			}
			row.Code, updates["code"] = optionalString(*request.Code), optionalString(*request.Code)
		}
		if request.Address != nil {
			row.Address, updates["address"] = optionalString(*request.Address), optionalString(*request.Address)
		}
		if request.Timezone != nil {
			row.Timezone, updates["timezone"] = *request.Timezone, *request.Timezone
		}
		if err := tx.Model(&store.StationModel{}).Where("org_id = ? and station_id = ?", orgID, stationID).Updates(updates).Error; err != nil {
			if isUniqueViolation(err) {
				return appstation.ErrCodeConflict
			}
			return fmt.Errorf("update station: %w", err)
		}
		updatedAt := now.UTC()
		row.UpdatedAt = &updatedAt
		if err := appendAudit(tx, orgID, stationID, actorID, "station.changed", before, station(row), now); err != nil {
			return err
		}
		result = station(row)
		return nil
	})
	return result, err
}

// Disable disables a station and appends its audit event atomically.
func (r *Repository) Disable(ctx context.Context, orgID, stationID, actorID uuid.UUID, now time.Time) error {
	if r == nil || r.db == nil {
		return appstation.ErrDependencyUnavailable
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row store.StationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and station_id = ?", orgID, stationID).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return appstation.ErrStationNotFound
			}
			return fmt.Errorf("lock station for disable: %w", err)
		}
		before := station(row)
		updatedAt := now.UTC()
		if err := tx.Model(&store.StationModel{}).Where("org_id = ? and station_id = ?", orgID, stationID).Updates(map[string]any{"enabled": false, "updated_at": updatedAt}).Error; err != nil {
			return fmt.Errorf("disable station: %w", err)
		}
		row.Enabled, row.UpdatedAt = false, &updatedAt
		return appendAudit(tx, orgID, stationID, actorID, "station.disabled", before, station(row), now)
	})
}

func station(row store.StationModel) appstation.Station {
	return appstation.Station{OrgID: row.OrgID, StationID: row.StationID, Name: row.Name, Code: stringValue(row.Code), Address: stringValue(row.Address), Timezone: row.Timezone, Enabled: row.Enabled, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func appendAudit(tx *gorm.DB, orgID, stationID, actorID uuid.UUID, eventType string, before, after any, now time.Time) error {
	payload, err := json.Marshal(map[string]any{"actor_user_id": actorID, "target_station_id": stationID, "before": before, "after": after})
	if err != nil {
		return fmt.Errorf("marshal station audit event: %w", err)
	}
	if _, err := auditrepository.AppendInTransaction(tx, appaudit.AppendRequest{OrgID: orgID, EventID: uuid.New(), EventType: eventType, Payload: payload, Outcome: "success"}, now.UTC()); err != nil {
		return fmt.Errorf("append station audit event: %w", err)
	}
	return nil
}

func rejectDuplicateCode(tx *gorm.DB, orgID uuid.UUID, code string, stationID uuid.UUID) error {
	if strings.TrimSpace(code) == "" {
		return nil
	}
	query := tx.Model(&store.StationModel{}).Where("org_id = ? and lower(code) = lower(?)", orgID, strings.TrimSpace(code))
	if stationID != uuid.Nil {
		query = query.Where("station_id <> ?", stationID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return fmt.Errorf("check station code: %w", err)
	}
	if count != 0 {
		return appstation.ErrCodeConflict
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && pgError.Code == "23505"
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
