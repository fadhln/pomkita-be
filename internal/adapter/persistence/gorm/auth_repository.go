package gormstore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	appauth "github.com/pomkita/pomkita-be/internal/service/auth"
	"gorm.io/gorm"
)

// AuthRepository persists users, signing keys, and sessions with GORM.
type AuthRepository struct {
	db      *gorm.DB
	secrets map[string]string
	legacy  bool
}

// NewAuthRepository creates an authentication repository. Secret values stay in process memory.
func NewAuthRepository(store *Store, secrets map[string]string) *AuthRepository {
	copyOfSecrets := make(map[string]string, len(secrets))
	for name, value := range secrets {
		copyOfSecrets[name] = value
	}
	if store == nil {
		return &AuthRepository{secrets: copyOfSecrets}
	}
	return &AuthRepository{db: store.db, secrets: copyOfSecrets}
}

// NewLegacyAuthRepository creates a GORM authentication repository for the current
// migration shape. It does not call the legacy authentication functions.
func NewLegacyAuthRepository(store *Store, secrets map[string]string) *AuthRepository {
	repository := NewAuthRepository(store, secrets)
	if repository != nil {
		repository.legacy = true
	}
	return repository
}

// FindUserByEmail finds an enabled or disabled user without exposing password data to callers.
func (r *AuthRepository) FindUserByEmail(ctx context.Context, email string) (appauth.User, error) {
	if r == nil || r.db == nil {
		return appauth.User{}, appauth.ErrDependencyUnavailable
	}
	var model UserModel
	err := r.db.WithContext(ctx).Where("lower(email) = lower(?)", strings.TrimSpace(email)).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return appauth.User{}, appauth.ErrUserNotFound
	}
	if err != nil {
		return appauth.User{}, fmt.Errorf("find user by email: %w", err)
	}
	return appauth.User{
		UserID: model.UserID, OrgID: model.OrgID, DisplayName: model.DisplayName,
		Email: model.Email, PasswordHash: model.PasswordHash, Enabled: model.Enabled,
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
	}
	var err error
	if r.legacy {
		var session legacySessionModel
		err = r.db.WithContext(ctx).Where("jti = ? and revoked_at is null", jti).Take(&session).Error
		if err == nil {
			err = r.db.WithContext(ctx).Table("users").Select("user_id, org_id, display_name").Where("user_id = ? and enabled = true", subject).Take(&identity).Error
		}
	} else {
		err = r.db.WithContext(ctx).Table("sessions").
			Select("users.user_id, users.org_id, users.display_name").
			Joins("join users on users.user_id = sessions.user_id").
			Where("sessions.jti = ? and sessions.user_id = ? and sessions.revoked_at is null and users.enabled = true", jti, subject).
			Take(&identity).Error
	}
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
	return appjwt.SessionView{UserID: identity.UserID, DisplayName: identity.DisplayName, Roles: roles, OrgID: identity.OrgID, StationIDs: stationIDs}, nil
}

// ActiveKey loads the one active signing key and resolves its secret reference.
func (r *AuthRepository) ActiveKey(ctx context.Context) (appjwt.Key, error) {
	if r == nil || r.db == nil {
		return appjwt.Key{}, appauth.ErrDependencyUnavailable
	}
	var model JWTKeyModel
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
	var model JWTKeyModel
	if err := r.db.WithContext(ctx).Where("kid = ?", kid).First(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return appjwt.Key{}, appjwt.ErrUnknownKID
		}
		return appjwt.Key{}, fmt.Errorf("load JWT key: %w", err)
	}
	return r.key(model)
}

func (r *AuthRepository) key(model JWTKeyModel) (appjwt.Key, error) {
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
	if r.legacy {
		model := legacySessionModel{JTI: session.JTI, KID: session.KID, IssuedAt: session.IssuedAt, ExpiresAt: session.ExpiresAt, LastActiveAt: session.LastActiveAt, RevokedAt: session.RevokedAt}
		if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
			return fmt.Errorf("create JWT session: %w", err)
		}
		return nil
	}
	model := SessionModel{JTI: session.JTI, UserID: session.UserID, KID: session.KID, IssuedAt: session.IssuedAt, ExpiresAt: session.ExpiresAt, LastActiveAt: session.LastActiveAt, RevokedAt: session.RevokedAt}
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
	if r.legacy {
		var model legacySessionModel
		if err := r.db.WithContext(ctx).Where("jti = ?", jti).First(&model).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return appjwt.Session{}, appjwt.ErrSessionNotFound
			}
			return appjwt.Session{}, fmt.Errorf("load JWT session: %w", err)
		}
		return r.touchLegacySession(ctx, model)
	}
	var model SessionModel
	if err := r.db.WithContext(ctx).Where("jti = ?", jti).First(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return appjwt.Session{}, appjwt.ErrSessionNotFound
		}
		return appjwt.Session{}, fmt.Errorf("load JWT session: %w", err)
	}
	if model.RevokedAt == nil {
		now := time.Now().UTC()
		if model.ExpiresAt.IsZero() || !now.After(model.ExpiresAt.Add(60*time.Second)) {
			if err := r.db.WithContext(ctx).Model(&SessionModel{}).Where("jti = ? and revoked_at is null", jti).Updates(map[string]any{"last_active_at": now}).Error; err != nil {
				return appjwt.Session{}, fmt.Errorf("touch JWT session: %w", err)
			}
			model.LastActiveAt = now
		}
	}
	return appjwt.Session{JTI: model.JTI, UserID: model.UserID, KID: model.KID, IssuedAt: model.IssuedAt, ExpiresAt: model.ExpiresAt, LastActiveAt: model.LastActiveAt, RevokedAt: model.RevokedAt}, nil
}

func (r *AuthRepository) touchLegacySession(ctx context.Context, model legacySessionModel) (appjwt.Session, error) {
	if model.RevokedAt == nil {
		now := time.Now().UTC()
		if model.ExpiresAt.IsZero() || !now.After(model.ExpiresAt.Add(60*time.Second)) {
			if err := r.db.WithContext(ctx).Model(&legacySessionModel{}).Where("jti = ? and revoked_at is null", model.JTI).Updates(map[string]any{"last_active_at": now}).Error; err != nil {
				return appjwt.Session{}, fmt.Errorf("touch JWT session: %w", err)
			}
			model.LastActiveAt = now
		}
	}
	return appjwt.Session{JTI: model.JTI, KID: model.KID, IssuedAt: model.IssuedAt, ExpiresAt: model.ExpiresAt, LastActiveAt: model.LastActiveAt, RevokedAt: model.RevokedAt}, nil
}

// RevokeSession marks a session as revoked.
func (r *AuthRepository) RevokeSession(ctx context.Context, jti uuid.UUID, at time.Time) error {
	if r == nil || r.db == nil {
		return appauth.ErrDependencyUnavailable
	}
	result := r.db.WithContext(ctx).Model(&SessionModel{}).Where("jti = ? and revoked_at is null", jti).Update("revoked_at", at)
	if result.Error != nil {
		return fmt.Errorf("revoke JWT session: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return appjwt.ErrSessionNotFound
	}
	return nil
}
