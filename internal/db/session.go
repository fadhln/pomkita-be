package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
)

// SessionManager combines database authentication with JWT session operations.
type SessionManager struct {
	db     *DB
	tokens *appjwt.Service
}

// NewSessionManager creates a database-backed session manager.
func NewSessionManager(database *DB, tokens *appjwt.Service) *SessionManager {
	return &SessionManager{db: database, tokens: tokens}
}

// Login verifies credentials through fn_login_user and issues a session JWT.
func (s *SessionManager) Login(ctx context.Context, email, password string) (string, appjwt.Claims, error) {
	var userID uuid.UUID
	if err := s.db.pool.QueryRow(ctx, `select user_id from public.fn_login_user($1, $2)`, email, password).Scan(&userID); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "28000" && pgErr.Message == "invalid_credentials" {
			return "", appjwt.Claims{}, ErrInvalidCredentials
		}
		return "", appjwt.Claims{}, fmt.Errorf("login user: %w", err)
	}
	return s.tokens.Issue(ctx, userID)
}

// Logout revokes the session through the existing JWT store path.
func (s *SessionManager) Logout(ctx context.Context, jti uuid.UUID) error {
	return s.tokens.Logout(ctx, jti)
}

// ReadSession verifies the token in a transaction and reads its procedure payload.
func (s *SessionManager) ReadSession(ctx context.Context, rawToken string) (appjwt.SessionView, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return appjwt.SessionView{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // commit is the successful path
	if err := s.db.SetRequestContext(ctx, tx, rawToken); err != nil {
		return appjwt.SessionView{}, err
	}
	var view appjwt.SessionView
	err = tx.QueryRow(ctx, `select user_id, display_name, roles, org_id, station_ids from public.fn_read_session()`).Scan(
		&view.UserID, &view.DisplayName, &view.Roles, &view.OrgID, &view.StationIDs,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return appjwt.SessionView{}, appjwt.ErrSessionNotFound
	}
	if err != nil {
		return appjwt.SessionView{}, fmt.Errorf("read session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return appjwt.SessionView{}, fmt.Errorf("commit session read: %w", err)
	}
	return view, nil
}
