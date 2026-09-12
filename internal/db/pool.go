package db

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
)

// DB owns the PostgreSQL connection pool used by the service.
type DB struct {
	pool       *pgxpool.Pool
	secretMu   sync.RWMutex
	jwtSecrets map[string]string
}

// New opens a PostgreSQL pool and verifies that the database is reachable.
func New(ctx context.Context, databaseURL string) (*DB, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("open database pool: %w", err)
	}
	database := &DB{pool: pool, jwtSecrets: make(map[string]string)}
	if err := database.Ping(ctx); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

// Close releases all pool connections.
func (d *DB) Close() {
	if d != nil && d.pool != nil {
		d.pool.Close()
	}
}

// Ping checks database reachability.
func (d *DB) Ping(ctx context.Context) error {
	if d == nil || d.pool == nil {
		return errors.New("database pool is not configured")
	}
	if err := d.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

// MigrationsCurrent reports whether the database has the latest clean version.
func (d *DB) MigrationsCurrent(ctx context.Context, latest int) (bool, error) {
	var version int
	var dirty bool
	err := d.pool.QueryRow(ctx, `select version, dirty from schema_migrations limit 1`).Scan(&version, &dirty)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return false, nil
		}
		return false, fmt.Errorf("read migration state: %w", err)
	}
	return version == latest && !dirty, nil
}

// SetRequestContext calls the database JWT verification function in a transaction.
func (d *DB) SetRequestContext(ctx context.Context, tx pgx.Tx, rawToken string) error {
	d.secretMu.RLock()
	secretNames := make([]string, 0, len(d.jwtSecrets))
	for name := range d.jwtSecrets {
		secretNames = append(secretNames, name)
	}
	sort.Strings(secretNames)
	for _, name := range secretNames {
		if _, err := tx.Exec(ctx, `select set_config($1, $2, true)`, name, d.jwtSecrets[name]); err != nil {
			d.secretMu.RUnlock()
			return fmt.Errorf("set JWT verification key: %w", err)
		}
	}
	d.secretMu.RUnlock()
	if _, err := tx.Exec(ctx, `select public.fn_set_request_context($1)`, rawToken); err != nil {
		return fmt.Errorf("set request context: %w", err)
	}
	return nil
}

// SetJWTSecrets sets the process-held values referenced by jwt_keys.secret_ref.
func (d *DB) SetJWTSecrets(secrets map[string]string) {
	d.secretMu.Lock()
	defer d.secretMu.Unlock()
	d.jwtSecrets = make(map[string]string, len(secrets))
	for name, value := range secrets {
		d.jwtSecrets[name] = value
	}
}

// Begin starts a database transaction for a procedure call.
func (d *DB) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin database transaction: %w", err)
	}
	return tx, nil
}

// JWTStore is a PostgreSQL-backed JWT key and session store.
type JWTStore struct {
	db      *DB
	secrets map[string]string
	mu      sync.RWMutex
}

// JWTStore creates a key and session store. Secret values stay in process memory.
func (d *DB) JWTStore(secrets map[string]string) *JWTStore {
	copyOfSecrets := make(map[string]string, len(secrets))
	for name, value := range secrets {
		copyOfSecrets[name] = value
	}
	return &JWTStore{db: d, secrets: copyOfSecrets}
}

// ActiveKey loads the active signing key.
func (s *JWTStore) ActiveKey(ctx context.Context) (appjwt.Key, error) {
	return s.loadKey(ctx, `select kid, secret_ref, status, activated_at, max_token_expiry from public.fn_read_active_jwt_key()`, nil)
}

// Key loads a key by key ID.
func (s *JWTStore) Key(ctx context.Context, kid string) (appjwt.Key, error) {
	return s.loadKey(ctx, `select kid, secret_ref, status, activated_at, max_token_expiry from public.fn_read_jwt_key($1)`, []any{kid})
}

func (s *JWTStore) loadKey(ctx context.Context, clause string, args []any) (appjwt.Key, error) {
	var key appjwt.Key
	var secretRef string
	var status string
	var activatedAt, maxTokenExpiry time.Time
	query := clause
	if err := s.db.pool.QueryRow(ctx, query, args...).Scan(&key.KID, &secretRef, &status, &activatedAt, &maxTokenExpiry); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return appjwt.Key{}, appjwt.ErrUnknownKID
		}
		return appjwt.Key{}, fmt.Errorf("load JWT key: %w", err)
	}
	s.mu.RLock()
	secret, ok := s.secrets[secretRef]
	s.mu.RUnlock()
	if !ok || secret == "" {
		return appjwt.Key{}, appjwt.ErrInvalidSignature
	}
	key.Secret = secret
	key.Status = appjwt.KeyStatus(status)
	key.ActivatedAt = activatedAt
	key.MaxTokenExpiry = maxTokenExpiry
	return key, nil
}

// CreateSession stores a newly issued token session.
func (s *JWTStore) CreateSession(ctx context.Context, session appjwt.Session) error {
	_, err := s.db.pool.Exec(ctx, `select public.fn_create_session($1, $2, $3, $4, $5)`, session.JTI, session.KID, session.IssuedAt, session.ExpiresAt, session.LastActiveAt)
	if err != nil {
		return fmt.Errorf("insert JWT session: %w", err)
	}
	return nil
}

// Session loads one token session.
func (s *JWTStore) Session(ctx context.Context, jti uuid.UUID) (appjwt.Session, error) {
	var session appjwt.Session
	var revokedAt *time.Time
	err := s.db.pool.QueryRow(ctx, `select jti, kid, issued_at, expires_at, last_active_at, revoked_at from public.fn_read_session_record($1)`, jti).Scan(&session.JTI, &session.KID, &session.IssuedAt, &session.ExpiresAt, &session.LastActiveAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return appjwt.Session{}, appjwt.ErrSessionNotFound
	}
	if err != nil {
		return appjwt.Session{}, fmt.Errorf("load JWT session: %w", err)
	}
	session.RevokedAt = revokedAt
	return session, nil
}

// RevokeSession marks one token session as revoked.
func (s *JWTStore) RevokeSession(ctx context.Context, jti uuid.UUID, at time.Time) error {
	_, err := s.db.pool.Exec(ctx, `select public.fn_revoke_session($1, $2)`, jti, at)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "28000" && pgErr.Message == "invalid_session" {
			return appjwt.ErrSessionNotFound
		}
		return fmt.Errorf("revoke JWT session: %w", err)
	}
	return nil
}
