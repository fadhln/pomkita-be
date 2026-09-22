package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	appjwt "github.com/fadhln/pomkita-be/internal/jwt"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type repositoryStub struct {
	user       User
	userErr    error
	read       appjwt.SessionView
	readErr    error
	createdJTI uuid.UUID
}

type usernameRepositoryStub struct {
	repositoryStub
	username string
}

func (r *usernameRepositoryStub) FindUserByUsername(_ context.Context, username string) (User, error) {
	r.username = username
	return r.user, r.userErr
}

func (r *repositoryStub) FindUserByEmail(context.Context, string) (User, error) {
	return r.user, r.userErr
}

func (r *repositoryStub) FindUserByUsername(context.Context, string) (User, error) {
	return r.user, r.userErr
}

func (r *repositoryStub) ReadSession(context.Context, uuid.UUID, uuid.UUID) (appjwt.SessionView, error) {
	return r.read, r.readErr
}

type tokenStoreStub struct {
	key     appjwt.Key
	issued  appjwt.Session
	view    appjwt.Session
	revoked uuid.UUID
}

func (s *tokenStoreStub) ActiveKey(context.Context) (appjwt.Key, error)   { return s.key, nil }
func (s *tokenStoreStub) Key(context.Context, string) (appjwt.Key, error) { return s.key, nil }
func (s *tokenStoreStub) CreateSession(_ context.Context, session appjwt.Session) error {
	s.issued = session
	return nil
}
func (s *tokenStoreStub) Session(context.Context, uuid.UUID) (appjwt.Session, error) {
	return s.view, nil
}
func (s *tokenStoreStub) RevokeSession(_ context.Context, jti uuid.UUID, _ time.Time) error {
	s.revoked = jti
	return nil
}

func TestService_Login_ValidCredentialsCreatesSession(t *testing.T) {
	userID := uuid.New()
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	repository := &repositoryStub{user: User{UserID: userID, Email: "user@example.com", PasswordHash: string(hash), Enabled: true}}
	tokens := appjwt.NewService(&tokenStoreStub{key: appjwt.Key{KID: "key-1", Secret: "test-secret", Status: appjwt.KeyActive}}, appjwt.Config{Issuer: "test", Audience: "test"})

	service := NewService(repository, tokens)
	token, claims, err := service.Login(context.Background(), "user@example.com", "correct-password")

	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if token == "" || claims.Subject != userID {
		t.Fatalf("login result: token=%q subject=%s", token, claims.Subject)
	}
}

func TestService_Login_InvalidCredentialsReturnsAuthenticationError(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	tokens := appjwt.NewService(&tokenStoreStub{key: appjwt.Key{KID: "key-1", Secret: "test-secret", Status: appjwt.KeyActive}}, appjwt.Config{Issuer: "test", Audience: "test"})
	service := NewService(&repositoryStub{user: User{UserID: uuid.New(), PasswordHash: string(hash), Enabled: true}}, tokens)

	_, _, err = service.Login(context.Background(), "user@example.com", "wrong-password")

	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("login error: got %v, want invalid credentials", err)
	}
}

func TestService_Login_UsesUsernameLookup(t *testing.T) {
	userID := uuid.New()
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	repository := &usernameRepositoryStub{repositoryStub: repositoryStub{user: User{UserID: userID, Username: "budi", PasswordHash: string(hash), Enabled: true}}}
	tokens := appjwt.NewService(&tokenStoreStub{key: appjwt.Key{KID: "key-1", Secret: "test-secret", Status: appjwt.KeyActive}}, appjwt.Config{Issuer: "test", Audience: "test"})

	service := NewService(repository, tokens)
	if _, _, err := service.Login(context.Background(), "Budi", "correct-password"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if repository.username != "budi" {
		t.Fatalf("username lookup: got %q, want budi", repository.username)
	}
}
