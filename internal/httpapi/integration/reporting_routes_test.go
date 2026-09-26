package httpapi_test

import (
	"context"
	. "github.com/fadhln/pomkita-be/internal/httpapi"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appjwt "github.com/fadhln/pomkita-be/internal/jwt"
	appreporting "github.com/fadhln/pomkita-be/internal/service/reporting"
	"github.com/google/uuid"
)

func TestReportRoute_UsesSessionScopeAndKeepsDecimalStrings(t *testing.T) {
	orgID, stationID, reportID := uuid.New(), uuid.New(), uuid.New()
	service := &ReportingStub{view: appreporting.ReportView{ReportID: reportID, StationID: stationID, ShiftID: uuid.New(), VersionNo: 1, Status: "submitted", Sales: []appreporting.SalesView{{CashAmount: "100.00", CashlessAmount: "0"}}}}
	sessions := &SessionStub{view: SessionView{OrgID: orgID, UserID: uuid.New(), StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, Reports: service, LatestMigration: 11})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/reports/"+reportID.String()+"?station_id="+stationID.String(), nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"cash_amount":"100.00"`) || service.orgID != orgID {
		t.Fatalf("report response: status=%d body=%s org=%s", recorder.Code, recorder.Body.String(), service.orgID)
	}
}

func TestPrintoutRoute_UsesTheSameReportView(t *testing.T) {
	orgID, stationID, reportID := uuid.New(), uuid.New(), uuid.New()
	service := &ReportingStub{view: appreporting.ReportView{ReportID: reportID, StationID: stationID, ShiftID: uuid.New(), VersionNo: 1, Status: "submitted", Sales: []appreporting.SalesView{{CashAmount: "100.00", CashlessAmount: "0"}}}}
	sessions := &SessionStub{view: SessionView{OrgID: orgID, UserID: uuid.New(), StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, Reports: service, LatestMigration: 11})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/reports/"+reportID.String()+"/printout?station_id="+stationID.String(), nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"cash_amount":"100.00"`) {
		t.Fatalf("printout response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

type ReportingStub struct {
	view  appreporting.ReportView
	orgID uuid.UUID
}

func (s *ReportingStub) ReadReport(_ context.Context, orgID, _ uuid.UUID, _ uuid.UUID) (appreporting.ReportView, error) {
	s.orgID = orgID
	return s.view, nil
}

type SessionStub struct{ view SessionView }

func (s *SessionStub) Login(context.Context, string, string) (string, appjwt.Claims, error) {
	return "", appjwt.Claims{}, nil
}

func (s *SessionStub) Logout(context.Context, uuid.UUID) error { return nil }

func (s *SessionStub) ReadSession(context.Context, string) (SessionView, error) {
	return s.view, nil
}

func (s *SessionStub) SetActiveContext(context.Context, uuid.UUID, []string, uuid.UUID, uuid.UUID) error {
	return nil
}
