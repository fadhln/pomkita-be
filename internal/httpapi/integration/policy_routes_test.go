package httpapi_test

import (
	"context"
	. "github.com/fadhln/pomkita-be/internal/httpapi"
	policyapi "github.com/fadhln/pomkita-be/internal/httpapi/policy"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apppolicy "github.com/fadhln/pomkita-be/internal/service/policy"
	"github.com/google/uuid"
)

func TestPolicyRevisionRoute_UsesVerifiedOwnerScope(t *testing.T) {
	orgID, stationID, actorID := uuid.New(), uuid.New(), uuid.New()
	service := &PolicyStub{result: apppolicy.PolicyRevision{RevisionID: uuid.New(), PolicyID: uuid.New(), PolicyKind: "threshold", ValidFrom: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)}}
	sessions := &SessionStub{view: SessionView{OrgID: orgID, UserID: actorID, Roles: []string{"Owner"}, StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, Policy: service, LatestMigration: 11})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/policies/revisions", strings.NewReader(`{"policy_kind":"threshold","policy_id":"`+uuid.New().String()+`","station_id":"`+stationID.String()+`","valid_from":"2026-09-14T00:00:00Z","loss_liter_threshold":"10.00","gain_liter_threshold":"11.00","loss_rupiah_threshold":"100","gain_rupiah_threshold":"200","variance_rupiah_threshold":"300","rollover_threshold":"20.0"}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.request.OrgID != orgID || service.request.ActorID != actorID || service.request.Role != "Owner" || service.request.StationID != stationID {
		t.Fatalf("policy request: %+v", service.request)
	}
}

type PolicyStub struct {
	request apppolicy.PolicyRevisionRequest
	result  apppolicy.PolicyRevision
}

func (s *PolicyStub) CreateRevision(_ context.Context, request apppolicy.PolicyRevisionRequest) (apppolicy.PolicyRevision, error) {
	s.request = request
	return s.result, nil
}

var _ policyapi.PolicyService = (*PolicyStub)(nil)
