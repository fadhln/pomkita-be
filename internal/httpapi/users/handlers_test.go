package users

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/httpapi/transport"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	appidentity "github.com/pomkita/pomkita-be/internal/service/identity"
)

type serviceStub struct {
	inviteErr error
	acceptErr error
	invite    appidentity.InvitationRequest
	adminErr  error
}

func (s *serviceStub) Invite(_ context.Context, request appidentity.InvitationRequest) error {
	s.invite = request
	return s.inviteErr
}
func (s *serviceStub) Accept(_ context.Context, _ appidentity.AcceptanceRequest) error {
	return s.acceptErr
}
func (s *serviceStub) ListUsers(context.Context, appidentity.Actor, uuid.UUID) ([]appidentity.UserView, error) {
	return []appidentity.UserView{}, s.adminErr
}
func (s *serviceStub) ReadUser(context.Context, appidentity.Actor, uuid.UUID) (appidentity.UserView, error) {
	return appidentity.UserView{}, s.adminErr
}
func (s *serviceStub) UpdateUser(context.Context, appidentity.Actor, uuid.UUID, appidentity.UserUpdateRequest) (appidentity.UserView, error) {
	return appidentity.UserView{}, s.adminErr
}
func (s *serviceStub) AssignRole(context.Context, appidentity.Actor, uuid.UUID, appidentity.RoleRequest) ([]appidentity.RoleView, error) {
	return []appidentity.RoleView{}, s.adminErr
}
func (s *serviceStub) RemoveRole(context.Context, appidentity.Actor, uuid.UUID, appidentity.RoleRequest) error {
	return s.adminErr
}
func (s *serviceStub) RoleHistory(context.Context, appidentity.Actor, uuid.UUID) ([]appidentity.RoleHistoryEvent, error) {
	return []appidentity.RoleHistoryEvent{}, s.adminErr
}
func (s *serviceStub) IssuePasswordReset(context.Context, appidentity.Actor, uuid.UUID) (appidentity.PasswordResetResult, error) {
	return appidentity.PasswordResetResult{}, s.adminErr
}

var _ AdminService = (*serviceStub)(nil)

type sessionStub struct {
	view appjwt.SessionView
	err  error
}

func (s sessionStub) ReadSession(context.Context, string) (appjwt.SessionView, error) {
	return s.view, s.err
}

type verifier struct{ err error }

func (v verifier) Verify(context.Context, string) (appjwt.Claims, error) {
	return appjwt.Claims{Subject: uuid.New()}, v.err
}

func TestInviteHandler_RequiresSession(t *testing.T) {
	router := testRouter(verifier{err: appjwt.ErrSessionNotFound}, sessionStub{}, &serviceStub{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"email":"new@example.test","display_name":"New","role":"Operator","station_id":"11111111-1111-4111-8111-111111111111"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", recorder.Code)
	}
}

func TestInviteHandler_PassesAuthenticatedOwnerScope(t *testing.T) {
	orgID, actorID, stationID := uuid.New(), uuid.New(), uuid.New()
	service := &serviceStub{}
	router := testRouter(verifier{}, sessionStub{view: appjwt.SessionView{UserID: actorID, OrgID: orgID, Roles: []string{"Owner"}}}, service)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"email":"new@example.test","display_name":"New","role":"Operator","station_id":"`+stationID.String()+`"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || service.invite.TargetOrgID != orgID || service.invite.ActorRole != "Owner" {
		t.Fatalf("response=%d invitation=%+v", recorder.Code, service.invite)
	}
}

func TestAcceptHandler_MapsInvalidTokenToBadRequest(t *testing.T) {
	router := testRouter(verifier{}, sessionStub{}, &serviceStub{acceptErr: appidentity.ErrInvalidToken})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/auth/invitations/accept", strings.NewReader(`{"token":"unknown","username":"new-user","password":"strong-password","display_name":"New"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"INVALID_TOKEN"`) {
		t.Fatalf("response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAdminUserRoutesRequireAdministrationRole(t *testing.T) {
	service := &serviceStub{adminErr: appidentity.ErrForbidden}
	router := testRouter(verifier{}, sessionStub{view: appjwt.SessionView{UserID: uuid.New(), OrgID: uuid.New(), Roles: []string{"Supervisor"}}}, service)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"user_administration_forbidden"`) {
		t.Fatalf("response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAdminUserRoutesRequireSession(t *testing.T) {
	router := testRouter(verifier{err: appjwt.ErrSessionNotFound}, sessionStub{}, &serviceStub{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", recorder.Code)
	}
}

func TestAdminUserRoutesReturnBareList(t *testing.T) {
	service := &serviceStub{}
	router := testRouter(verifier{}, sessionStub{view: appjwt.SessionView{UserID: uuid.New(), OrgID: uuid.New(), Roles: []string{"Owner"}}}, service)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "[]" {
		t.Fatalf("response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func testRouter(verifier transport.TokenVerifier, sessions transport.SessionService, service Service) *gin.Engine {
	router := gin.New()
	router.Use(transport.RequestID(), transport.ErrorMappingMiddleware())
	RegisterRoutes(router, verifier, sessions, service)
	return router
}
