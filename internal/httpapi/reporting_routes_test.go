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
	"github.com/jackc/pgx/v5/pgconn"
)

type reportingServiceStub struct{}

func (reportingServiceStub) ReadReportPrintout(context.Context, string, uuid.UUID) (json.RawMessage, error) {
	return json.RawMessage(`{"report_id":"11111111-1111-4111-8111-111111111111"}`), nil
}

func (reportingServiceStub) ReadAnomalyExport(context.Context, string) ([]AnomalyExportRow, error) {
	return []AnomalyExportRow{{Kind: "variance", SourceID: "event-1", VarianceRupiah: "-20"}}, nil
}

func (reportingServiceStub) ReadAuditExport(context.Context, string) ([]AuditExportRow, error) {
	return []AuditExportRow{{EventID: "event-1", OrgSequence: "1", Payload: json.RawMessage(`{"ok":true}`)}}, nil
}

func (reportingServiceStub) VerifyAuditChain(context.Context, string) (AuditVerifyResult, error) {
	return AuditVerifyResult{Verified: true, OrgSequence: "1", Result: "chain_verified"}, nil
}

func (reportingServiceStub) ReadPolicyHistory(context.Context, string) ([]json.RawMessage, error) {
	return []json.RawMessage{json.RawMessage(`{"policy_kind":"threshold"}`)}, nil
}

type reportingErrorStub struct{ err error }

func (s reportingErrorStub) ReadReportPrintout(context.Context, string, uuid.UUID) (json.RawMessage, error) {
	return nil, s.err
}
func (s reportingErrorStub) ReadAnomalyExport(context.Context, string) ([]AnomalyExportRow, error) {
	return nil, s.err
}
func (s reportingErrorStub) ReadAuditExport(context.Context, string) ([]AuditExportRow, error) {
	return nil, s.err
}
func (s reportingErrorStub) VerifyAuditChain(context.Context, string) (AuditVerifyResult, error) {
	return AuditVerifyResult{}, s.err
}
func (s reportingErrorStub) ReadPolicyHistory(context.Context, string) ([]json.RawMessage, error) {
	return nil, s.err
}

type timestampReportingStub struct{}

func (timestampReportingStub) ReadReportPrintout(context.Context, string, uuid.UUID) (json.RawMessage, error) {
	return json.RawMessage(`{"report_id":"11111111-1111-4111-8111-111111111111","submitted_at":"2026-01-01T00:00:00+00:00"}`), nil
}
func (timestampReportingStub) ReadAnomalyExport(context.Context, string) ([]AnomalyExportRow, error) {
	return nil, nil
}
func (timestampReportingStub) ReadAuditExport(context.Context, string) ([]AuditExportRow, error) {
	return nil, nil
}
func (timestampReportingStub) VerifyAuditChain(context.Context, string) (AuditVerifyResult, error) {
	return AuditVerifyResult{}, nil
}
func (timestampReportingStub) ReadPolicyHistory(context.Context, string) ([]json.RawMessage, error) {
	return nil, nil
}

type reportingEqualityShiftStub struct{ shiftServiceStub }

func (reportingEqualityShiftStub) ReadReport(context.Context, string, uuid.UUID) (json.RawMessage, error) {
	return json.RawMessage(`{"report_id":"11111111-1111-4111-8111-111111111111","submitted_at":"2026-01-01T00:00:00Z"}`), nil
}

func TestReportingAPI_RequiresAuthenticationOnEveryEndpoint(t *testing.T) {
	router := NewRouterWithDependencies("test", nil, readyStub{current: true}, nil)
	for _, path := range []string{
		"/report/11111111-1111-4111-8111-111111111111/printout",
		"/anomalies/export",
		"/audit/export",
		"/audit/verify",
		"/policy/history",
	} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status: got %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
			}
		})
	}
}

func TestReportingAPI_ReturnsExpectedResponseShapes(t *testing.T) {
	router := NewRouterWithAllDependencies("test", nil, readyStub{current: true}, verifierStub{}, nil, nil, reportingServiceStub{})
	request := func(path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer token")
		router.ServeHTTP(recorder, req)
		return recorder
	}

	printout := request("/report/11111111-1111-4111-8111-111111111111/printout")
	if printout.Code != http.StatusOK || printout.Header().Get("Content-Type") != "application/json; charset=utf-8" || !strings.Contains(printout.Body.String(), `"report_id"`) {
		t.Fatalf("printout: status=%d content-type=%q body=%s", printout.Code, printout.Header().Get("Content-Type"), printout.Body)
	}
	anomalies := request("/anomalies/export")
	if anomalies.Code != http.StatusOK || !strings.HasPrefix(anomalies.Body.String(), "kind,source,source_id") || !strings.Contains(anomalies.Body.String(), "variance") {
		t.Fatalf("anomaly export: status=%d body=%s", anomalies.Code, anomalies.Body)
	}
	audit := request("/audit/export")
	if audit.Code != http.StatusOK || !strings.HasPrefix(audit.Body.String(), "event_id,org_sequence,event_type") || !strings.Contains(audit.Body.String(), `{""ok"":true}`) {
		t.Fatalf("audit export: status=%d body=%s", audit.Code, audit.Body)
	}
	verify := request("/audit/verify")
	if verify.Code != http.StatusOK || !strings.Contains(verify.Body.String(), `"verified":true`) || !strings.Contains(verify.Body.String(), `"org_sequence":"1"`) {
		t.Fatalf("audit verify: status=%d body=%s", verify.Code, verify.Body)
	}
	policy := request("/policy/history")
	if policy.Code != http.StatusOK || !strings.Contains(policy.Body.String(), `"policy_kind":"threshold"`) {
		t.Fatalf("policy history: status=%d body=%s", policy.Code, policy.Body)
	}
}

func TestReportingAPI_PrintoutMatchesReportScreenBytes(t *testing.T) {
	router := NewRouterWithAllDependencies("test", nil, readyStub{current: true}, verifierStub{}, nil, reportingEqualityShiftStub{}, timestampReportingStub{})
	request := func(path string) []byte {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer token")
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s: status=%d body=%s", path, recorder.Code, recorder.Body)
		}
		return recorder.Body.Bytes()
	}

	report := request("/report/11111111-1111-4111-8111-111111111111")
	printout := request("/report/11111111-1111-4111-8111-111111111111/printout")
	if string(printout) != string(report) {
		t.Fatalf("printout bytes differ: report=%s printout=%s", report, printout)
	}
}

func TestReportingAPI_MapsProcedureAuthorizationFailureToForbidden(t *testing.T) {
	router := NewRouterWithAllDependencies("test", nil, readyStub{current: true}, verifierStub{}, nil, nil,
		reportingErrorStub{err: errors.New("procedure failure")})
	// Use a PostgreSQL authorization error so the test proves the HTTP mapping.
	service := reportingErrorStub{err: &pgconn.PgError{Code: "42501", Message: "report_role_required"}}
	router = NewRouterWithAllDependencies("test", nil, readyStub{current: true}, verifierStub{}, nil, nil, service)
	for _, path := range []string{
		"/report/11111111-1111-4111-8111-111111111111/printout",
		"/anomalies/export",
		"/audit/export",
		"/audit/verify",
		"/policy/history",
	} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer token")
			router.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status=%d body=%s, want 403", recorder.Code, recorder.Body)
			}
		})
	}
}
