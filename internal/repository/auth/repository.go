package auth

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	appjwt "github.com/fadhln/pomkita-be/internal/jwt"
	"github.com/fadhln/pomkita-be/internal/repository/store"
	appauth "github.com/fadhln/pomkita-be/internal/service/auth"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AuthRepository persists users, signing keys, and sessions with GORM.
type AuthRepository struct {
	db      *gorm.DB
	secrets map[string]string
}

// NewAuthRepository creates an authentication repository. Secret values stay in process memory.
func NewAuthRepository(store *store.Store, secrets map[string]string) *AuthRepository {
	copyOfSecrets := make(map[string]string, len(secrets))
	for name, value := range secrets {
		copyOfSecrets[name] = value
	}
	if store == nil {
		return &AuthRepository{secrets: copyOfSecrets}
	}
	return &AuthRepository{db: store.DB, secrets: copyOfSecrets}
}

// FindUserByEmail finds an enabled or disabled user without exposing password data to callers.
func (r *AuthRepository) FindUserByEmail(ctx context.Context, email string) (appauth.User, error) {
	if r == nil || r.db == nil {
		return appauth.User{}, appauth.ErrDependencyUnavailable
	}
	var model store.UserModel
	err := r.db.WithContext(ctx).Where("lower(email) = lower(?)", strings.TrimSpace(email)).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return appauth.User{}, appauth.ErrUserNotFound
	}
	if err != nil {
		return appauth.User{}, fmt.Errorf("find user by email: %w", err)
	}
	return appauth.User{
		UserID: model.UserID, OrgID: model.OrgID, DisplayName: model.DisplayName,
		Email: model.Email, Username: model.Username, PasswordHash: model.PasswordHash, Enabled: model.Enabled,
	}, nil
}

// FindUserByUsername finds an enabled or disabled user by case-insensitive username.
func (r *AuthRepository) FindUserByUsername(ctx context.Context, username string) (appauth.User, error) {
	if r == nil || r.db == nil {
		return appauth.User{}, appauth.ErrDependencyUnavailable
	}
	var model store.UserModel
	err := r.db.WithContext(ctx).Where("lower(username) = lower(?)", strings.TrimSpace(username)).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return appauth.User{}, appauth.ErrUserNotFound
	}
	if err != nil {
		return appauth.User{}, fmt.Errorf("find user by username: %w", err)
	}
	return appauth.User{
		UserID: model.UserID, OrgID: model.OrgID, DisplayName: model.DisplayName,
		Email: model.Email, Username: model.Username, PasswordHash: model.PasswordHash, Enabled: model.Enabled,
	}, nil
}

// ReadSession reads the user identity and station roles for a session.
func (r *AuthRepository) ReadSession(ctx context.Context, jti, subject uuid.UUID) (appjwt.SessionView, error) {
	if r == nil || r.db == nil {
		return appjwt.SessionView{}, appauth.ErrDependencyUnavailable
	}
	var identity struct {
		UserID      uuid.UUID
		OrgID       uuid.UUID
		DisplayName string
		Username    string
	}
	err := r.db.WithContext(ctx).Table("sessions").
		Select("users.user_id, users.org_id, users.display_name, users.username").
		Joins("join users on users.user_id = sessions.user_id").
		Where("sessions.jti = ? and sessions.user_id = ? and sessions.revoked_at is null and users.enabled = true", jti, subject).
		Take(&identity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return appjwt.SessionView{}, appjwt.ErrSessionNotFound
	}
	if err != nil {
		return appjwt.SessionView{}, fmt.Errorf("read session identity: %w", err)
	}

	type roleRow struct {
		Role      string
		StationID uuid.UUID
	}
	var rows []roleRow
	if err := r.db.WithContext(ctx).Table("user_station_roles").
		Select("role, station_id").
		Where("org_id = ? and user_id = ?", identity.OrgID, identity.UserID).
		Find(&rows).Error; err != nil {
		return appjwt.SessionView{}, fmt.Errorf("read session roles: %w", err)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].StationID == rows[j].StationID {
			return rows[i].Role < rows[j].Role
		}
		return rows[i].StationID.String() < rows[j].StationID.String()
	})
	roles := make([]string, 0, len(rows))
	stationIDs := make([]uuid.UUID, 0, len(rows))
	seenRoles := make(map[string]struct{})
	seenStations := make(map[uuid.UUID]struct{})
	for _, row := range rows {
		if _, ok := seenRoles[row.Role]; !ok {
			roles = append(roles, row.Role)
			seenRoles[row.Role] = struct{}{}
		}
		if _, ok := seenStations[row.StationID]; !ok {
			stationIDs = append(stationIDs, row.StationID)
			seenStations[row.StationID] = struct{}{}
		}
	}
	activeContext, err := r.ReadActiveContext(ctx, jti)
	if err != nil {
		return appjwt.SessionView{}, fmt.Errorf("read active session context: %w", err)
	}
	return appjwt.SessionView{UserID: identity.UserID, Username: identity.Username, DisplayName: identity.DisplayName, Roles: roles, OrgID: identity.OrgID, StationIDs: stationIDs, ActiveContext: activeContext}, nil
}

