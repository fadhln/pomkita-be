package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"
	draftapi "github.com/pomkita/pomkita-be/internal/httpapi/draft"
	governanceapi "github.com/pomkita/pomkita-be/internal/httpapi/governance"
	policyapi "github.com/pomkita/pomkita-be/internal/httpapi/policy"
	sessionapi "github.com/pomkita/pomkita-be/internal/httpapi/session"
	shiftapi "github.com/pomkita/pomkita-be/internal/httpapi/shift"
	submissionapi "github.com/pomkita/pomkita-be/internal/httpapi/submission"
	usersapi "github.com/pomkita/pomkita-be/internal/httpapi/users"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	appgovernance "github.com/pomkita/pomkita-be/internal/service/governance"
	apppolicy "github.com/pomkita/pomkita-be/internal/service/policy"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
	appshift "github.com/pomkita/pomkita-be/internal/service/shift"
	appsubmission "github.com/pomkita/pomkita-be/internal/service/submission"
)

type openAPIEmptyInput struct{}
type openAPIEmptyOutput struct{}

type openAPIIDInput struct {
	ID string `path:"id" format:"uuid"`
}

type openAPIStationInput struct {
	StationID string `query:"station_id" format:"uuid"`
}

type openAPIIDStationInput struct {
	ID        string `path:"id" format:"uuid"`
	StationID string `query:"station_id" format:"uuid"`
}

type openAPIBodyInput[T any] struct {
	Body T
}

type openAPIIDBodyInput[T any] struct {
	ID   string `path:"id" format:"uuid"`
	Body T
}

type openAPIHeaderBodyInput[T any] struct {
	IdempotencyKey string `header:"Idempotency-Key" required:"true"`
	Body           T
}

type openAPIStatusOutput struct {
	Body struct {
		Status string `json:"status"`
	}
}

type openAPIVerifiedOutput struct {
	Body struct {
		Verified bool `json:"verified"`
	}
}

// OpenAPIDocument builds the public contract from typed Go operation definitions.
func OpenAPIDocument() *huma.OpenAPI {
	gin.SetMode(gin.ReleaseMode)
	config := huma.DefaultConfig("PomKita API", "1.0.0")
	config.OpenAPI.OpenAPI = "3.0.3"
	config.OpenAPIPath = ""
	config.DocsPath = ""
	config.SchemasPath = ""
	config.OpenAPI.Extensions = map[string]any{"x-generated-by": "huma"}
	if config.OpenAPI.Components == nil {
		config.OpenAPI.Components = &huma.Components{}
	}
	config.OpenAPI.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"sessionCookie": {Type: "apiKey", In: "cookie", Name: "pomkita_session", Description: "httpOnly session cookie"},
		"bearerAuth":    {Type: "http", Scheme: "bearer", BearerFormat: "JWT"},
	}

	api := humagin.New(gin.New(), config)
	registerOpenAPIOperations(api)
	decorateIdentityContract(api.OpenAPI())
	setCSVResponse(api, "/api/v1/anomalies/export")
	setCSVResponse(api, "/api/v1/audit/export")
	return api.OpenAPI()
}

func decorateIdentityContract(document *huma.OpenAPI) {
	if schema := document.Components.Schemas.Map()["ErrorModel"]; schema != nil {
		if schema.Properties == nil {
			schema.Properties = map[string]*huma.Schema{}
		}
		schema.Properties["code"] = &huma.Schema{Type: "string", Description: "Stable machine error code"}
		schema.Properties["message"] = &huma.Schema{Type: "string", Description: "Safe user message"}
		schema.Properties["request_id"] = &huma.Schema{Type: "string"}
		schema.Properties["field_errors"] = &huma.Schema{Type: "object", AdditionalProperties: &huma.Schema{Type: "string"}}
	}
	identity := map[string]struct {
		roles   []string
		errors  map[string][]string
		example any
	}{
		"/api/v1/login":                   {errors: map[string][]string{"401": {"invalid_credentials"}}, example: map[string]any{"username": "demo-owner", "password": "demo-password"}},
		"/api/v1/session":                 {roles: []string{"Operator", "Supervisor", "Station Admin", "Owner", "Superadmin"}},
		"/api/v1/users":                   {roles: []string{"Owner", "Superadmin"}, errors: map[string][]string{"403": {"user_administration_forbidden", "organization_scope_forbidden"}, "409": {"email_conflict", "username_conflict"}, "422": {"organization_has_no_station", "invalid_identity_request"}}, example: map[string]any{"email": "operator@example.test", "display_name": "Operator", "role": "Operator", "station_id": "22222222-2222-4222-8222-222222222222"}},
		"/api/v1/auth/invitations/accept": {errors: map[string][]string{"400": {"INVALID_TOKEN"}, "422": {"invalid_username", "weak_password"}}, example: map[string]any{"token": "raw-token-from-email", "username": "operator-1", "password": "strong-password", "display_name": "Operator"}},
	}
	for path, metadata := range identity {
		item := document.Paths[path]
		if item == nil || item.Post == nil && path != "/api/v1/session" {
			if path == "/api/v1/session" && item != nil && item.Get != nil {
				decorateOperation(item.Get, metadata.roles, metadata.errors, nil, path == "/api/v1/login")
			}
			continue
		}
		if item.Post != nil {
			decorateOperation(item.Post, metadata.roles, metadata.errors, metadata.example, path == "/api/v1/login")
		}
	}
}

