package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
)

func TestModernReportRoute_UsesSessionScopeAndKeepsDecimalStrings(t *testing.T) {
	orgID, stationID, reportID := uuid.New(), uuid.New(), uuid.New()
	service := &modernReportingStub{view: appreporting.ReportView{ReportID: reportID, StationID: stationID, ShiftID: uuid.New(), VersionNo: 1, Status: "submitted", Sales: []appreporting.SalesView{{CashAmount: "100.00", CashlessAmount: "0"}}}}
	sessions := &modernSessionStub{view: SessionView{OrgID: orgID, UserID: uuid.New(), StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, Reports: service, LatestMigration: 11})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/reports/"+reportID.String()+"?station_id="+stationID.String(), nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"cash_amount":"100.00"`) || service.orgID != orgID {
		t.Fatalf("report response: status=%d body=%s org=%s", recorder.Code, recorder.Body.String(), service.orgID)
	}
}

func TestModernPrintoutRoute_UsesTheSameReportView(t *testing.T) {
	orgID, stationID, reportID := uuid.New(), uuid.New(), uuid.New()
	service := &modernReportingStub{view: appreporting.ReportView{ReportID: reportID, StationID: stationID, ShiftID: uuid.New(), VersionNo: 1, Status: "submitted", Sales: []appreporting.SalesView{{CashAmount: "100.00", CashlessAmount: "0"}}}}
	sessions := &modernSessionStub{view: SessionView{OrgID: orgID, UserID: uuid.New(), StationIDs: []uuid.UUID{stationID}}}
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{Readiness: readyStub{current: true}, Verifier: verifierStub{}, Sessions: sessions, Reports: service, LatestMigration: 11})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/reports/"+reportID.String()+"/printout?station_id="+stationID.String(), nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"cash_amount":"100.00"`) {
		t.Fatalf("printout response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

type modernReportingStub struct {
	view  appreporting.ReportView
	orgID uuid.UUID
}

func (s *modernReportingStub) ReadReport(_ context.Context, orgID, _ uuid.UUID, _ uuid.UUID) (appreporting.ReportView, error) {
	s.orgID = orgID
	return s.view, nil
}

type modernSessionStub struct{ view SessionView }

func (s *modernSessionStub) Login(context.Context, string, string) (string, appjwt.Claims, error) {
	return "", appjwt.Claims{}, nil
}

func (s *modernSessionStub) Logout(context.Context, uuid.UUID) error { return nil }

func (s *modernSessionStub) ReadSession(context.Context, string) (SessionView, error) {
	return s.view, nil
}
