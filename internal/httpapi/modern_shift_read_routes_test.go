package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	appshift "github.com/pomkita/pomkita-be/internal/service/shift"
)

func TestModernShiftListRoute_UsesVerifiedOrganizationAndStationScope(t *testing.T) {
	orgID, stationID := uuid.New(), uuid.New()
	service := &modernShiftReadStub{}
	sessions := &modernSessionStub{view: SessionView{OrgID: orgID, UserID: uuid.New(), StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, ModernShiftRead: service, LatestMigration: 11})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/shifts?station_id="+stationID.String(), nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.orgID != orgID || service.stationID != stationID {
		t.Fatalf("read scope: org=%s station=%s", service.orgID, service.stationID)
	}
}

type modernShiftReadStub struct {
	orgID     uuid.UUID
	stationID uuid.UUID
}

func (s *modernShiftReadStub) List(_ context.Context, orgID uuid.UUID, stationID *uuid.UUID) ([]appshift.Summary, error) {
	s.orgID = orgID
	if stationID != nil {
		s.stationID = *stationID
	}
	return []appshift.Summary{}, nil
}

func (s *modernShiftReadStub) Detail(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (appshift.Detail, error) {
	return appshift.Detail{}, nil
}

var _ ModernShiftReadService = (*modernShiftReadStub)(nil)
