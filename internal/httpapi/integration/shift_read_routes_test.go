package httpapi_test

import (
	"context"
	. "github.com/pomkita/pomkita-be/internal/httpapi"
	shiftapi "github.com/pomkita/pomkita-be/internal/httpapi/shift"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	appshift "github.com/pomkita/pomkita-be/internal/service/shift"
)

func TestShiftListRoute_UsesVerifiedOrganizationAndStationScope(t *testing.T) {
	orgID, stationID := uuid.New(), uuid.New()
	service := &ShiftReadStub{}
	sessions := &SessionStub{view: SessionView{OrgID: orgID, UserID: uuid.New(), StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, ShiftRead: service, LatestMigration: 11})
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

type ShiftReadStub struct {
	orgID     uuid.UUID
	stationID uuid.UUID
}

func (s *ShiftReadStub) List(_ context.Context, orgID uuid.UUID, stationID *uuid.UUID) ([]appshift.Summary, error) {
	s.orgID = orgID
	if stationID != nil {
		s.stationID = *stationID
	}
	return []appshift.Summary{}, nil
}

func (s *ShiftReadStub) Detail(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (appshift.Detail, error) {
	return appshift.Detail{}, nil
}

var _ shiftapi.ShiftReadService = (*ShiftReadStub)(nil)
