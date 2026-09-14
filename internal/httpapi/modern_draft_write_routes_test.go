package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	appdraft "github.com/pomkita/pomkita-be/internal/service/draft"
)

func TestModernDraftHeartbeatRoute_UsesVerifiedStationScope(t *testing.T) {
	orgID, stationID, actorID := uuid.New(), uuid.New(), uuid.New()
	service := &modernDraftWriteStub{}
	sessions := &modernSessionStub{view: SessionView{OrgID: orgID, UserID: actorID, Roles: []string{"Supervisor"}, StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{
		Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions,
		ModernDraftWrites: service, LatestMigration: 11,
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

type modernDraftWriteStub struct {
	heartbeat appdraft.HeartbeatRequest
}

func (s *modernDraftWriteStub) Heartbeat(_ context.Context, request appdraft.HeartbeatRequest) error {
	s.heartbeat = request
	return nil
}

func (s *modernDraftWriteStub) WriteReading(context.Context, appdraft.WriteReadingRequest) (int, error) {
	return 0, nil
}

func (s *modernDraftWriteStub) WriteSales(context.Context, appdraft.WriteSalesRequest) (int, error) {
	return 0, nil
}

func (s *modernDraftWriteStub) WriteLoss(context.Context, appdraft.WriteLossRequest) (int, error) {
	return 0, nil
}

func (s *modernDraftWriteStub) StageEvidence(context.Context, appdraft.StageEvidenceRequest) (int, error) {
	return 0, nil
}

var _ ModernDraftWriteService = (*modernDraftWriteStub)(nil)
