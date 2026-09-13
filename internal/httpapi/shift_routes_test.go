package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	appdb "github.com/pomkita/pomkita-be/internal/db"
)

const testUUID = "11111111-1111-4111-8111-111111111111"

type shiftServiceStub struct{}

func (shiftServiceStub) OpenShift(context.Context, string, uuid.UUID, uuid.UUID, time.Time, bool, *string, *string, *int) (appdb.OpenShiftResult, error) {
	return appdb.OpenShiftResult{ShiftID: uuid.MustParse(testUUID), StationSeq: "1", BusinessDate: "2026-01-02", ShiftPriceMapSnapshot: json.RawMessage(`{"hash_version":1,"items":[]}`), ShiftPriceMapHash: "aa"}, nil
}
func (shiftServiceStub) ClaimDraft(context.Context, string, uuid.UUID) (appdb.ClaimDraftResult, error) {
	return appdb.ClaimDraftResult{DraftID: uuid.MustParse(testUUID), ClaimToken: uuid.MustParse("22222222-2222-4222-8222-222222222222"), ClaimExpiresAt: "2026-01-01T00:00:00.000000Z", Revision: 2}, nil
}
func (shiftServiceStub) HeartbeatDraft(context.Context, string, uuid.UUID, uuid.UUID) (bool, error) {
	return true, nil
}
func (shiftServiceStub) WriteDraftReading(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID, int, string, string) (int, error) {
	return 3, nil
}
func (shiftServiceStub) WriteDraftSales(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID, int, string, string) (int, error) {
	return 4, nil
}
func (shiftServiceStub) WriteDraftLoss(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID, int, string, string, string, string, string) (int, error) {
	return 5, nil
}
func (shiftServiceStub) StageDraftEvidence(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID, int, string, string, []byte, int64, string) (int, error) {
	return 6, nil
}
func (shiftServiceStub) ReadDraft(context.Context, string, uuid.UUID) (json.RawMessage, error) {
	return json.RawMessage(`{"draft_id":"` + testUUID + `","shift_id":"` + testUUID + `","status":"editing","revision":2,"readings":[],"sales":[],"losses":[]}`), nil
}
func (shiftServiceStub) ReadShiftList(context.Context, string, *uuid.UUID) ([]json.RawMessage, error) {
	return []json.RawMessage{json.RawMessage(`{"shift_id":"` + testUUID + `","station_id":"` + testUUID + `","station_seq":"1","business_date":"2026-01-02","status":"open","current_report_id":null}`)}, nil
}
func (shiftServiceStub) ReadShiftDetail(context.Context, string, uuid.UUID) (json.RawMessage, error) {
	return json.RawMessage(`{"shift_id":"` + testUUID + `","station_id":"` + testUUID + `","station_seq":"1","opened_at":"2026-01-01T00:00:00.000000Z","business_date":"2026-01-02","status":"open","current_report_id":null,"draft":{"draft_id":"` + testUUID + `","status":"editing","revision":2,"recovery_count":0}}`), nil
}
func (shiftServiceStub) ReadReport(context.Context, string, uuid.UUID) (json.RawMessage, error) {
	return json.RawMessage(`{"report_id":"` + testUUID + `","shift_id":"` + testUUID + `","version_no":1,"status":"submitted","submitted_at":"2026-01-01T00:00:00.000000Z","readings":[],"sales":[],"losses":[]}`), nil
}
func (shiftServiceStub) SubmitShift(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID, int, string, json.RawMessage) (appdb.SubmitShiftResult, error) {
	return appdb.SubmitShiftResult{ReportID: uuid.MustParse(testUUID), Replay: false, RequestHash: "bb"}, nil
}