func decorateOperation(operation *huma.Operation, roles []string, errorsByStatus map[string][]string, example any, csrf bool) {
	if operation.Extensions == nil {
		operation.Extensions = map[string]any{}
	}
	if len(roles) > 0 {
		operation.Extensions["x-permitted-roles"] = roles
	}
	if csrf {
		operation.Extensions["x-csrf-required"] = true
	}
	if len(errorsByStatus) > 0 {
		operation.Extensions["x-stable-error-codes"] = errorsByStatus
	}
	if example != nil && operation.RequestBody != nil && operation.RequestBody.Content["application/json"] != nil {
		operation.RequestBody.Content["application/json"].Examples = map[string]*huma.Example{"example": {Summary: "Request example", Value: example}}
	}
	if response := operation.Responses["400"]; response != nil && response.Content["application/problem+json"] != nil {
		response.Content["application/problem+json"].Examples = map[string]*huma.Example{"example": {Summary: "Error example", Value: map[string]any{"type": "about:blank", "title": "Bad Request", "status": 400, "detail": "Request failed", "code": "INVALID_TOKEN", "request_id": "request-id", "field_errors": nil}}}
	}
}

func registerOpenAPIOperations(api huma.API) {
	registerOpenAPIOperation[openAPIEmptyInput, openAPIStatusOutput](api, http.MethodGet, "/health", "health", "Health check")
	registerOpenAPIOperation[openAPIEmptyInput, openAPIStatusOutput](api, http.MethodGet, "/ready", "ready", "Readiness check")
	registerOpenAPIOperation[openAPIBodyInput[sessionapi.LoginRequest], openAPIEmptyOutput](api, http.MethodPost, "/api/v1/login", "login", "Create a session")
	registerOpenAPIOperation[openAPIEmptyInput, openAPIEmptyOutput](api, http.MethodDelete, "/api/v1/logout", "logout", "Revoke a session")
	registerOpenAPIOperation[openAPIEmptyInput, openAPIOutput[appjwt.SessionView]](api, http.MethodGet, "/api/v1/session", "session", "Read the current session")
	registerOpenAPIOperation[openAPIBodyInput[usersapi.InviteRequest], openAPIEmptyOutput](api, http.MethodPost, "/api/v1/users", "inviteUser", "Invite a user")
	registerOpenAPIOperation[openAPIBodyInput[usersapi.AcceptRequest], openAPIEmptyOutput](api, http.MethodPost, "/api/v1/auth/invitations/accept", "acceptInvitation", "Accept an invitation")

	registerOpenAPIOperation[openAPIStationInput, openAPIOutput[[]appshift.Summary]](api, http.MethodGet, "/api/v1/shifts", "listShifts", "List shifts")
	registerOpenAPIOperation[openAPIBodyInput[shiftapi.OpenShiftRequest], openAPIOutput[appshift.Shift]](api, http.MethodPost, "/api/v1/shifts", "openShift", "Open a shift")
	registerOpenAPIOperation[openAPIIDStationInput, openAPIOutput[appshift.Detail]](api, http.MethodGet, "/api/v1/shifts/{id}", "getShift", "Read shift detail")

	registerOpenAPIOperation[openAPIBodyInput[draftapi.ClaimDraftRequest], openAPIOutput[appdraftClaimResult]](api, http.MethodPost, "/api/v1/drafts/claim", "claimDraft", "Claim a draft")
	registerOpenAPIOperation[openAPIBodyInput[draftapi.DraftLeaseRequest], openAPIEmptyOutput](api, http.MethodPost, "/api/v1/drafts/heartbeat", "heartbeatDraft", "Renew a draft lease")
	registerOpenAPIOperation[openAPIBodyInput[draftapi.DraftReadingRequest], openAPIOutput[openAPIDraftRevision]](api, http.MethodPost, "/api/v1/drafts/readings", "writeDraftReading", "Write a draft reading")
	registerOpenAPIOperation[openAPIBodyInput[draftapi.DraftSalesRequest], openAPIOutput[openAPIDraftRevision]](api, http.MethodPost, "/api/v1/drafts/sales", "writeDraftSales", "Write draft sales")
	registerOpenAPIOperation[openAPIBodyInput[draftapi.DraftLossRequest], openAPIOutput[openAPIDraftRevision]](api, http.MethodPost, "/api/v1/drafts/losses", "writeDraftLoss", "Write a draft loss")
	registerOpenAPIOperation[openAPIBodyInput[draftapi.DraftEvidenceRequest], openAPIOutput[openAPIDraftRevision]](api, http.MethodPost, "/api/v1/drafts/evidence", "stageDraftEvidence", "Stage draft evidence")

	registerOpenAPIOperation[openAPIHeaderBodyInput[submissionapi.SubmitRequest], openAPIOutput[appsubmission.Result]](api, http.MethodPost, "/api/v1/submissions", "submitReport", "Submit a report")
	registerOpenAPIOperation[openAPIIDStationInput, openAPIOutput[appreporting.ReportView]](api, http.MethodGet, "/api/v1/reports/{id}", "getReport", "Read a report")
	registerOpenAPIOperation[openAPIIDStationInput, openAPIOutput[appreporting.ReportView]](api, http.MethodGet, "/api/v1/reports/{id}/printout", "printReport", "Read report printout")
	registerOpenAPIOperation[openAPIIDBodyInput[governanceapi.AcknowledgeRequest], openAPIOutput[appgovernance.Acknowledgement]](api, http.MethodPost, "/api/v1/reports/{id}/acknowledgement", "acknowledgeReport", "Acknowledge a report")

	registerOpenAPIOperation[openAPIEmptyInput, openAPIOutput[[]appgovernance.AmendmentQueueView]](api, http.MethodGet, "/api/v1/amendments", "listAmendmentQueue", "List pending amendments")
	registerOpenAPIOperation[openAPIBodyInput[governanceapi.AmendmentRequest], openAPIOutput[appgovernance.Amendment]](api, http.MethodPost, "/api/v1/amendments", "requestAmendment", "Request an amendment")
	registerOpenAPIOperation[openAPIIDBodyInput[governanceapi.AmendmentDecisionRequest], openAPIOutput[appgovernance.Amendment]](api, http.MethodPost, "/api/v1/amendments/{id}/approve", "approveAmendment", "Approve an amendment")
	registerOpenAPIOperation[openAPIIDBodyInput[governanceapi.AmendmentDecisionRequest], openAPIEmptyOutput](api, http.MethodPost, "/api/v1/amendments/{id}/reject", "rejectAmendment", "Reject an amendment")

	registerOpenAPIOperation[openAPIBodyInput[policyapi.PolicyRevisionRequest], openAPIOutput[apppolicy.PolicyRevision]](api, http.MethodPost, "/api/v1/policies/revisions", "createPolicyRevision", "Create a policy revision")
	registerOpenAPIOperation[openAPIStationInput, openAPIOutput[[]apppolicy.RevisionView]](api, http.MethodGet, "/api/v1/policies/history", "policyHistory", "Read policy history")

	registerOpenAPIOperation[openAPIStationInput, openAPIOutput[[]appreporting.AnomalyView]](api, http.MethodGet, "/api/v1/anomalies", "anomalies", "Read anomalies")
	registerOpenAPIOperation[openAPIStationInput, openAPIOutput[[]appreporting.AnomalyView]](api, http.MethodGet, "/api/v1/anomalies/export", "exportAnomalies", "Export anomalies")
	registerOpenAPIOperation[openAPIEmptyInput, openAPIOutput[[]appreporting.AuditRow]](api, http.MethodGet, "/api/v1/audit", "auditTrail", "Read the audit trail")
	registerOpenAPIOperation[openAPIEmptyInput, openAPIOutput[[]appreporting.AuditRow]](api, http.MethodGet, "/api/v1/audit/export", "exportAudit", "Export audit events")
	registerOpenAPIOperation[openAPIEmptyInput, openAPIVerifiedOutput](api, http.MethodGet, "/api/v1/audit/verify", "verifyAudit", "Verify the audit chain")
}

