package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	appgovernance "github.com/pomkita/pomkita-be/internal/service/governance"
)

func TestModernAmendmentRequestRoute_UsesVerifiedSessionScope(t *testing.T) {
	orgID, stationID, shiftID, reportID, actorID, targetID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	service := &modernAmendmentStub{}
	sessions := &modernSessionStub{view: SessionView{OrgID: orgID, UserID: actorID, Roles: []string{"Supervisor"}, StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{
		Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions,
		ModernAmendment: service, LatestMigration: 11,
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

type modernAmendmentStub struct {
	request appgovernance.AmendmentRequest
}

func (s *modernAmendmentStub) Request(_ context.Context, request appgovernance.AmendmentRequest) (appgovernance.Amendment, error) {
	s.request = request
	return appgovernance.Amendment{}, nil
}

func (s *modernAmendmentStub) Approve(context.Context, appgovernance.ApproveAmendmentRequest) (appgovernance.Amendment, error) {
	return appgovernance.Amendment{}, nil
}

func (s *modernAmendmentStub) Reject(context.Context, appgovernance.RejectAmendmentRequest) error {
	return nil
}

var _ ModernAmendmentService = (*modernAmendmentStub)(nil)