// ReadActiveContext reads the organization and station selected for one session.
func (r *AuthRepository) ReadActiveContext(ctx context.Context, jti uuid.UUID) (*appjwt.ActiveContext, error) {
	if r == nil || r.db == nil {
		return nil, appauth.ErrDependencyUnavailable
	}
	var row struct {
		OrgID     uuid.UUID
		StationID uuid.UUID
	}
	err := r.db.WithContext(ctx).Table("session_active_context").Select("org_id, station_id").Where("jti = ?", jti).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read active context: %w", err)
	}
	return &appjwt.ActiveContext{OrgID: row.OrgID, StationID: row.StationID}, nil
}

// SetActiveContext replaces the context stored for one session.
func (r *AuthRepository) SetActiveContext(ctx context.Context, jti, orgID, stationID uuid.UUID, updatedAt time.Time) error {
	if r == nil || r.db == nil {
		return appauth.ErrDependencyUnavailable
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`insert into session_active_context (jti, org_id, station_id, updated_at) values (?, ?, ?, ?) on conflict (jti) do update set org_id = excluded.org_id, station_id = excluded.station_id, updated_at = excluded.updated_at`, jti, orgID, stationID, updatedAt.UTC()).Error; err != nil {
			return fmt.Errorf("store session active context: %w", err)
		}
		result := tx.Exec(`update users set preferred_org_id = ?, preferred_station_id = ? where user_id = (select user_id from sessions where jti = ?)`, orgID, stationID, jti)
		if result.Error != nil {
			return fmt.Errorf("store account context preference: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return errors.New("session user was not found for context preference")
		}
		return nil
	})
}

// ReadContextPreference reads the organization and station saved for one user.
func (r *AuthRepository) ReadContextPreference(ctx context.Context, userID uuid.UUID) (*appjwt.ActiveContext, error) {
	if r == nil || r.db == nil {
		return nil, appauth.ErrDependencyUnavailable
	}
	var preference struct {
		OrgID     *uuid.UUID `gorm:"column:preferred_org_id"`
		StationID *uuid.UUID `gorm:"column:preferred_station_id"`
	}
	if err := r.db.WithContext(ctx).Table("users").
		Select("preferred_org_id, preferred_station_id").
		Where("user_id = ?", userID).Take(&preference).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, appauth.ErrUserNotFound
		}
		return nil, fmt.Errorf("read account context preference: %w", err)
	}
	if preference.OrgID == nil || preference.StationID == nil {
		return nil, nil
	}
	return &appjwt.ActiveContext{OrgID: *preference.OrgID, StationID: *preference.StationID}, nil
}

// OrganizationExists reports whether an organization exists.
func (r *AuthRepository) OrganizationExists(ctx context.Context, orgID uuid.UUID) (bool, error) {
	if r == nil || r.db == nil {
		return false, appauth.ErrDependencyUnavailable
	}
	var count int64
	err := r.db.WithContext(ctx).Table("organizations").Where("org_id = ?", orgID).Count(&count).Error
	return count == 1, err
}

// StationInOrganization reports whether a station belongs to an organization.
func (r *AuthRepository) StationInOrganization(ctx context.Context, orgID, stationID uuid.UUID) (bool, error) {
	if r == nil || r.db == nil {
		return false, appauth.ErrDependencyUnavailable
	}
	var count int64
	err := r.db.WithContext(ctx).Table("stations").Where("org_id = ? and station_id = ?", orgID, stationID).Count(&count).Error
	return count == 1, err
}

