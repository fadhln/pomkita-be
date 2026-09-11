package jwt

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// KeyStatus is the lifecycle state of a signing key.
type KeyStatus string

const (
	// KeyActive is the current signing key state.
	KeyActive KeyStatus = "active"
	// KeyPrevious is a verification-only key before its token expiry cutoff.
	KeyPrevious KeyStatus = "previous"
	// KeyRetired is a key that cannot verify new requests.
	KeyRetired KeyStatus = "retired"
)

// Key is a signing key record loaded from jwt_keys.
type Key struct {
	KID            string
	Secret         string
	Status         KeyStatus
	ActivatedAt    time.Time
	MaxTokenExpiry time.Time
}

// Session is a database session record for one issued token.
type Session struct {
	JTI       uuid.UUID
	KID       string
	IssuedAt  time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// Store provides key and session persistence to the JWT service.
type Store interface {
	ActiveKey(context.Context) (Key, error)
	Key(context.Context, string) (Key, error)
	CreateSession(context.Context, Session) error
	Session(context.Context, uuid.UUID) (Session, error)
	RevokeSession(context.Context, uuid.UUID, time.Time) error
}

// Config defines token validation and clock settings.
type Config struct {
	Issuer   string
	Audience string
	Now      func() time.Time
}

// Claims are the verified application claims.
type Claims struct {
	Issuer    string
	Audience  string
	Subject   uuid.UUID
	JTI       uuid.UUID
	IssuedAt  time.Time
	ExpiresAt time.Time
	KID       string
}

// Service issues, verifies, and revokes session JWTs.
type Service struct {
	store    Store
	issuer   string
	audience string
	now      func() time.Time
}

var (
	// ErrInvalidAlgorithm means that the JWT does not use HS256.
	ErrInvalidAlgorithm = errors.New("jwt_invalid_algorithm")
	// ErrInvalidIssuer means that iss is not the configured issuer.
	ErrInvalidIssuer = errors.New("jwt_invalid_issuer")
	// ErrInvalidAudience means that aud is not the configured audience.
	ErrInvalidAudience = errors.New("jwt_invalid_audience")
	// ErrExpired means that exp is outside the allowed clock skew.
	ErrExpired = errors.New("jwt_expired")
	// ErrFutureIssuedAt means that iat is in the future.
	ErrFutureIssuedAt = errors.New("jwt_future_issued_at")
	// ErrRevokedJTI means that the session was revoked.
	ErrRevokedJTI = errors.New("jwt_revoked_jti")
	// ErrUnknownKID means that the token key ID is not known.
	ErrUnknownKID = errors.New("jwt_unknown_kid")
	// ErrRetiredKey means that a previous key passed its expiry cutoff.
	ErrRetiredKey = errors.New("jwt_retired_key")
	// ErrInvalidSignature means that the JWT signature is not valid.
	ErrInvalidSignature = errors.New("jwt_invalid_signature")
	// ErrSessionNotFound means that no session exists for jti.
	ErrSessionNotFound = errors.New("jwt_session_not_found")
	// ErrSessionExpired means that the database session expired.
	ErrSessionExpired = errors.New("jwt_session_expired")
)

// NewService creates a JWT service with a deterministic clock when provided.
func NewService(store Store, config Config) *Service {
	issuer := config.Issuer
	if issuer == "" {
		issuer = "pomkita"
	}
	audience := config.Audience
	if audience == "" {
		audience = "pomkita"
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, issuer: issuer, audience: audience, now: now}
}

type wireClaims struct {
	jwtv5.RegisteredClaims
}

// Issue creates and persists a fifteen-minute session JWT.
func (s *Service) Issue(ctx context.Context, subject uuid.UUID) (string, Claims, error) {
	key, err := s.store.ActiveKey(ctx)
	if err != nil {
		return "", Claims{}, fmt.Errorf("load active JWT key: %w", err)
	}
	if key.Status != KeyActive {
		return "", Claims{}, ErrUnknownKID
	}
	now := s.now().UTC().Truncate(time.Second)
	expires := now.Add(15 * time.Minute)
	if !key.MaxTokenExpiry.IsZero() && key.MaxTokenExpiry.Before(expires) {
		expires = key.MaxTokenExpiry.UTC().Truncate(time.Second)
	}
	if !expires.After(now) {
		return "", Claims{}, ErrRetiredKey
	}
	jti := uuid.New()
	claims := Claims{
		Issuer: s.issuer, Audience: s.audience, Subject: subject, JTI: jti,
		IssuedAt: now, ExpiresAt: expires, KID: key.KID,
	}
	wire := wireClaims{RegisteredClaims: jwtv5.RegisteredClaims{
		Issuer:    s.issuer,
		Subject:   subject.String(),
		ID:        jti.String(),
		IssuedAt:  jwtv5.NewNumericDate(now),
		ExpiresAt: jwtv5.NewNumericDate(expires),
		Audience:  jwtv5.ClaimStrings{s.audience},
	}}
	token := jwtv5.NewWithClaims(jwtv5.SigningMethodHS256, wire)
	token.Header["kid"] = key.KID
	signed, err := token.SignedString([]byte(key.Secret))
	if err != nil {
		return "", Claims{}, fmt.Errorf("sign JWT: %w", err)
	}
	if err := s.store.CreateSession(ctx, Session{JTI: jti, KID: key.KID, IssuedAt: now, ExpiresAt: expires}); err != nil {
		return "", Claims{}, fmt.Errorf("create JWT session: %w", err)
	}
	return signed, claims, nil
}

// Verify validates the signature, claims, signing-key lifecycle, and session.
func (s *Service) Verify(ctx context.Context, raw string) (Claims, error) {
	parser := jwtv5.NewParser(jwtv5.WithoutClaimsValidation())
	token, _, err := parser.ParseUnverified(raw, &wireClaims{})
	if err != nil {
		return Claims{}, fmt.Errorf("parse JWT: %w", err)
	}
	if token.Method == nil || token.Method.Alg() != jwtv5.SigningMethodHS256.Alg() {
		return Claims{}, ErrInvalidAlgorithm
	}
	wire, ok := token.Claims.(*wireClaims)
	if !ok {
		return Claims{}, ErrInvalidSignature
	}
	if wire.Issuer != s.issuer {
		return Claims{}, ErrInvalidIssuer
	}
	if len(wire.Audience) != 1 || wire.Audience[0] != s.audience {
		return Claims{}, ErrInvalidAudience
	}
	if wire.IssuedAt == nil || wire.ExpiresAt == nil {
		return Claims{}, ErrExpired
	}
	issuedAt := wire.IssuedAt.Time.UTC()
	expiresAt := wire.ExpiresAt.Time.UTC()
	now := s.now().UTC()
	if now.After(expiresAt.Add(60 * time.Second)) {
		return Claims{}, ErrExpired
	}
	if issuedAt.After(now) {
		return Claims{}, ErrFutureIssuedAt
	}
	subject, err := uuid.Parse(wire.Subject)
	if err != nil {
		return Claims{}, fmt.Errorf("invalid subject: %w", err)
	}
	jti, err := uuid.Parse(wire.ID)
	if err != nil {
		return Claims{}, fmt.Errorf("invalid jti: %w", err)
	}
	kid, ok := token.Header["kid"].(string)
	if !ok || kid == "" {
		return Claims{}, ErrUnknownKID
	}
	key, err := s.store.Key(ctx, kid)
	if err != nil {
		if errors.Is(err, ErrUnknownKID) {
			return Claims{}, ErrUnknownKID
		}
		return Claims{}, fmt.Errorf("load JWT key: %w", err)
	}
	if key.Status == KeyRetired || (key.Status == KeyPrevious && !now.Before(key.MaxTokenExpiry)) {
		return Claims{}, ErrRetiredKey
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || !verifyHMAC(token.Method, parts[0]+"."+parts[1], raw, key.Secret) {
		return Claims{}, ErrInvalidSignature
	}
	session, err := s.store.Session(ctx, jti)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return Claims{}, ErrSessionNotFound
		}
		return Claims{}, fmt.Errorf("load JWT session: %w", err)
	}
	if session.RevokedAt != nil {
		return Claims{}, ErrRevokedJTI
	}
	if session.KID != kid {
		return Claims{}, ErrSessionNotFound
	}
	if now.After(session.ExpiresAt.Add(60 * time.Second)) {
		return Claims{}, ErrSessionExpired
	}
	return Claims{Issuer: wire.Issuer, Audience: wire.Audience[0], Subject: subject, JTI: jti, IssuedAt: issuedAt, ExpiresAt: expiresAt, KID: kid}, nil
}

func verifyHMAC(method jwtv5.SigningMethod, signingInput, raw, secret string) bool {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return false
	}
	if method.Alg() != jwtv5.SigningMethodHS256.Alg() {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	return method.Verify(signingInput, signature, []byte(secret)) == nil
}

// Logout revokes a session in the database.
func (s *Service) Logout(ctx context.Context, jti uuid.UUID) error {
	if err := s.store.RevokeSession(ctx, jti, s.now().UTC()); err != nil {
		return fmt.Errorf("revoke JWT session: %w", err)
	}
	return nil
}
