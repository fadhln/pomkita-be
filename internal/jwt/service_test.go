package jwt

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type memoryStore struct {
	keys     map[string]Key
	sessions map[uuid.UUID]Session
}

func (s *memoryStore) ActiveKey(context.Context) (Key, error) {
	for _, key := range s.keys {
		if key.Status == KeyActive {
			return key, nil
		}
	}
	return Key{}, ErrUnknownKID
}

func (s *memoryStore) Key(_ context.Context, kid string) (Key, error) {
	key, ok := s.keys[kid]
	if !ok {
		return Key{}, ErrUnknownKID
	}
	return key, nil
}

func (s *memoryStore) CreateSession(_ context.Context, session Session) error {
	s.sessions[session.JTI] = session
	return nil
}

func (s *memoryStore) Session(_ context.Context, jti uuid.UUID) (Session, error) {
	session, ok := s.sessions[jti]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	return session, nil
}

func (s *memoryStore) RevokeSession(_ context.Context, jti uuid.UUID, at time.Time) error {
	session, ok := s.sessions[jti]
	if !ok {
		return ErrSessionNotFound
	}
	session.RevokedAt = &at
	s.sessions[jti] = session
	return nil
}

func TestService_VerifyRejectsInvalidClaimsWithDistinctErrors(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	key := Key{KID: "key_1", Secret: "secret", Status: KeyActive, MaxTokenExpiry: now.Add(15 * time.Minute)}
	userID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	jti := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	store := &memoryStore{keys: map[string]Key{"key_1": key}, sessions: map[uuid.UUID]Session{
		jti: {JTI: jti, KID: key.KID, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(10 * time.Minute)},
	}}
	service := NewService(store, Config{Issuer: "pomkita", Audience: "pomkita", Now: func() time.Time { return now }})

	cases := []struct {
		name   string
		token  string
		expect error
	}{
		{"algorithm", rawToken(t, "secret", map[string]any{"alg": "HS512", "kid": "key_1"}, standardClaims(userID, jti, now)), ErrInvalidAlgorithm},
		{"issuer", signedToken(t, "secret", "key_1", map[string]any{"iss": "other"}, userID, jti, now), ErrInvalidIssuer},
		{"audience", signedToken(t, "secret", "key_1", map[string]any{"aud": "other"}, userID, jti, now), ErrInvalidAudience},
		{"expired", signedToken(t, "secret", "key_1", map[string]any{"exp": now.Add(-2 * time.Minute).Unix()}, userID, jti, now), ErrExpired},
		{"future issued at", signedToken(t, "secret", "key_1", map[string]any{"iat": now.Add(time.Minute).Unix()}, userID, jti, now), ErrFutureIssuedAt},
		{"unknown kid", signedToken(t, "secret", "missing", nil, userID, jti, now), ErrUnknownKID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.Verify(context.Background(), tc.token)
			if !errors.Is(err, tc.expect) {
				t.Fatalf("got %v, want %v", err, tc.expect)
			}
		})
	}

	store.sessions[jti] = Session{JTI: jti, KID: key.KID, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(10 * time.Minute), RevokedAt: timePtr(now)}
	_, err := service.Verify(context.Background(), signedToken(t, "secret", "key_1", nil, userID, jti, now))
	if !errors.Is(err, ErrRevokedJTI) {
		t.Fatalf("got %v, want %v", err, ErrRevokedJTI)
	}
}

func TestService_IssueCreatesFifteenMinuteSessionAndUUIDv4JTI(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	key := Key{KID: "key_1", Secret: "secret", Status: KeyActive, MaxTokenExpiry: now.Add(15 * time.Minute)}
	store := &memoryStore{keys: map[string]Key{"key_1": key}, sessions: map[uuid.UUID]Session{}}
	service := NewService(store, Config{Issuer: "pomkita", Audience: "pomkita", Now: func() time.Time { return now }})

	token, claims, err := service.Issue(context.Background(), uuid.MustParse("33333333-3333-4333-8333-333333333333"))
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if token == "" {
		t.Fatal("issued token is empty")
	}
	if claims.ExpiresAt.Sub(claims.IssuedAt) != 15*time.Minute {
		t.Fatalf("token lifetime: got %s", claims.ExpiresAt.Sub(claims.IssuedAt))
	}
	if claims.JTI.Version() != 4 {
		t.Fatalf("JTI is not UUIDv4: %s", claims.JTI)
	}
	session, ok := store.sessions[claims.JTI]
	if !ok {
		t.Fatal("issued session was not stored")
	}
	if session.KID != key.KID || !session.ExpiresAt.Equal(claims.ExpiresAt) {
		t.Fatalf("stored session does not match token: %+v", session)
	}
}

func TestService_PreviousKeyAcceptedUntilMaximumTokenExpiry(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	previousExpiry := now.Add(5 * time.Minute)
	key := Key{KID: "old_key", Secret: "old-secret", Status: KeyPrevious, MaxTokenExpiry: previousExpiry}
	userID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	jti := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	store := &memoryStore{keys: map[string]Key{"old_key": key}, sessions: map[uuid.UUID]Session{
		jti: {JTI: jti, KID: key.KID, IssuedAt: now.Add(-time.Minute), ExpiresAt: previousExpiry},
	}}
	clock := now
	service := NewService(store, Config{Issuer: "pomkita", Audience: "pomkita", Now: func() time.Time { return clock }})
	token := signedToken(t, key.Secret, key.KID, nil, userID, jti, now)
	if _, err := service.Verify(context.Background(), token); err != nil {
		t.Fatalf("previous key rejected before cutoff: %v", err)
	}
	clock = previousExpiry.Add(time.Nanosecond)
	if _, err := service.Verify(context.Background(), token); !errors.Is(err, ErrRetiredKey) {
		t.Fatalf("got %v, want %v", err, ErrRetiredKey)
	}
}

func TestService_LogoutRevokesSession(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	jti := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	store := &memoryStore{keys: map[string]Key{}, sessions: map[uuid.UUID]Session{jti: {JTI: jti}}}
	service := NewService(store, Config{Now: func() time.Time { return now }})
	if err := service.Logout(context.Background(), jti); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if store.sessions[jti].RevokedAt == nil || !store.sessions[jti].RevokedAt.Equal(now) {
		t.Fatalf("session was not revoked: %+v", store.sessions[jti])
	}
}

func standardClaims(subject, jti uuid.UUID, now time.Time) map[string]any {
	return map[string]any{
		"iss": "pomkita", "sub": subject.String(), "jti": jti.String(),
		"iat": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(), "aud": "pomkita",
	}
}

func signedToken(t *testing.T, secret, kid string, changes map[string]any, subject, jti uuid.UUID, now time.Time) string {
	t.Helper()
	claims := standardClaims(subject, jti, now)
	for key, value := range changes {
		claims[key] = value
	}
	return rawToken(t, secret, map[string]any{"alg": "HS256", "kid": kid}, claims)
}

func rawToken(t *testing.T, secret string, header, claims map[string]any) string {
	t.Helper()
	encode := func(value any) string {
		bytes, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal JWT part: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(bytes)
	}
	input := encode(header) + "." + encode(claims)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func timePtr(value time.Time) *time.Time { return &value }
