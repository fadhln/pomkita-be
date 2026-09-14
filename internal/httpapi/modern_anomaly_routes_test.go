package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
)

func TestModernAnomalyExportRoute_UsesVerifiedOrganization(t *testing.T) {
	orgID := uuid.New()
	service := &modernAnomalyStub{}
	sessions := &modernSessionStub{view: SessionView{OrgID: orgID, UserID: uuid.New(), Roles: []string{"Owner"}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, ModernAnomalies: service, LatestMigration: 11})
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

type modernAnomalyStub struct{ orgID uuid.UUID }

func (s *modernAnomalyStub) Anomalies(_ context.Context, orgID uuid.UUID, _ *uuid.UUID) ([]appreporting.AnomalyView, error) {
	s.orgID = orgID
	return []appreporting.AnomalyView{}, nil
}

var _ ModernAnomalyService = (*modernAnomalyStub)(nil)
