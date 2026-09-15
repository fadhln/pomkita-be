// Package identity persists user invitations and account activation.
package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	auditrepository "github.com/pomkita/pomkita-be/internal/repository/audit"
	store "github.com/pomkita/pomkita-be/internal/repository/store"
	appaudit "github.com/pomkita/pomkita-be/internal/service/audit"
	appidentity "github.com/pomkita/pomkita-be/internal/service/identity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IdentityRepository persists identity changes.
type IdentityRepository struct {
	db *gorm.DB
}

// NewIdentityRepository creates an identity repository.
func NewIdentityRepository(database *store.Store) *IdentityRepository {
	if database == nil {
		return &IdentityRepository{}
	}
	return &IdentityRepository{db: database.DB}
}

// CreateInvitation creates the user, role, token, and audit event atomically.
func (r *IdentityRepository) CreateInvitation(ctx context.Context, request appidentity.InvitationRequest, eventID uuid.UUID, tokenHash []byte, now time.Time) (uuid.UUID, error) {
	if r == nil || r.db == nil {
		return uuid.Nil, appidentity.ErrDependencyUnavailable
	}
	userID := uuid.New()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var organization store.OrganizationModel
		if err := tx.Where("org_id = ?", request.TargetOrgID).First(&organization).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return appidentity.ErrWrongOrganization
			}
			return fmt.Errorf("find invitation organization: %w", err)
		}
		var stationCount int64
		if err := tx.Model(&store.StationModel{}).Where("org_id = ?", request.TargetOrgID).Count(&stationCount).Error; err != nil {
			return fmt.Errorf("count invitation stations: %w", err)
		}
		if stationCount == 0 {
			return appidentity.ErrNoStation
		}
		var station store.StationModel
		if err := tx.Where("org_id = ? and station_id = ?", request.TargetOrgID, request.StationID).First(&station).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return appidentity.ErrWrongOrganization
			}
			return fmt.Errorf("find invitation station: %w", err)
		}
		var emailCount int64
		if err := tx.Model(&store.UserModel{}).Where("lower(email) = lower(?)", strings.TrimSpace(request.Email)).Count(&emailCount).Error; err != nil {
			return fmt.Errorf("check invitation email: %w", err)
		}
		if emailCount != 0 {
			return appidentity.ErrEmailConflict
		}
		createdAt := now.UTC()
		user := map[string]any{
			"user_id": userID, "org_id": request.TargetOrgID, "email": strings.TrimSpace(request.Email),
			"display_name": strings.TrimSpace(request.DisplayName), "enabled": false,
			"invited_at": createdAt, "updated_at": createdAt, "password_hash": nil, "username": nil,
			"created_at": createdAt,
		}
		if err := tx.Table("users").Create(user).Error; err != nil {
			return fmt.Errorf("create invited user: %w", err)
		}
		if err := tx.Table("user_station_roles").Create(map[string]any{
			"org_id": request.TargetOrgID, "station_id": request.StationID, "user_id": userID, "role": request.Role,
		}).Error; err != nil {
			return fmt.Errorf("create invited user role: %w", err)
		}
		if err := tx.Model(&store.AccountTokenModel{}).Where("user_id = ? and purpose = ? and consumed_at is null", userID, "invitation").Update("consumed_at", createdAt).Error; err != nil {
			return fmt.Errorf("consume previous invitation token: %w", err)
		}
		createdBy := request.ActorID
		token := store.AccountTokenModel{TokenID: eventID, OrgID: request.TargetOrgID, UserID: userID, Purpose: "invitation", TokenHash: append([]byte(nil), tokenHash...), ExpiresAt: createdAt.Add(7 * 24 * time.Hour), CreatedBy: &createdBy, CreatedAt: createdAt}
		if err := tx.Create(&token).Error; err != nil {
			return fmt.Errorf("create invitation token: %w", err)
		}
		payload := []byte(fmt.Sprintf(`{"actor_user_id":%q,"user_id":%q,"station_id":%q,"role":%q}`, request.ActorID.String(), userID.String(), station.StationID.String(), request.Role))
		if _, err := auditrepository.AppendInTransaction(tx, appauditRequest(request.TargetOrgID, uuid.New(), "user.created", payload), createdAt); err != nil {
			return fmt.Errorf("append user creation audit event: %w", err)
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

// AcceptInvitation activates a user and consumes the matching token atomically.
func (r *IdentityRepository) AcceptInvitation(ctx context.Context, request appidentity.AcceptanceRequest, passwordHash string, tokenHash []byte, now time.Time) error {
	if r == nil || r.db == nil {
		return appidentity.ErrDependencyUnavailable
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var token store.AccountTokenModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("purpose = ? and token_hash = ?", "invitation", tokenHash).First(&token).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return appidentity.ErrInvalidToken
			}
			return fmt.Errorf("find invitation token: %w", err)
		}
		if token.ConsumedAt != nil || !now.Before(token.ExpiresAt) {
			return appidentity.ErrInvalidToken
		}
		var usernameCount int64
		if err := tx.Model(&store.UserModel{}).Where("lower(username) = lower(?) and user_id <> ?", request.Username, token.UserID).Count(&usernameCount).Error; err != nil {
			return fmt.Errorf("check invitation username: %w", err)
		}
		if usernameCount != 0 {
			return appidentity.ErrUsernameConflict
		}
		var user store.UserModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and user_id = ?", token.OrgID, token.UserID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return appidentity.ErrInvalidToken
			}
			return fmt.Errorf("find invited user: %w", err)
		}
		if user.Enabled {
			return appidentity.ErrInvalidToken
		}
		activatedAt := now.UTC()
		if err := tx.Model(&store.UserModel{}).Where("org_id = ? and user_id = ?", token.OrgID, token.UserID).Updates(map[string]any{
			"username": request.Username, "password_hash": passwordHash, "display_name": request.DisplayName,
			"enabled": true, "activated_at": activatedAt, "updated_at": activatedAt,
		}).Error; err != nil {
			return fmt.Errorf("activate invited user: %w", err)
		}
		if err := tx.Model(&store.AccountTokenModel{}).Where("token_id = ?", token.TokenID).Update("consumed_at", activatedAt).Error; err != nil {
			return fmt.Errorf("consume invitation token: %w", err)
		}
		payload := []byte(fmt.Sprintf(`{"user_id":%q,"username":%q}`, token.UserID.String(), request.Username))
		if _, err := auditrepository.AppendInTransaction(tx, appauditRequest(token.OrgID, uuid.New(), "invitation.accepted", payload), activatedAt); err != nil {
			return fmt.Errorf("append invitation acceptance audit event: %w", err)
		}
		return nil
	})
}

func appauditRequest(orgID, eventID uuid.UUID, eventType string, payload []byte) appaudit.AppendRequest {
	return appaudit.AppendRequest{OrgID: orgID, EventID: eventID, EventType: eventType, Payload: payload, Outcome: "success"}
}
