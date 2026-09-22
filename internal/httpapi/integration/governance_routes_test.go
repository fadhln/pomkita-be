package httpapi_test

import (
	"context"
	. "github.com/fadhln/pomkita-be/internal/httpapi"
	governanceapi "github.com/fadhln/pomkita-be/internal/httpapi/governance"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appgovernance "github.com/fadhln/pomkita-be/internal/service/governance"
	"github.com/google/uuid"
)

func TestAcknowledgeRoute_UsesVerifiedRoleAndScope(t *testing.T) {
	orgID, stationID, actorID, shiftID, reportID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	service := &GovernanceStub{}
	sessions := &SessionStub{view: SessionView{OrgID: orgID, UserID: actorID, Roles: []string{"Station Admin"}, StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, Governance: service, LatestMigration: 11})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/reports/"+reportID.String()+"/acknowledgement", strings.NewReader(`{"station_id":"`+stationID.String()+`","shift_id":"`+shiftID.String()+`","version_no":1,"decision":"acked"}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.request.OrgID != orgID || service.request.ActorID != actorID || service.request.Role != "Station Admin" || service.request.ReportID != reportID {
		t.Fatalf("ack request: %+v", service.request)
	}
}

type GovernanceStub struct {
	request appgovernance.AcknowledgeRequest
}

func (s *GovernanceStub) Acknowledge(_ context.Context, request appgovernance.AcknowledgeRequest) (appgovernance.Acknowledgement, error) {
	s.request = request
	return appgovernance.Acknowledgement{AckID: uuid.New(), ReportID: request.ReportID, VersionNo: request.VersionNo, Decision: request.Decision}, nil
}

var _ governanceapi.GovernanceService = (*GovernanceStub)(nil)
