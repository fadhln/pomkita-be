// Package organization persists organization administration state.
package organization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	auditrepository "github.com/fadhln/pomkita-be/internal/repository/audit"
	store "github.com/fadhln/pomkita-be/internal/repository/store"
	appaudit "github.com/fadhln/pomkita-be/internal/service/audit"
	apporg "github.com/fadhln/pomkita-be/internal/service/organization"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository persists organization and first-station changes.
type Repository struct{ db *gorm.DB }

// NewRepository creates an organization repository.
func NewRepository(database *store.Store) *Repository {
	if database == nil {
		return &Repository{}
	}
	return &Repository{db: database.DB}
}

// List reads all organizations or one organization in the permitted scope.
func (r *Repository) List(ctx context.Context, orgID uuid.UUID, all bool) ([]apporg.Organization, error) {
	if r == nil || r.db == nil {
		return nil, apporg.ErrDependencyUnavailable
	}
	query := r.db.WithContext(ctx).Order("org_id")
	if !all {
		query = query.Where("org_id = ?", orgID)
	}
	var rows []store.OrganizationModel
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}
	result := make([]apporg.Organization, 0, len(rows))
	for _, row := range rows {
		result = append(result, organization(row))
	}
	return result, nil
}

// Read reads one organization.
func (r *Repository) Read(ctx context.Context, orgID uuid.UUID) (apporg.Organization, error) {
	if r == nil || r.db == nil {
		return apporg.Organization{}, apporg.ErrDependencyUnavailable
	}
	var row store.OrganizationModel
	if err := r.db.WithContext(ctx).Where("org_id = ?", orgID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apporg.Organization{}, apporg.ErrOrganizationNotFound
		}
		return apporg.Organization{}, fmt.Errorf("read organization: %w", err)
	}
	return organization(row), nil
}

// Create creates an organization, its first station, and its audit event atomically.
func (r *Repository) Create(ctx context.Context, request apporg.CreateRequest, actorID uuid.UUID, now time.Time) (apporg.Organization, error) {
	if r == nil || r.db == nil {
		return apporg.Organization{}, apporg.ErrDependencyUnavailable
	}
	if request.FirstStation == nil {
		return apporg.Organization{}, apporg.ErrNoStation
	}
	return createOrganization(r, ctx, request, actorID, now)
}

// Update changes an organization and appends its audit event in one transaction.
func (r *Repository) Update(ctx context.Context, orgID uuid.UUID, request apporg.UpdateRequest, actorID uuid.UUID, now time.Time) (apporg.Organization, error) {
	if r == nil || r.db == nil {
		return apporg.Organization{}, apporg.ErrDependencyUnavailable
	}
	var result apporg.Organization
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row store.OrganizationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ?", orgID).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apporg.ErrOrganizationNotFound
			}
			return fmt.Errorf("lock organization: %w", err)
		}
		before := organization(row)
		updates := map[string]any{"updated_at": now.UTC()}
		if request.Name != nil {
			row.Name, updates["name"] = *request.Name, *request.Name
		}
		if request.LegalName != nil {
			row.LegalName, updates["legal_name"] = stringPointer(*request.LegalName), *request.LegalName
		}
		if request.Address != nil {
			row.Address, updates["address"] = stringPointer(*request.Address), *request.Address
		}
		if request.ContactEmail != nil {
			row.ContactEmail, updates["contact_email"] = stringPointer(*request.ContactEmail), *request.ContactEmail
		}
		if request.Timezone != nil {
			row.Timezone, updates["timezone"] = stringPointer(*request.Timezone), *request.Timezone
		}
		if request.Enabled != nil {
			row.Enabled, updates["enabled"] = *request.Enabled, *request.Enabled
		}
		if err := tx.Model(&store.OrganizationModel{}).Where("org_id = ?", orgID).Updates(updates).Error; err != nil {
			return fmt.Errorf("update organization: %w", err)
		}
		updatedAt := now.UTC()
		row.UpdatedAt = &updatedAt
		if err := appendAudit(tx, orgID, actorID, "organization.changed", before, organization(row), now); err != nil {
			return err
		}
		result = organization(row)
		return nil
	})
	return result, err
}

// Disable sets enabled to false and appends its audit event atomically.
func (r *Repository) Disable(ctx context.Context, orgID, actorID uuid.UUID, now time.Time) error {
	if r == nil || r.db == nil {
		return apporg.ErrDependencyUnavailable
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row store.OrganizationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ?", orgID).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apporg.ErrOrganizationNotFound
			}
			return fmt.Errorf("lock organization for disable: %w", err)
		}
		before := organization(row)
		updatedAt := now.UTC()
		if err := tx.Model(&store.OrganizationModel{}).Where("org_id = ?", orgID).Updates(map[string]any{"enabled": false, "updated_at": updatedAt}).Error; err != nil {
			return fmt.Errorf("disable organization: %w", err)
		}
		row.Enabled, row.UpdatedAt = false, &updatedAt
		return appendAudit(tx, orgID, actorID, "organization.disabled", before, organization(row), now)
	})
}

func createOrganization(r *Repository, ctx context.Context, request apporg.CreateRequest, actorID uuid.UUID, now time.Time) (apporg.Organization, error) {
	var result apporg.Organization
	orgID, stationID := uuid.New(), uuid.New()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := store.OrganizationModel{OrgID: orgID, Name: request.Name, LegalName: stringPointer(request.LegalName), Address: stringPointer(request.Address), ContactEmail: stringPointer(request.ContactEmail), Timezone: stringPointer(request.Timezone), Enabled: true, CreatedAt: now.UTC()}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("create organization: %w", err)
		}
		if request.FirstStation == nil {
			return apporg.ErrNoStation
		}
		if err := tx.Create(&store.StationModel{OrgID: orgID, StationID: stationID, Name: request.FirstStation.Name, Timezone: request.FirstStation.Timezone, CreatedAt: now.UTC()}).Error; err != nil {
			return fmt.Errorf("create first station: %w", err)
		}
		if err := appendAudit(tx, orgID, actorID, "organization.created", nil, organization(row), now); err != nil {
			return err
		}
		result = organization(row)
		return nil
	})
	return result, err
}

func organization(row store.OrganizationModel) apporg.Organization {
	return apporg.Organization{OrgID: row.OrgID, Name: row.Name, LegalName: stringValue(row.LegalName), Address: stringValue(row.Address), ContactEmail: stringValue(row.ContactEmail), Timezone: stringValue(row.Timezone), Enabled: row.Enabled, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func stringPointer(value string) *string { return &value }
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func appendAudit(tx *gorm.DB, orgID, actorID uuid.UUID, eventType string, before, after any, now time.Time) error {
	payload, err := json.Marshal(map[string]any{"actor_user_id": actorID, "target_org_id": orgID, "before": before, "after": after})
	if err != nil {
		return fmt.Errorf("marshal organization audit event: %w", err)
	}
	if _, err := auditrepository.AppendInTransaction(tx, appaudit.AppendRequest{OrgID: orgID, EventID: uuid.New(), EventType: eventType, Payload: payload, Outcome: "success"}, now.UTC()); err != nil {
		return fmt.Errorf("append organization audit event: %w", err)
	}
	return nil
}
