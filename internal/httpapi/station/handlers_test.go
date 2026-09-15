package station

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	appstation "github.com/pomkita/pomkita-be/internal/service/station"
)

type stationHTTPServiceStub struct{}

func (stationHTTPServiceStub) List(context.Context, appstation.Actor, uuid.UUID) ([]appstation.Station, error) {
	return []appstation.Station{}, nil
}
func (stationHTTPServiceStub) Read(context.Context, appstation.Actor, uuid.UUID, uuid.UUID) (appstation.Station, error) {
	return appstation.Station{}, nil
}
func (stationHTTPServiceStub) Create(context.Context, appstation.Actor, uuid.UUID, appstation.CreateRequest) (appstation.Station, error) {
	return appstation.Station{}, nil
}
func (stationHTTPServiceStub) Update(context.Context, appstation.Actor, uuid.UUID, uuid.UUID, appstation.UpdateRequest) (appstation.Station, error) {
	return appstation.Station{}, nil
}
func (stationHTTPServiceStub) Disable(context.Context, appstation.Actor, uuid.UUID, uuid.UUID) error {
	return nil
}

type stationHTTPVerifier struct{}

func (stationHTTPVerifier) Verify(context.Context, string) (appjwt.Claims, error) {
	return appjwt.Claims{Subject: uuid.New(), JTI: uuid.New()}, nil
}

type stationHTTPSessions struct{ view appjwt.SessionView }

func (s stationHTTPSessions) ReadSession(context.Context, string) (appjwt.SessionView, error) {
	return s.view, nil
}

func TestStationsWithoutSessionAreUnauthorized(t *testing.T) {
	router := gin.New()
	RegisterRoutes(router, nil, nil, stationHTTPServiceStub{})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/stations", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestStationsWrongRoleAreForbiddenOnEveryPath(t *testing.T) {
	router := gin.New()
	RegisterRoutes(router, stationHTTPVerifier{}, stationHTTPSessions{view: appjwt.SessionView{OrgID: uuid.New(), Roles: []string{"Operator"}}}, stationHTTPServiceStub{})
	paths := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/stations", ""},
		{http.MethodPost, "/stations", `{"name":"Main","timezone":"UTC"}`},
		{http.MethodGet, "/stations/33333333-3333-4333-8333-333333333333", ""},
		{http.MethodPatch, "/stations/33333333-3333-4333-8333-333333333333", `{"name":"Updated"}`},
		{http.MethodPost, "/stations/33333333-3333-4333-8333-333333333333/disable", "{}"},
	}
	for _, item := range paths {
		t.Run(item.method+item.path, func(t *testing.T) {
			request := httptest.NewRequest(item.method, item.path, strings.NewReader(item.body))
			request.Header.Set("Authorization", "Bearer token")
			request.Header.Set("X-Requested-With", "PomKita")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusForbidden)
			}
		})
	}
}

func TestCreateStationRequiresName(t *testing.T) {
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("raw_token", "token"); c.Next() })
	orgID := uuid.New()
	RegisterRoutes(router, stationHTTPVerifier{}, stationHTTPSessions{view: appjwt.SessionView{OrgID: orgID, Roles: []string{"Owner"}}}, stationHTTPServiceStub{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/stations", strings.NewReader(`{"timezone":"UTC"}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Requested-With", "PomKita")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest && recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status: got %d, want validation status", recorder.Code)
	}
}

func TestSuperadminStationListRequiresOrganizationScope(t *testing.T) {
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("raw_token", "token"); c.Next() })
	RegisterRoutes(router, stationHTTPVerifier{}, stationHTTPSessions{view: appjwt.SessionView{OrgID: uuid.New(), Roles: []string{"Superadmin"}}}, stationHTTPServiceStub{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/stations", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
}
