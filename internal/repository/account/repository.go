// Package account persists account profiles, passwords, and reset tokens.
package account

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	auditrepository "github.com/pomkita/pomkita-be/internal/repository/audit"
	store "github.com/pomkita/pomkita-be/internal/repository/store"
	appaccount "github.com/pomkita/pomkita-be/internal/service/account"
	appaudit "github.com/pomkita/pomkita-be/internal/service/audit"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository persists account data with GORM.
type Repository struct{ db *gorm.DB }

// NewRepository creates an account repository.
func NewRepository(database *store.Store) *Repository {
	if database == nil {
		return &Repository{}
	}
	return &Repository{db: database.DB}
}

// ReadProfile reads the actor profile and its permitted station scope.
func (r *Repository) ReadProfile(ctx context.Context, userID uuid.UUID) (appaccount.Profile, error) {
	if r == nil || r.db == nil {
		return appaccount.Profile{}, appaccount.ErrDependencyUnavailable
	}
	return readProfile(r.db.WithContext(ctx), userID)
}

func readProfile(db *gorm.DB, userID uuid.UUID) (appaccount.Profile, error) {
	var row struct {
		UserID                       uuid.UUID
		Email, Username, DisplayName string
		OrgID                        uuid.UUID
		OrgName                      string
		Enabled                      bool
	}
	err := db.Table("users").Select("users.user_id, users.email, users.username, users.display_name, users.org_id, organizations.name as org_name, users.enabled").Joins("join organizations on organizations.org_id = users.org_id").Where("users.user_id = ?", userID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return appaccount.Profile{}, appaccount.ErrUserNotFound
	}
	if err != nil {
		return appaccount.Profile{}, fmt.Errorf("read account profile: %w", err)
	}
	var roles []string
	if err := db.Table("user_station_roles").Where("org_id = ? and user_id = ?", row.OrgID, row.UserID).Pluck("role", &roles).Error; err != nil {
		return appaccount.Profile{}, fmt.Errorf("read account roles: %w", err)
	}
	var stations []appaccount.Station
	if err := db.Table("user_station_roles").Select("stations.station_id, stations.name, stations.timezone").Joins("join stations on stations.org_id = user_station_roles.org_id and stations.station_id = user_station_roles.station_id").Where("user_station_roles.org_id = ? and user_station_roles.user_id = ?", row.OrgID, row.UserID).Group("stations.station_id, stations.name, stations.timezone").Find(&stations).Error; err != nil {
		return appaccount.Profile{}, fmt.Errorf("read account stations: %w", err)
	}
	roles = uniqueStrings(roles)
	if roles == nil {
		roles = []string{}
	}
	sort.Strings(roles)
	sort.Slice(stations, func(i, j int) bool { return stations[i].StationID.String() < stations[j].StationID.String() })
	if stations == nil {
		stations = []appaccount.Station{}
	}
	if !row.Enabled {
		return appaccount.Profile{}, appaccount.ErrDisabledAccount
	}
	return appaccount.Profile{UserID: row.UserID, Email: row.Email, Username: row.Username, DisplayName: row.DisplayName, Org: appaccount.Organization{ID: row.OrgID, Name: row.OrgName}, Roles: roles, Stations: stations}, nil
}

// UpdateProfile updates optional fields and appends the account audit event.
func (r *Repository) UpdateProfile(ctx context.Context, userID uuid.UUID, request appaccount.UpdateRequest, eventID uuid.UUID, now time.Time) (appaccount.Profile, error) {
	if r == nil || r.db == nil {
		return appaccount.Profile{}, appaccount.ErrDependencyUnavailable
	}
	var profile appaccount.Profile
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user store.UserModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&user).Error; err != nil {
			return mapUserError(err)
		}
		updates := map[string]any{}
		changed := make([]string, 0, 2)
		if request.DisplayName != nil && user.DisplayName != *request.DisplayName {
			updates["display_name"] = *request.DisplayName
			changed = append(changed, "display_name")
		}
		if request.Username != nil && user.Username != *request.Username {
			var count int64
			if err := tx.Model(&store.UserModel{}).Where("lower(username) = lower(?) and user_id <> ?", *request.Username, userID).Count(&count).Error; err != nil {
				return fmt.Errorf("check account username: %w", err)
			}
			if count != 0 {
				return appaccount.ErrUsernameConflict
			}
			updates["username"] = *request.Username
			changed = append(changed, "username")
		}
		if len(changed) > 0 {
			updates["updated_at"] = now.UTC()
			if err := tx.Model(&store.UserModel{}).Where("user_id = ?", userID).Updates(updates).Error; err != nil {
				return fmt.Errorf("update account profile: %w", err)
			}
			payload := fmt.Sprintf(`{"user_id":%q,"changed":[%q]}`, userID.String(), strings.Join(changed, `","`))
			if _, err := auditrepository.AppendInTransaction(tx, auditRequest(user.OrgID, eventID, "account.updated", []byte(payload)), now); err != nil {
				return fmt.Errorf("append account audit event: %w", err)
			}
		}
		var err error
		profile, err = readProfile(tx, userID)
		return err
	})
	return profile, err
}

// Credentials reads the password hash for one account.
func (r *Repository) Credentials(ctx context.Context, userID uuid.UUID) (appaccount.Credentials, error) {
	if r == nil || r.db == nil {
		return appaccount.Credentials{}, appaccount.ErrDependencyUnavailable
	}
	var user store.UserModel
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&user).Error; err != nil {
		return appaccount.Credentials{}, mapUserError(err)
	}
	return appaccount.Credentials{UserID: user.UserID, PasswordHash: user.PasswordHash, Enabled: user.Enabled}, nil
}

