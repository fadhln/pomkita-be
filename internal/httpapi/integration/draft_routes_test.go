package httpapi_test

import (
	"context"
	. "github.com/pomkita/pomkita-be/internal/httpapi"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	appdraft "github.com/pomkita/pomkita-be/internal/service/draft"
)

func TestClaimDraftRoute_UsesVerifiedActorScope(t *testing.T) {
	orgID, stationID, actorID, shiftID, draftID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	service := &DraftStub{result: appdraft.ClaimResult{DraftID: draftID, ClaimToken: uuid.New(), ClaimExpiresAt: time.Date(2026, 9, 14, 4, 0, 0, 0, time.UTC), Revision: 2}}
	sessions := &SessionStub{view: SessionView{OrgID: orgID, UserID: actorID, StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, Draft: service, LatestMigration: 11})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/drafts/claim", strings.NewReader(`{"station_id":"`+stationID.String()+`","shift_id":"`+shiftID.String()+`","draft_id":"`+draftID.String()+`"}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.request.ActorID != actorID || service.request.DraftID != draftID || service.request.ShiftID != shiftID {
		t.Fatalf("claim request: %+v", service.request)
	}
}

type DraftStub struct {
	request appdraft.ClaimRequest
	result  appdraft.ClaimResult
}

func (s *DraftStub) Claim(_ context.Context, request appdraft.ClaimRequest) (appdraft.ClaimResult, error) {
	s.request = request
	return s.result, nil
}

var _ DraftService = (*DraftStub)(nil)