// ActiveKey loads the one active signing key and resolves its secret reference.
func (r *AuthRepository) ActiveKey(ctx context.Context) (appjwt.Key, error) {
	if r == nil || r.db == nil {
		return appjwt.Key{}, appauth.ErrDependencyUnavailable
	}
	var model store.JWTKeyModel
	if err := r.db.WithContext(ctx).Where("status = ?", string(appjwt.KeyActive)).First(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return appjwt.Key{}, appjwt.ErrUnknownKID
		}
		return appjwt.Key{}, fmt.Errorf("load active JWT key: %w", err)
	}
	return r.key(model)
}

// Key loads a signing key by its key identifier.
func (r *AuthRepository) Key(ctx context.Context, kid string) (appjwt.Key, error) {
	if r == nil || r.db == nil {
		return appjwt.Key{}, appauth.ErrDependencyUnavailable
	}
	var model store.JWTKeyModel
	if err := r.db.WithContext(ctx).Where("kid = ?", kid).First(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return appjwt.Key{}, appjwt.ErrUnknownKID
		}
		return appjwt.Key{}, fmt.Errorf("load JWT key: %w", err)
	}
	return r.key(model)
}

func (r *AuthRepository) key(model store.JWTKeyModel) (appjwt.Key, error) {
	secret := r.secrets[model.SecretRef]
	if secret == "" {
		return appjwt.Key{}, appjwt.ErrInvalidSignature
	}
	return appjwt.Key{KID: model.KID, Secret: secret, Status: appjwt.KeyStatus(model.Status), ActivatedAt: model.ActivatedAt, MaxTokenExpiry: model.MaxTokenExpiry}, nil
}

// CreateSession persists a newly issued session.
func (r *AuthRepository) CreateSession(ctx context.Context, session appjwt.Session) error {
	if r == nil || r.db == nil {
		return appauth.ErrDependencyUnavailable
	}
	model := store.SessionModel{JTI: session.JTI, UserID: session.UserID, KID: session.KID, IssuedAt: session.IssuedAt, ExpiresAt: session.ExpiresAt, LastActiveAt: session.LastActiveAt, RevokedAt: session.RevokedAt}
	if model.UserID == uuid.Nil {
		return errors.New("session user ID is required")
	}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return fmt.Errorf("create JWT session: %w", err)
	}
	return nil
}

// Session loads and touches one active session.
func (r *AuthRepository) Session(ctx context.Context, jti uuid.UUID) (appjwt.Session, error) {
	if r == nil || r.db == nil {
		return appjwt.Session{}, appauth.ErrDependencyUnavailable
	}
	var model store.SessionModel
	if err := r.db.WithContext(ctx).Where("jti = ?", jti).First(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return appjwt.Session{}, appjwt.ErrSessionNotFound
		}
		return appjwt.Session{}, fmt.Errorf("load JWT session: %w", err)
	}
	if model.RevokedAt == nil {
		now := time.Now().UTC()
		if model.ExpiresAt.IsZero() || !now.After(model.ExpiresAt.Add(60*time.Second)) {
			if err := r.db.WithContext(ctx).Model(&store.SessionModel{}).Where("jti = ? and revoked_at is null", jti).Updates(map[string]any{"last_active_at": now}).Error; err != nil {
				return appjwt.Session{}, fmt.Errorf("touch JWT session: %w", err)
			}
			model.LastActiveAt = now
		}
	}
	return appjwt.Session{JTI: model.JTI, UserID: model.UserID, KID: model.KID, IssuedAt: model.IssuedAt, ExpiresAt: model.ExpiresAt, LastActiveAt: model.LastActiveAt, RevokedAt: model.RevokedAt}, nil
}

// RevokeSession marks a session as revoked.
func (r *AuthRepository) RevokeSession(ctx context.Context, jti uuid.UUID, at time.Time) error {
	if r == nil || r.db == nil {
		return appauth.ErrDependencyUnavailable
	}
	result := r.db.WithContext(ctx).Model(&store.SessionModel{}).Where("jti = ? and revoked_at is null", jti).Update("revoked_at", at)
	if result.Error != nil {
		return fmt.Errorf("revoke JWT session: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return appjwt.ErrSessionNotFound
	}
	return nil
}