type openAPIOutput[T any] struct {
	Body T
}

type appdraftClaimResult struct {
	DraftID      string `json:"draft_id" format:"uuid"`
	ClaimToken   string `json:"claim_token" format:"uuid"`
	ClaimExpires string `json:"claim_expires_at" format:"date-time"`
	Revision     int    `json:"revision"`
}

type openAPIDraftRevision struct {
	Revision int `json:"revision"`
}

func registerOpenAPIOperation[I, O any](api huma.API, method, path, operationID, summary string) {
	security := []map[string][]string(nil)
	if path != "/health" && path != "/ready" && path != "/api/v1/login" && path != "/api/v1/auth/invitations/accept" {
		security = []map[string][]string{{"sessionCookie": {}}, {"bearerAuth": {}}}
	}
	huma.Register(api, huma.Operation{
		Method:      method,
		Path:        path,
		OperationID: operationID,
		Summary:     summary,
		Errors:      []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusUnprocessableEntity},
		Security:    security,
	}, func(context.Context, *I) (*O, error) {
		return new(O), nil
	})
}

func setCSVResponse(api huma.API, path string) {
	response := api.OpenAPI().Paths[path].Get.Responses["200"]
	response.Content = map[string]*huma.MediaType{
		"text/csv": {Schema: &huma.Schema{Type: "string"}},
	}
}
