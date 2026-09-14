package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	appsubmission "github.com/pomkita/pomkita-be/internal/service/submission"
)

func TestModernSubmitRoute_UsesSessionScopeAndIdempotencyHeader(t *testing.T) {
	orgID, stationID, actorID := uuid.New(), uuid.New(), uuid.New()
	service := &modernSubmissionStub{result: appsubmission.Result{ReportID: uuid.New()}}
	sessions := &modernSessionStub{view: SessionView{OrgID: orgID, UserID: actorID, StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, ModernSubmission: service, LatestMigration: 11})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/submissions", strings.NewReader(`{"station_id":"`+stationID.String()+`","shift_id":"`+uuid.New().String()+`","draft_id":"`+uuid.New().String()+`","claim_token":"`+uuid.New().String()+`","revision":2,"payload":{"readings":[],"sales":[],"losses":[]}}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	request.Header.Set("Idempotency-Key", "submit-1")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.request.OrgID != orgID || service.request.StationID != stationID || service.request.ActorID != actorID || service.request.IdempotencyKey != "submit-1" {
		t.Fatalf("submit request: %+v", service.request)
	}
}

type modernSubmissionStub struct {
	request appsubmission.Request
	result  appsubmission.Result
}

func (s *modernSubmissionStub) Submit(_ context.Context, request appsubmission.Request) (appsubmission.Result, error) {
	s.request = request
	return s.result, nil
}

var _ ModernSubmissionService = (*modernSubmissionStub)(nil)