// ChangePassword stores a hash, revokes other sessions, and appends an audit event.
func (r *Repository) ChangePassword(ctx context.Context, userID uuid.UUID, passwordHash string, currentSessionID, eventID uuid.UUID, now time.Time) error {
	if r == nil || r.db == nil {
		return appaccount.ErrDependencyUnavailable
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user store.UserModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&user).Error; err != nil {
			return mapUserError(err)
		}
		if !user.Enabled {
			return appaccount.ErrDisabledAccount
		}
		if err := tx.Model(&store.UserModel{}).Where("user_id = ?", userID).Updates(map[string]any{"password_hash": passwordHash, "updated_at": now.UTC()}).Error; err != nil {
			return fmt.Errorf("store account password: %w", err)
		}
		if err := tx.Model(&store.SessionModel{}).Where("user_id = ? and jti <> ? and revoked_at is null", userID, currentSessionID).Update("revoked_at", now.UTC()).Error; err != nil {
			return fmt.Errorf("revoke account sessions: %w", err)
		}
		payload := fmt.Sprintf(`{"user_id":%q}`, userID.String())
		if _, err := auditrepository.AppendInTransaction(tx, auditRequest(user.OrgID, eventID, "account.password_changed", []byte(payload)), now); err != nil {
			return fmt.Errorf("append password audit event: %w", err)
		}
		return nil
	})
}

// FindResetTarget finds a user by username or email.
func (r *Repository) FindResetTarget(ctx context.Context, identifier string) (appaccount.ResetTarget, error) {
	if r == nil || r.db == nil {
		return appaccount.ResetTarget{}, appaccount.ErrDependencyUnavailable
	}
	var user store.UserModel
	identifier = strings.TrimSpace(identifier)
	if err := r.db.WithContext(ctx).Where("lower(username) = lower(?) or lower(email) = lower(?)", identifier, identifier).First(&user).Error; err != nil {
		return appaccount.ResetTarget{}, mapUserError(err)
	}
	return appaccount.ResetTarget{UserID: user.UserID, OrgID: user.OrgID, Email: user.Email, Enabled: user.Enabled}, nil
}

// IssuePasswordReset consumes the old open token and creates one new token.
func (r *Repository) IssuePasswordReset(ctx context.Context, userID, tokenID uuid.UUID, tokenHash []byte, now, expiry time.Time) error {
	if r == nil || r.db == nil {
		return appaccount.ErrDependencyUnavailable
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&store.AccountTokenModel{}).Where("user_id = ? and purpose = ? and consumed_at is null", userID, "password_reset").Update("consumed_at", now.UTC()).Error; err != nil {
			return fmt.Errorf("consume old password reset token: %w", err)
		}
		var user store.UserModel
		if err := tx.Where("user_id = ?", userID).First(&user).Error; err != nil {
			return mapUserError(err)
		}
		if err := tx.Create(&store.AccountTokenModel{TokenID: tokenID, OrgID: user.OrgID, UserID: userID, Purpose: "password_reset", TokenHash: append([]byte(nil), tokenHash...), ExpiresAt: expiry.UTC(), CreatedAt: now.UTC()}).Error; err != nil {
			return fmt.Errorf("create password reset token: %w", err)
		}
		return nil
	})
}

// ResetPassword stores the new hash, consumes the token, revokes sessions, and audits the change.
func (r *Repository) ResetPassword(ctx context.Context, tokenHash []byte, passwordHash string, eventID uuid.UUID, now time.Time) error {
	if r == nil || r.db == nil {
		return appaccount.ErrDependencyUnavailable
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var token store.AccountTokenModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("purpose = ? and token_hash = ?", "password_reset", tokenHash).First(&token).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return appaccount.ErrInvalidToken
			}
			return fmt.Errorf("find password reset token: %w", err)
		}
		if token.ConsumedAt != nil || !now.Before(token.ExpiresAt) {
			return appaccount.ErrInvalidToken
		}
		var user store.UserModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("org_id = ? and user_id = ?", token.OrgID, token.UserID).First(&user).Error; err != nil {
			return appaccount.ErrInvalidToken
		}
		if !user.Enabled {
			return appaccount.ErrInvalidToken
		}
		if err := tx.Model(&store.UserModel{}).Where("user_id = ?", user.UserID).Updates(map[string]any{"password_hash": passwordHash, "updated_at": now.UTC()}).Error; err != nil {
			return fmt.Errorf("store reset password: %w", err)
		}
		if err := tx.Model(&store.AccountTokenModel{}).Where("token_id = ?", token.TokenID).Update("consumed_at", now.UTC()).Error; err != nil {
			return fmt.Errorf("consume password reset token: %w", err)
		}
		if err := tx.Model(&store.SessionModel{}).Where("user_id = ? and revoked_at is null", user.UserID).Update("revoked_at", now.UTC()).Error; err != nil {
			return fmt.Errorf("revoke reset sessions: %w", err)
		}
		payload := fmt.Sprintf(`{"user_id":%q}`, user.UserID.String())
		if _, err := auditrepository.AppendInTransaction(tx, auditRequest(user.OrgID, eventID, "account.password_reset", []byte(payload)), now); err != nil {
			return fmt.Errorf("append reset audit event: %w", err)
		}
		return nil
	})
}

func mapUserError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return appaccount.ErrUserNotFound
	}
	return fmt.Errorf("read account user: %w", err)
}

func auditRequest(orgID, eventID uuid.UUID, eventType string, payload []byte) appaudit.AppendRequest {
	return appaudit.AppendRequest{OrgID: orgID, EventID: eventID, EventType: eventType, Payload: payload, Outcome: "success"}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
