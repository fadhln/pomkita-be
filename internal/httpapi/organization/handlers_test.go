package organization

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appjwt "github.com/fadhln/pomkita-be/internal/jwt"
	apporg "github.com/fadhln/pomkita-be/internal/service/organization"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type httpServiceStub struct{}

func (httpServiceStub) List(context.Context, apporg.Actor) ([]apporg.Organization, error) {
	return []apporg.Organization{}, nil
}
func (httpServiceStub) Read(context.Context, apporg.Actor, uuid.UUID) (apporg.Organization, error) {
	return apporg.Organization{}, nil
}
func (httpServiceStub) Create(context.Context, apporg.Actor, apporg.CreateRequest) (apporg.Organization, error) {
	return apporg.Organization{}, nil
}
func (httpServiceStub) Update(context.Context, apporg.Actor, uuid.UUID, apporg.UpdateRequest) (apporg.Organization, error) {
	return apporg.Organization{}, nil
}
func (httpServiceStub) Disable(context.Context, apporg.Actor, uuid.UUID) error { return nil }

type httpVerifier struct{}

func (httpVerifier) Verify(context.Context, string) (appjwt.Claims, error) {
	return appjwt.Claims{Subject: uuid.New(), JTI: uuid.New()}, nil
}

type httpSessions struct{ view appjwt.SessionView }

func (s httpSessions) ReadSession(context.Context, string) (appjwt.SessionView, error) {
	return s.view, nil
}

func TestOrganizationsWithoutSessionAreUnauthorized(t *testing.T) {
	router := gin.New()
	RegisterRoutes(router, nil, nil, httpServiceStub{})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/organizations", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestOrganizationsWrongRoleAreForbidden(t *testing.T) {
	router := gin.New()
	RegisterRoutes(router, httpVerifier{}, httpSessions{view: appjwt.SessionView{OrgID: uuid.New(), Roles: []string{"Operator"}}}, httpServiceStub{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/organizations", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestCreateOrganizationRequiresFirstStation(t *testing.T) {
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("raw_token", "token"); c.Next() })
	RegisterRoutes(router, httpVerifier{}, httpSessions{view: appjwt.SessionView{Roles: []string{"Superadmin"}}}, httpServiceStub{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/organizations", strings.NewReader(`{"name":"Org"}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Requested-With", "PomKita")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest && recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status: got %d, want validation status", recorder.Code)
	}
}
