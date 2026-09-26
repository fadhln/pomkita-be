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
	user                                            User
	userErr                                         error
	read                                            appjwt.SessionView
	readErr                                         error
	createdJTI                                      uuid.UUID
	orgExists                                       bool
	stationExists                                   bool
	preference                                      *appjwt.ActiveContext
	setContextErr                                   error
	setContextJTI, setContextOrg, setContextStation uuid.UUID
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
func (r *repositoryStub) OrganizationExists(context.Context, uuid.UUID) (bool, error) {
	return r.orgExists, nil
}
func (r *repositoryStub) StationInOrganization(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return r.stationExists, nil
}
func (r *repositoryStub) ReadContextPreference(context.Context, uuid.UUID) (*appjwt.ActiveContext, error) {
	return r.preference, nil
}
func (r *repositoryStub) SetActiveContext(_ context.Context, jti, orgID, stationID uuid.UUID, _ time.Time) error {
	r.setContextJTI, r.setContextOrg, r.setContextStation = jti, orgID, stationID
	return r.setContextErr
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

func TestService_SetActiveContextPersistsForSuperadmin(t *testing.T) {
	jti, orgID, stationID := uuid.New(), uuid.New(), uuid.New()
	repository := &repositoryStub{orgExists: true, stationExists: true}
	service := NewService(repository, nil)
	if err := service.SetActiveContext(context.Background(), jti, []string{"Superadmin"}, orgID, stationID); err != nil {
		t.Fatalf("set active context: %v", err)
	}
	if repository.setContextJTI != jti || repository.setContextOrg != orgID || repository.setContextStation != stationID {
		t.Fatalf("stored context: jti=%s org=%s station=%s", repository.setContextJTI, repository.setContextOrg, repository.setContextStation)
	}
}

func TestService_SetActiveContextAcceptsDisabledExistingTargets(t *testing.T) {
	jti, orgID, stationID := uuid.New(), uuid.New(), uuid.New()
	repository := &repositoryStub{orgExists: true, stationExists: true}
	service := NewService(repository, nil)

	err := service.SetActiveContext(context.Background(), jti, []string{"Superadmin"}, orgID, stationID)

	if err != nil {
		t.Fatalf("set context for disabled existing targets: got %v, want nil", err)
	}
	if repository.setContextJTI != jti || repository.setContextOrg != orgID || repository.setContextStation != stationID {
		t.Fatalf("stored context: jti=%s org=%s station=%s", repository.setContextJTI, repository.setContextOrg, repository.setContextStation)
	}
}

func TestService_SetActiveContextRejectsForbiddenAndInvalidTargets(t *testing.T) {
	jti, orgID, stationID := uuid.New(), uuid.New(), uuid.New()
	for _, test := range []struct {
		name                     string
		roles                    []string
		orgExists, stationExists bool
		want                     error
	}{
		{name: "non superadmin", roles: []string{"Owner"}, orgExists: true, stationExists: true, want: ErrActiveContextForbidden},
		{name: "organization absent", roles: []string{"Superadmin"}, stationExists: true, want: ErrActiveContextNotFound},
		{name: "station absent or outside organization", roles: []string{"Superadmin"}, orgExists: true, want: ErrActiveContextNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &repositoryStub{orgExists: test.orgExists, stationExists: test.stationExists}
			service := NewService(repository, nil)
			err := service.SetActiveContext(context.Background(), jti, test.roles, orgID, stationID)
			if !errors.Is(err, test.want) {
				t.Fatalf("error: got %v, want %v", err, test.want)
			}
			if repository.setContextJTI != uuid.Nil {
				t.Fatal("invalid context was stored")
			}
		})
	}
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

func TestService_Login_RestoresSavedContextForNewSession(t *testing.T) {
	userID := uuid.New()
	orgID, stationID := uuid.New(), uuid.New()
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	repository := &repositoryStub{
		user:       User{UserID: userID, PasswordHash: string(hash), Enabled: true},
		preference: &appjwt.ActiveContext{OrgID: orgID, StationID: stationID},
		orgExists:  true, stationExists: true,
	}
	tokens := appjwt.NewService(&tokenStoreStub{key: appjwt.Key{KID: "key-1", Secret: "test-secret", Status: appjwt.KeyActive}}, appjwt.Config{Issuer: "test", Audience: "test"})
	service := NewService(repository, tokens)

	_, claims, err := service.Login(context.Background(), "user@example.com", "correct-password")

	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if repository.setContextJTI != claims.JTI || repository.setContextOrg != orgID || repository.setContextStation != stationID {
		t.Fatalf("restored context: jti=%s org=%s station=%s", repository.setContextJTI, repository.setContextOrg, repository.setContextStation)
	}
}

func TestService_Login_InvalidSavedContextFallsBackToIdentityDefaults(t *testing.T) {
	userID := uuid.New()
	orgID, stationID := uuid.New(), uuid.New()
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	for _, test := range []struct {
		name                     string
		orgExists, stationExists bool
	}{
		{name: "organization no longer exists", orgExists: false, stationExists: true},
		{name: "station no longer belongs to organization", orgExists: true, stationExists: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &repositoryStub{
				user:       User{UserID: userID, PasswordHash: string(hash), Enabled: true},
				preference: &appjwt.ActiveContext{OrgID: orgID, StationID: stationID},
				orgExists:  test.orgExists, stationExists: test.stationExists,
			}
			tokens := appjwt.NewService(&tokenStoreStub{key: appjwt.Key{KID: "key-1", Secret: "test-secret", Status: appjwt.KeyActive}}, appjwt.Config{Issuer: "test", Audience: "test"})
			service := NewService(repository, tokens)

			_, _, err := service.Login(context.Background(), "user@example.com", "correct-password")

			if err != nil {
				t.Fatalf("login with invalid saved context: %v", err)
			}
			if repository.setContextJTI != uuid.Nil {
				t.Fatalf("invalid context was seeded: org=%s station=%s", repository.setContextOrg, repository.setContextStation)
			}
		})
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
