package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	appdb "github.com/pomkita/pomkita-be/internal/db"
)

type policyServiceStub struct {
	created     appdb.PolicyRevisionResult
	createIn    appdb.CreatePolicyRevisionInput
	tombstone   appdb.PolicyRevisionResult
	tombstoneIn appdb.TombstonePolicyRevisionInput
}

func (s *policyServiceStub) CreatePolicyRevision(_ context.Context, _ string, input appdb.CreatePolicyRevisionInput) (appdb.PolicyRevisionResult, error) {
	s.createIn = input
	return s.created, nil
}

func (s *policyServiceStub) TombstonePolicyRevision(_ context.Context, _ string, input appdb.TombstonePolicyRevisionInput) (appdb.PolicyRevisionResult, error) {
	s.tombstoneIn = input
	return s.tombstone, nil
}

func TestB5PolicyAPI_RequiresAuthenticationAndCSRF(t *testing.T) {
	service := &policyServiceStub{}
	router := NewRouterWithAllDependencies("test", nil, readyStub{}, verifierStub{}, nil, nil, service)
	for _, item := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/policy/revision"},
		{http.MethodPost, "/policy/revision/tombstone"},
	} {
		t.Run(item.method+" "+item.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(item.method, item.path, nil))
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("without session: got %d, want 401", recorder.Code)
			}

			recorder = httptest.NewRecorder()
			request := httptest.NewRequest(item.method, item.path, strings.NewReader(`{}`))
			request.Header.Set("Authorization", "Bearer token")
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"csrf_required"`) {
				t.Fatalf("without CSRF: status=%d body=%s", recorder.Code, recorder.Body)
			}
		})
	}
}

func TestB5PolicyAPI_BindsCreateAndTombstoneBodies(t *testing.T) {
	service := &policyServiceStub{
		created:   appdb.PolicyRevisionResult{RevisionID: uuid.MustParse(testUUID), PolicyKind: "threshold", PolicyID: uuid.MustParse(testUUID), ValidFrom: "2026-09-13T00:00:00.000000Z"},
		tombstone: appdb.PolicyRevisionResult{RevisionID: uuid.MustParse(testUUID), PolicyKind: "threshold", PolicyID: uuid.MustParse(testUUID), Disabled: true},
	}
	router := NewRouterWithAllDependencies("test", nil, readyStub{}, verifierStub{}, nil, nil, service)
	request := func(path, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		router.ServeHTTP(recorder, req)
		return recorder
	}

	created := request("/policy/revision", `{"policy_kind":"threshold","policy_id":"`+testUUID+`","valid_from":"2026-09-13T00:00:00Z","loss_liter_threshold":"10.00","gain_liter_threshold":"11.00","loss_rupiah_threshold":"100","gain_rupiah_threshold":"200","variance_rupiah_threshold":"300","rollover_threshold":"20.0"}`)
	if created.Code != http.StatusOK || !strings.Contains(created.Body.String(), `"revision_id"`) || service.createIn.LossLiterThreshold != "10.00" {
		t.Fatalf("create: status=%d body=%s input=%+v", created.Code, created.Body, service.createIn)
	}
	tombstone := request("/policy/revision/tombstone", `{"policy_kind":"threshold","revision_id":"`+testUUID+`","reason":"Policy is retired"}`)
	if tombstone.Code != http.StatusOK || !strings.Contains(tombstone.Body.String(), `"disabled":true`) || service.tombstoneIn.Reason != "Policy is retired" {
		t.Fatalf("tombstone: status=%d body=%s input=%+v", tombstone.Code, tombstone.Body, service.tombstoneIn)
	}
}

func TestB5PolicyAPI_MapsConflictAndAuthorizationErrors(t *testing.T) {
	service := &policyErrorService{err: &pgconn.PgError{Code: "23505", Message: "policy_revision_overlap"}}
	router := NewRouterWithAllDependencies("test", nil, readyStub{}, verifierStub{}, nil, nil, service)
	req := httptest.NewRequest(http.MethodPost, "/policy/revision", strings.NewReader(`{"policy_kind":"threshold","policy_id":"`+testUUID+`","valid_from":"2026-09-13T00:00:00Z","loss_liter_threshold":"1","gain_liter_threshold":"1","loss_rupiah_threshold":"1","gain_rupiah_threshold":"1","variance_rupiah_threshold":"1","rollover_threshold":"1"}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), `"code":"policy_revision_overlap"`) {
		t.Fatalf("conflict: status=%d body=%s", recorder.Code, recorder.Body)
	}

	service.err = &pgconn.PgError{Code: "42501", Message: "policy_revision_role_required"}
	req = httptest.NewRequest(http.MethodPost, "/policy/revision/tombstone", strings.NewReader(`{"policy_kind":"threshold","revision_id":"`+testUUID+`","reason":"retired"}`))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("authorization: status=%d body=%s", recorder.Code, recorder.Body)
	}
}

type policyErrorService struct{ err error }

func (s policyErrorService) CreatePolicyRevision(context.Context, string, appdb.CreatePolicyRevisionInput) (appdb.PolicyRevisionResult, error) {
	return appdb.PolicyRevisionResult{}, s.err
}

func (s policyErrorService) TombstonePolicyRevision(context.Context, string, appdb.TombstonePolicyRevisionInput) (appdb.PolicyRevisionResult, error) {
	return appdb.PolicyRevisionResult{}, s.err
}
