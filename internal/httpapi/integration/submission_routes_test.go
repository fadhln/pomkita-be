package httpapi_test

import (
	"context"
	. "github.com/pomkita/pomkita-be/internal/httpapi"
	submissionapi "github.com/pomkita/pomkita-be/internal/httpapi/submission"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	appsubmission "github.com/pomkita/pomkita-be/internal/service/submission"
)

func TestSubmitRoute_UsesSessionScopeAndIdempotencyHeader(t *testing.T) {
	orgID, stationID, actorID := uuid.New(), uuid.New(), uuid.New()
	service := &SubmissionStub{result: appsubmission.Result{ReportID: uuid.New()}}
	sessions := &SessionStub{view: SessionView{OrgID: orgID, UserID: actorID, StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, Submission: service, LatestMigration: 11})
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

type SubmissionStub struct {
	request appsubmission.Request
	result  appsubmission.Result
}

func (s *SubmissionStub) Submit(_ context.Context, request appsubmission.Request) (appsubmission.Result, error) {
	s.request = request
	return s.result, nil
}

var _ submissionapi.SubmissionService = (*SubmissionStub)(nil)
