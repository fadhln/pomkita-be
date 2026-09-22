package httpapi_test

import (
	"context"
	. "github.com/fadhln/pomkita-be/internal/httpapi"
	auditapi "github.com/fadhln/pomkita-be/internal/httpapi/audit"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAuditVerifyRoute_UsesSessionOrganization(t *testing.T) {
	orgID := uuid.New()
	service := &AuditVerifyStub{}
	sessions := &SessionStub{view: SessionView{OrgID: orgID, UserID: uuid.New(), Roles: []string{"Owner"}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, AuditVerify: service, LatestMigration: 11})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit/verify", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"verified":true`) || service.orgID != orgID {
		t.Fatalf("response: status=%d body=%s org=%s", recorder.Code, recorder.Body.String(), service.orgID)
	}
}

type AuditVerifyStub struct{ orgID uuid.UUID }

func (s *AuditVerifyStub) Verify(_ context.Context, orgID uuid.UUID) error {
	s.orgID = orgID
	return nil
}

var _ auditapi.AuditVerificationService = (*AuditVerifyStub)(nil)
