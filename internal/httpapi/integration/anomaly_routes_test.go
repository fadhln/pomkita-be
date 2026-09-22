package httpapi_test

import (
	"context"
	. "github.com/fadhln/pomkita-be/internal/httpapi"
	reportingapi "github.com/fadhln/pomkita-be/internal/httpapi/reporting"
	"net/http"
	"net/http/httptest"
	"testing"

	appreporting "github.com/fadhln/pomkita-be/internal/service/reporting"
	"github.com/google/uuid"
)

func TestAnomalyExportRoute_UsesVerifiedOrganization(t *testing.T) {
	orgID := uuid.New()
	service := &AnomalyStub{}
	sessions := &SessionStub{view: SessionView{OrgID: orgID, UserID: uuid.New(), Roles: []string{"Owner"}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, Anomalies: service, LatestMigration: 11})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/anomalies/export", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.orgID != orgID {
		t.Fatalf("anomaly scope: got %s want %s", service.orgID, orgID)
	}
}

func TestAnomalyRoute_RequiresStationForStationScopedActor(t *testing.T) {
	service := &AnomalyStub{}
	sessions := &SessionStub{view: SessionView{OrgID: uuid.New(), UserID: uuid.New(), Roles: []string{"Supervisor"}, StationIDs: []uuid.UUID{uuid.New(), uuid.New()}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, Anomalies: service, LatestMigration: 11})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/anomalies", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status: got %d body=%s", recorder.Code, recorder.Body.String())
	}
}

type AnomalyStub struct{ orgID uuid.UUID }

func (s *AnomalyStub) Anomalies(_ context.Context, orgID uuid.UUID, _ *uuid.UUID) ([]appreporting.AnomalyView, error) {
	s.orgID = orgID
	return []appreporting.AnomalyView{}, nil
}

var _ reportingapi.AnomalyService = (*AnomalyStub)(nil)
