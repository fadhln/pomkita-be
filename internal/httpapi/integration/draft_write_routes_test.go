package httpapi_test

import (
	"context"
	. "github.com/fadhln/pomkita-be/internal/httpapi"
	draftapi "github.com/fadhln/pomkita-be/internal/httpapi/draft"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appdraft "github.com/fadhln/pomkita-be/internal/service/draft"
	"github.com/google/uuid"
)

func TestDraftHeartbeatRoute_UsesVerifiedStationScope(t *testing.T) {
	orgID, stationID, actorID := uuid.New(), uuid.New(), uuid.New()
	service := &DraftWriteStub{}
	sessions := &SessionStub{view: SessionView{OrgID: orgID, UserID: actorID, Roles: []string{"Supervisor"}, StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{
		Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions,
		DraftWrites: service, LatestMigration: 11,
	})
	recorder := httptest.NewRecorder()
	draftID, claimToken := uuid.New(), uuid.New()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/drafts/heartbeat", strings.NewReader(`{"station_id":"`+stationID.String()+`","draft_id":"`+draftID.String()+`","claim_token":"`+claimToken.String()+`"}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status: got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.heartbeat.DraftID != draftID || service.heartbeat.ClaimToken != claimToken || service.heartbeat.ActorID != actorID {
		t.Fatalf("heartbeat request: %+v", service.heartbeat)
	}
}

type DraftWriteStub struct {
	heartbeat appdraft.HeartbeatRequest
}

func (s *DraftWriteStub) Heartbeat(_ context.Context, request appdraft.HeartbeatRequest) error {
	s.heartbeat = request
	return nil
}

func (s *DraftWriteStub) WriteReading(context.Context, appdraft.WriteReadingRequest) (int, error) {
	return 0, nil
}

func (s *DraftWriteStub) WriteSales(context.Context, appdraft.WriteSalesRequest) (int, error) {
	return 0, nil
}

func (s *DraftWriteStub) WriteLoss(context.Context, appdraft.WriteLossRequest) (int, error) {
	return 0, nil
}

func (s *DraftWriteStub) StageEvidence(context.Context, appdraft.StageEvidenceRequest) (int, error) {
	return 0, nil
}

var _ draftapi.DraftWriteService = (*DraftWriteStub)(nil)
