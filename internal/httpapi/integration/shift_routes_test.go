package httpapi_test

import (
	"context"
	"encoding/json"
	. "github.com/pomkita/pomkita-be/internal/httpapi"
	shiftapi "github.com/pomkita/pomkita-be/internal/httpapi/shift"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	appshift "github.com/pomkita/pomkita-be/internal/service/shift"
)

func TestOpenShiftRoute_UsesSessionTenantAndReturnsDecimalSafeShift(t *testing.T) {
	orgID, stationID, actorID := uuid.New(), uuid.New(), uuid.New()
	service := &ShiftStub{result: appshift.Shift{ShiftID: uuid.New(), OrgID: orgID, StationID: stationID, StationSeq: 4, SupervisorID: actorID, OpenedAt: time.Date(2026, 9, 14, 3, 0, 0, 0, time.UTC), Status: appshift.StatusOpen}}
	sessions := &SessionStub{view: SessionView{OrgID: orgID, UserID: actorID, Roles: []string{"Supervisor"}, StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, Shift: service, LatestMigration: 11})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/shifts", strings.NewReader(`{"station_id":"`+stationID.String()+`","opened_at":"2026-09-14T03:00:00Z","backfilled":false}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.request.OrgID != orgID || service.request.ActorID != actorID || service.request.Role != "Supervisor" || service.request.StationID != stationID {
		t.Fatalf("open request: %+v", service.request)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["status"] != string(appshift.StatusOpen) {
		t.Fatalf("response: %s", recorder.Body.String())
	}
}

type ShiftStub struct {
	request appshift.OpenRequest
	result  appshift.Shift
}

func (s *ShiftStub) OpenShift(_ context.Context, request appshift.OpenRequest) (appshift.Shift, error) {
	s.request = request
	return s.result, nil
}

var _ shiftapi.ShiftService = (*ShiftStub)(nil)
