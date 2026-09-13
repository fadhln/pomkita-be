package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	appdb "github.com/pomkita/pomkita-be/internal/db"
)

type governanceServiceStub struct{}

func (governanceServiceStub) AckShift(context.Context, string, uuid.UUID, uuid.UUID, int, string, *string, bool, *string) (appdb.AckShiftResult, error) {
	return appdb.AckShiftResult{AckID: uuid.MustParse(testUUID), ReportID: uuid.MustParse(testUUID), VersionNo: 1, Decision: "acked"}, nil
}

func (governanceServiceStub) ApproveAmendment(context.Context, string, uuid.UUID, []byte) (appdb.ApproveAmendmentResult, error) {
	return appdb.ApproveAmendmentResult{AmendmentID: uuid.MustParse(testUUID), AppliedReportID: uuid.MustParse(testUUID), VersionNo: 2}, nil
}

func (governanceServiceStub) ReadGovernanceAnomalies(context.Context, string) ([]json.RawMessage, error) {
	return []json.RawMessage{json.RawMessage(`{"source":"ack_decision","is_break_glass":true,"at":"2026-01-01T00:00:00Z"}`)}, nil
}

func TestGovernanceAPI_RequiresAuthenticationOnEveryEndpoint(t *testing.T) {
	router := NewRouterWithDependencies("test", nil, readyStub{}, nil)
	for _, item := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/shift/ack"},
		{http.MethodPost, "/amendment/approve"},
		{http.MethodGet, "/anomalies"},
	} {
		t.Run(item.method+" "+item.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(item.method, item.path, nil))
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status: got %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
			}
		})
	}
}

func TestGovernanceAPI_ReturnsProcedureResponseShapes(t *testing.T) {
	router := NewRouterWithAllDependencies("test", nil, readyStub{}, verifierStub{}, nil, nil, governanceServiceStub{})
	request := func(method, path, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		router.ServeHTTP(recorder, req)
		return recorder
	}
	ack := request(http.MethodPost, "/shift/ack", `{"shift_id":"`+testUUID+`","report_id":"`+testUUID+`","version_no":1,"decision":"acked"}`)
	if ack.Code != http.StatusOK || !strings.Contains(ack.Body.String(), `"decision":"acked"`) {
		t.Fatalf("ack: status=%d body=%s", ack.Code, ack.Body)
	}
	approve := request(http.MethodPost, "/amendment/approve", `{"amendment_id":"`+testUUID+`","stale_check_hash":"0000000000000000000000000000000000000000000000000000000000000000"}`)
	if approve.Code != http.StatusOK || !strings.Contains(approve.Body.String(), `"version_no":2`) {
		t.Fatalf("approve: status=%d body=%s", approve.Code, approve.Body)
	}
	anomalies := request(http.MethodGet, "/anomalies", "")
	if anomalies.Code != http.StatusOK || !strings.Contains(anomalies.Body.String(), `"source":"ack_decision"`) || !strings.Contains(anomalies.Body.String(), `"at":"2026-01-01T00:00:00.000000Z"`) {
		t.Fatalf("anomalies: status=%d body=%s", anomalies.Code, anomalies.Body)
	}
}

func TestGovernanceAPI_RejectsInvalidAuthenticationOnEveryEndpoint(t *testing.T) {
	router := NewRouterWithAllDependencies("test", nil, readyStub{}, verifierStub{err: errors.New("bad token")}, nil, nil, governanceServiceStub{})
	for _, item := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/shift/ack"},
		{http.MethodPost, "/amendment/approve"},
		{http.MethodGet, "/anomalies"},
	} {
		t.Run(item.method+" "+item.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(item.method, item.path, nil)
			req.Header.Set("Authorization", "Bearer invalid")
			router.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestGovernanceAPI_RequiresCSRFOnMutations(t *testing.T) {
	router := NewRouterWithAllDependencies("test", nil, readyStub{}, verifierStub{}, nil, nil, governanceServiceStub{})
	for _, path := range []string{"/shift/ack", "/amendment/approve"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.Header.Set("Authorization", "Bearer token")
			router.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"csrf_required"`) {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body)
			}
		})
	}
}