func TestShiftAPI_RequiresAuthenticationOnEveryEndpoint(t *testing.T) {
	router := NewRouterWithDependencies("test", nil, readyStub{}, nil)
	requests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/shift/open"},
		{http.MethodPost, "/draft/claim"},
		{http.MethodPost, "/draft/heartbeat"},
		{http.MethodPost, "/draft/reading"},
		{http.MethodPost, "/draft/sales"},
		{http.MethodPost, "/draft/loss"},
		{http.MethodPost, "/draft/evidence"},
		{http.MethodGet, "/draft?shift_id=11111111-1111-4111-8111-111111111111"},
		{http.MethodPost, "/shift/submit"},
		{http.MethodGet, "/shifts"},
		{http.MethodGet, "/shifts/11111111-1111-4111-8111-111111111111"},
		{http.MethodGet, "/report/11111111-1111-4111-8111-111111111111"},
	}
	for _, item := range requests {
		t.Run(item.method+" "+item.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(item.method, item.path, nil))
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status: got %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
			}
		})
	}
}

func TestShiftAPI_RejectsMalformedMutationJSON(t *testing.T) {
	router := NewRouterWithAllDependencies("test", nil, readyStub{}, verifierStub{}, nil, nil)
	request := httptest.NewRequest(http.MethodPost, "/draft/reading", strings.NewReader("{"))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestShiftAPI_HappyPathReturnsProcedureShapes(t *testing.T) {
	router := NewRouterWithAllDependencies("test", nil, readyStub{}, verifierStub{}, nil, shiftServiceStub{})
	validID := testUUID
	cases := []struct{ name, method, path, body, want string }{
		{"open", http.MethodPost, "/shift/open", `{"station_id":"` + validID + `","opened_at":"2026-01-01T00:00:00Z","backfilled":false}`, `"station_seq":"1"`},
		{"claim", http.MethodPost, "/draft/claim", `{"shift_id":"` + validID + `"}`, `"claim_token"`},
		{"heartbeat", http.MethodPost, "/draft/heartbeat", `{"draft_id":"` + validID + `","claim_token":"22222222-2222-4222-8222-222222222222"}`, `"renewed":true`},
		{"reading", http.MethodPost, "/draft/reading", `{"draft_id":"` + validID + `","claim_token":"22222222-2222-4222-8222-222222222222","revision":2,"nozzle_id":"` + validID + `","meter_start":"1.0","meter_end":"2.0"}`, `"revision":3`},
		{"sales", http.MethodPost, "/draft/sales", `{"draft_id":"` + validID + `","claim_token":"22222222-2222-4222-8222-222222222222","revision":2,"dispenser_id":"` + validID + `","cash_amount":"1000","cashless_amount":"0"}`, `"revision":4`},
		{"loss", http.MethodPost, "/draft/loss", `{"draft_id":"` + validID + `","claim_token":"22222222-2222-4222-8222-222222222222","revision":2,"loss_id":"` + validID + `","direction":"loss","reason_code":"test","liters":"1.00","cash_amount":"","note":"note"}`, `"revision":5`},
		{"evidence", http.MethodPost, "/draft/evidence", `{"draft_id":"` + validID + `","claim_token":"22222222-2222-4222-8222-222222222222","revision":2,"loss_row_id":"` + validID + `","evidence_type":"photo","object_key":"key","content_hash":"0000000000000000000000000000000000000000000000000000000000000000","size_bytes":10,"mime":"image/jpeg"}`, `"revision":6`},
		{"draft", http.MethodGet, "/draft?shift_id=" + validID, ``, `"draft_id"`},
		{"submit", http.MethodPost, "/shift/submit", `{"shift_id":"` + validID + `","draft_id":"` + validID + `","claim_token":"22222222-2222-4222-8222-222222222222","revision":2,"hash_version":1,"readings":[],"sales":[],"losses":[]}`, `"report_id"`},
		{"shift list", http.MethodGet, "/shifts", ``, `"station_seq"`},
		{"shift detail", http.MethodGet, "/shifts/" + validID, ``, `"current_report_id"`},
		{"report", http.MethodGet, "/report/" + validID, ``, `"submitted_at"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Authorization", "Bearer token")
			if tc.method == http.MethodPost {
				request.Header.Set("X-Requested-With", "XMLHttpRequest")
			}
			if tc.name == "submit" {
				request.Header.Set("Idempotency-Key", "test-key")
			}
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), tc.want) {
				t.Fatalf("response: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestShiftAPI_RejectsInvalidAuthenticationOnEveryEndpoint(t *testing.T) {
	router := NewRouterWithAllDependencies("test", nil, readyStub{}, verifierStub{err: errors.New("bad token")}, nil, shiftServiceStub{})
	for _, item := range []struct{ method, path string }{
		{http.MethodPost, "/shift/open"}, {http.MethodPost, "/draft/claim"}, {http.MethodPost, "/draft/heartbeat"},
		{http.MethodPost, "/draft/reading"}, {http.MethodPost, "/draft/sales"}, {http.MethodPost, "/draft/loss"},
		{http.MethodPost, "/draft/evidence"}, {http.MethodGet, "/draft?shift_id=" + testUUID}, {http.MethodPost, "/shift/submit"},
		{http.MethodGet, "/shifts"}, {http.MethodGet, "/shifts/" + testUUID}, {http.MethodGet, "/report/" + testUUID},
	} {
		t.Run(item.method+" "+item.path, func(t *testing.T) {
			request := httptest.NewRequest(item.method, item.path, nil)
			request.Header.Set("Authorization", "Bearer invalid")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status: got %d, want 401", recorder.Code)
			}
		})
	}
}

func TestShiftAPI_RequiresCSRFOnEveryMutation(t *testing.T) {
	router := NewRouterWithAllDependencies("test", nil, readyStub{}, verifierStub{}, nil, shiftServiceStub{})
	for _, path := range []string{"/shift/open", "/draft/claim", "/draft/heartbeat", "/draft/reading", "/draft/sales", "/draft/loss", "/draft/evidence", "/shift/submit"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, path, nil)
			request.Header.Set("Authorization", "Bearer token")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"csrf_required"`) {
				t.Fatalf("response: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

type submitErrorShiftService struct {
	shiftServiceStub
	err error
}

func (s submitErrorShiftService) SubmitShift(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID, int, string, json.RawMessage) (appdb.SubmitShiftResult, error) {
	return appdb.SubmitShiftResult{}, s.err
}

func TestShiftAPI_MapsRolloverValidationToAFieldError(t *testing.T) {
	router := NewRouterWithAllDependencies("test", nil, readyStub{}, verifierStub{}, nil, submitErrorShiftService{err: &pgconn.PgError{Code: "23514", Message: "rollover_over_threshold"}})
	request := httptest.NewRequest(http.MethodPost, "/shift/submit", strings.NewReader(`{"shift_id":"`+testUUID+`","draft_id":"`+testUUID+`","claim_token":"22222222-2222-4222-8222-222222222222","revision":1,"hash_version":1,"readings":[],"sales":[],"losses":[]}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	request.Header.Set("Idempotency-Key", "rollover-test")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), `"field_errors"`) {
		t.Fatalf("response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

type emptyShiftListService struct{ shiftServiceStub }

func (emptyShiftListService) ReadShiftList(context.Context, string, *uuid.UUID) ([]json.RawMessage, error) {
	return nil, nil
}

func TestShiftAPI_EmptyShiftListIsAnArray(t *testing.T) {
	router := NewRouterWithAllDependencies("test", nil, readyStub{}, verifierStub{}, nil, emptyShiftListService{})
	request := httptest.NewRequest(http.MethodGet, "/shifts", nil)
	request.Header.Set("Authorization", "Bearer token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != "[]" {
		t.Fatalf("response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestTempDecode(t *testing.T) {
	var loss draftLossRequest
	err := json.Unmarshal([]byte(`{"draft_id":"`+testUUID+`","claim_token":"22222222-2222-4222-8222-222222222222","revision":2,"loss_id":"`+testUUID+`","direction":"loss","reason_code":"test","liters":"1.00","cash_amount":"","note":"note"}`), &loss)
	t.Logf("loss=%#v err=%v valid=%v", loss, err, validDecimal(loss.Liters))
	var evidence draftEvidenceRequest
	err = json.Unmarshal([]byte(`{"draft_id":"`+testUUID+`","claim_token":"22222222-2222-4222-8222-222222222222","revision":2,"loss_row_id":"`+testUUID+`","evidence_type":"photo","object_key":"key","content_hash":"0000000000000000000000000000000000000000000000000000000000000000","size_bytes":10,"mime":"image/jpeg"}`), &evidence)
	t.Logf("evidence=%#v err=%v", evidence, err)
}
