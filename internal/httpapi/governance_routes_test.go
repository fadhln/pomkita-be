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

func (governanceServiceStub) RequestAmendment(context.Context, string, uuid.UUID, uuid.UUID, string, []appdb.AmendmentItem) (appdb.RequestAmendmentResult, error) {
	return appdb.RequestAmendmentResult{AmendmentID: uuid.MustParse(testUUID), BaseReportID: uuid.MustParse(testUUID), Status: "pending", StaleCheckHash: "00", RequestedAt: "2026-01-01T00:00:00.000000Z"}, nil
}

func (governanceServiceStub) RejectAmendment(context.Context, string, uuid.UUID, string) (appdb.RejectAmendmentResult, error) {
	return appdb.RejectAmendmentResult{AmendmentID: uuid.MustParse(testUUID), Status: "rejected", RejectionReason: "not enough evidence", DecidedAt: "2026-01-01T00:00:00.000000Z"}, nil
}

func (governanceServiceStub) ReadAmendmentQueue(context.Context, string) ([]appdb.AmendmentQueueEntry, error) {
	return []appdb.AmendmentQueueEntry{{AmendmentID: uuid.MustParse(testUUID), Status: "pending", Reason: "cash correction", Items: []appdb.AmendmentQueueItem{}}}, nil
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
		{http.MethodPost, "/amendment/request"},
		{http.MethodPost, "/amendment/reject"},
		{http.MethodGet, "/anomalies"},
		{http.MethodGet, "/amendments"},
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
	requestAmendment := request(http.MethodPost, "/amendment/request", `{"shift_id":"`+testUUID+`","base_report_id":"`+testUUID+`","reason":"cash correction","items":[{"target_kind":"sales_declared","target_logical_id":"`+testUUID+`","field":"cash_amount","old_value":"2000","new_value":"2500"}]}`)
	if requestAmendment.Code != http.StatusOK || !strings.Contains(requestAmendment.Body.String(), `"status":"pending"`) {
		t.Fatalf("request amendment: status=%d body=%s", requestAmendment.Code, requestAmendment.Body)
	}
	reject := request(http.MethodPost, "/amendment/reject", `{"amendment_id":"`+testUUID+`","rejection_reason":"not enough evidence"}`)
	if reject.Code != http.StatusOK || !strings.Contains(reject.Body.String(), `"status":"rejected"`) {
		t.Fatalf("reject amendment: status=%d body=%s", reject.Code, reject.Body)
	}
	queue := request(http.MethodGet, "/amendments", "")
	if queue.Code != http.StatusOK || !strings.Contains(queue.Body.String(), `"reason":"cash correction"`) {
		t.Fatalf("amendment queue: status=%d body=%s", queue.Code, queue.Body)
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
		{http.MethodPost, "/amendment/request"},
		{http.MethodPost, "/amendment/reject"},
		{http.MethodGet, "/anomalies"},
		{http.MethodGet, "/amendments"},
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
	for _, path := range []string{"/shift/ack", "/amendment/approve", "/amendment/request", "/amendment/reject"} {
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
