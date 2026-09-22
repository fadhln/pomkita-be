package httpapi_test

import (
	"context"
	. "github.com/fadhln/pomkita-be/internal/httpapi"
	policyapi "github.com/fadhln/pomkita-be/internal/httpapi/policy"
	"net/http"
	"net/http/httptest"
	"testing"

	apppolicy "github.com/fadhln/pomkita-be/internal/service/policy"
	"github.com/google/uuid"
)

func TestPolicyHistoryRoute_UsesVerifiedSessionScope(t *testing.T) {
	orgID, stationID := uuid.New(), uuid.New()
	service := &PolicyReadStub{}
	sessions := &SessionStub{view: SessionView{OrgID: orgID, UserID: uuid.New(), Roles: []string{"Owner"}, StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, PolicyRead: service, LatestMigration: 11})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/policies/history?station_id="+stationID.String(), nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.orgID != orgID || service.stationID != stationID {
		t.Fatalf("policy scope: org=%s station=%s", service.orgID, service.stationID)
	}
}

type PolicyReadStub struct {
	orgID     uuid.UUID
	stationID uuid.UUID
}

func (s *PolicyReadStub) History(_ context.Context, orgID uuid.UUID, stationID *uuid.UUID) ([]apppolicy.RevisionView, error) {
	s.orgID = orgID
	if stationID != nil {
		s.stationID = *stationID
	}
	return []apppolicy.RevisionView{}, nil
}

var _ policyapi.PolicyReadService = (*PolicyReadStub)(nil)
