package httpapi_test

import (
	"context"
	. "github.com/pomkita/pomkita-be/internal/httpapi"
	auditapi "github.com/pomkita/pomkita-be/internal/httpapi/audit"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
)

func TestAuditExportRoute_UsesSessionOrganizationAndStableCSV(t *testing.T) {
	orgID := uuid.New()
	service := &AuditStub{rows: []appreporting.AuditRow{{EventID: uuid.New(), OrgSequence: 1, EventType: "shift_opened", Outcome: "success", CreatedAt: "2026-09-14T03:00:00.000000Z", PrevHash: []byte{1}, RowHash: []byte{2}}}}
	sessions := &SessionStub{view: SessionView{OrgID: orgID, UserID: uuid.New(), Roles: []string{"Owner"}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, Audit: service, LatestMigration: 11})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit/export", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "text/csv; charset=utf-8" {
		t.Fatalf("response: status=%d content-type=%q body=%s", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
	if !strings.HasPrefix(recorder.Body.String(), "event_id,org_sequence,event_type") || service.orgID != orgID {
		t.Fatalf("CSV or scope: org=%s body=%s", service.orgID, recorder.Body.String())
	}
}

func TestAuditExportRoute_RejectsSupervisor(t *testing.T) {
	sessions := &SessionStub{view: SessionView{OrgID: uuid.New(), UserID: uuid.New(), Roles: []string{"Supervisor"}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, Audit: &AuditStub{}, LatestMigration: 11})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit/export", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status: got %d body=%s", recorder.Code, recorder.Body.String())
	}
}

type AuditStub struct {
	orgID uuid.UUID
	rows  []appreporting.AuditRow
}

func (s *AuditStub) ExportAudit(_ context.Context, orgID uuid.UUID) ([]appreporting.AuditRow, error) {
	s.orgID = orgID
	return s.rows, nil
}

var _ auditapi.AuditService = (*AuditStub)(nil)
