package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestModernAuditVerifyRoute_UsesSessionOrganization(t *testing.T) {
	orgID := uuid.New()
	service := &modernAuditVerifyStub{}
	sessions := &modernSessionStub{view: SessionView{OrgID: orgID, UserID: uuid.New()}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, ModernAuditVerify: service, LatestMigration: 11})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit/verify", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"verified":true`) || service.orgID != orgID {
		t.Fatalf("response: status=%d body=%s org=%s", recorder.Code, recorder.Body.String(), service.orgID)
	}
}

type modernAuditVerifyStub struct{ orgID uuid.UUID }

func (s *modernAuditVerifyStub) Verify(_ context.Context, orgID uuid.UUID) error {
	s.orgID = orgID
	return nil
}

var _ ModernAuditVerificationService = (*modernAuditVerifyStub)(nil)
