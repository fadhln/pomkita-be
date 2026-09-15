package httpapi_test

import (
	"context"
	"encoding/json"
	. "github.com/pomkita/pomkita-be/internal/httpapi"
	governanceapi "github.com/pomkita/pomkita-be/internal/httpapi/governance"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	appgovernance "github.com/pomkita/pomkita-be/internal/service/governance"
)

func TestAmendmentRequestRoute_UsesVerifiedSessionScope(t *testing.T) {
	orgID, stationID, shiftID, reportID, actorID, targetID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	service := &AmendmentStub{}
	sessions := &SessionStub{view: SessionView{OrgID: orgID, UserID: actorID, Roles: []string{"Supervisor"}, StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{
		Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions,
		Amendment: service, LatestMigration: 11,
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/amendments", strings.NewReader(`{"station_id":"`+stationID.String()+`","shift_id":"`+shiftID.String()+`","base_report_id":"`+reportID.String()+`","reason":"correct cash","stale_check_hash":"0000000000000000000000000000000000000000000000000000000000000000","items":[{"target_kind":"sales_declared","target_logical_id":"`+targetID.String()+`","field":"cash_amount","old_value":"100","new_value":"110"}]}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.request.OrgID != orgID || service.request.RequesterID != actorID || service.request.StationID != stationID || service.request.Role != "Supervisor" {
		t.Fatalf("amendment request: %+v", service.request)
	}
}

func TestAmendmentQueueRoute_UsesVerifiedApproverScope(t *testing.T) {
	orgID, stationID, actorID := uuid.New(), uuid.New(), uuid.New()
	amendmentID := uuid.New()
	service := &AmendmentStub{queue: []appgovernance.AmendmentQueueView{{AmendmentID: amendmentID, StationID: stationID, Status: "pending", Items: []appgovernance.AmendmentQueueItem{}}}}
	sessions := &SessionStub{view: SessionView{OrgID: orgID, UserID: actorID, Roles: []string{"Station Admin"}, StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{
		Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions,
		Amendment: service, LatestMigration: 11,
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/amendments", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.queueRequest.OrgID != orgID || service.queueRequest.ActorID != actorID || service.queueRequest.Role != "Station Admin" || len(service.queueRequest.StationIDs) != 1 || service.queueRequest.StationIDs[0] != stationID {
		t.Fatalf("queue request: %+v", service.queueRequest)
	}
	var response []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response) != 1 || response[0]["AmendmentID"] != amendmentID.String() {
		t.Fatalf("response: got %v", response)
	}
}

type AmendmentStub struct {
	request      appgovernance.AmendmentRequest
	queue        []appgovernance.AmendmentQueueView
	queueRequest appgovernance.AmendmentQueueRequest
}

func (s *AmendmentStub) Request(_ context.Context, request appgovernance.AmendmentRequest) (appgovernance.Amendment, error) {
	s.request = request
	return appgovernance.Amendment{}, nil
}

func (s *AmendmentStub) Approve(context.Context, appgovernance.ApproveAmendmentRequest) (appgovernance.Amendment, error) {
	return appgovernance.Amendment{}, nil
}

func (s *AmendmentStub) Reject(context.Context, appgovernance.RejectAmendmentRequest) error {
	return nil
}

func (s *AmendmentStub) ListQueue(_ context.Context, request appgovernance.AmendmentQueueRequest) ([]appgovernance.AmendmentQueueView, error) {
	s.queueRequest = request
	return s.queue, nil
}

var _ governanceapi.AmendmentService = (*AmendmentStub)(nil)
