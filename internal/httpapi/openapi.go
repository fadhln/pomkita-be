package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"
	accountapi "github.com/pomkita/pomkita-be/internal/httpapi/account"
	draftapi "github.com/pomkita/pomkita-be/internal/httpapi/draft"
	governanceapi "github.com/pomkita/pomkita-be/internal/httpapi/governance"
	policyapi "github.com/pomkita/pomkita-be/internal/httpapi/policy"
	sessionapi "github.com/pomkita/pomkita-be/internal/httpapi/session"
	shiftapi "github.com/pomkita/pomkita-be/internal/httpapi/shift"
	submissionapi "github.com/pomkita/pomkita-be/internal/httpapi/submission"
	usersapi "github.com/pomkita/pomkita-be/internal/httpapi/users"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	appaccount "github.com/pomkita/pomkita-be/internal/service/account"
	appgovernance "github.com/pomkita/pomkita-be/internal/service/governance"
	apporganization "github.com/pomkita/pomkita-be/internal/service/organization"
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
		csrf    bool
	}{
		"/api/v1/login":                   {errors: map[string][]string{"401": {"invalid_credentials"}}, example: map[string]any{"username": "demo-owner", "password": "demo-password"}},
		"/api/v1/session":                 {roles: []string{"Operator", "Supervisor", "Station Admin", "Owner", "Superadmin"}},
		"/api/v1/users":                   {roles: []string{"Owner", "Superadmin"}, errors: map[string][]string{"403": {"user_administration_forbidden", "organization_scope_forbidden"}, "409": {"email_conflict", "username_conflict"}, "422": {"organization_has_no_station", "invalid_identity_request"}}, example: map[string]any{"email": "operator@example.test", "display_name": "Operator", "role": "Operator", "station_id": "22222222-2222-4222-8222-222222222222"}},
		"/api/v1/auth/invitations/accept": {errors: map[string][]string{"400": {"INVALID_TOKEN"}, "422": {"invalid_username", "weak_password"}}, example: map[string]any{"token": "raw-token-from-email", "username": "operator-1", "password": "strong-password", "display_name": "Operator"}},
		"/api/v1/account":                 {roles: []string{"Operator", "Supervisor", "Station Admin", "Owner", "Superadmin"}, errors: map[string][]string{"401": {"invalid_session"}}},
		"/api/v1/account/password":        {roles: []string{"Operator", "Supervisor", "Station Admin", "Owner", "Superadmin"}, errors: map[string][]string{"401": {"invalid_session"}, "403": {"csrf_required"}, "422": {"current_password_incorrect", "weak_password"}}, csrf: true},
		"/api/v1/auth/password/forgot":    {errors: map[string][]string{"400": {"validation_error"}, "403": {"csrf_required"}, "429": {"rate_limited"}, "503": {"internal_error"}}, example: map[string]any{"identifier": "operator@example.test"}, csrf: true},
		"/api/v1/auth/password/reset":     {errors: map[string][]string{"400": {"INVALID_TOKEN"}, "403": {"csrf_required"}, "422": {"weak_password"}, "429": {"rate_limited"}, "503": {"internal_error"}}, example: map[string]any{"token": "raw-token-from-email", "password": "strong-password"}, csrf: true},
	}
	for path, metadata := range identity {
		item := document.Paths[path]
		if item == nil {
			continue
		}
		if item.Post != nil {
			decorateOperation(item.Post, metadata.roles, metadata.errors, metadata.example, path == "/api/v1/login" || metadata.csrf)
		}
		if item.Get != nil {
			decorateOperation(item.Get, metadata.roles, metadata.errors, nil, false)
		}
		if item.Patch != nil {
			decorateOperation(item.Patch, metadata.roles, metadata.errors, metadata.example, true)
			item.Patch.Extensions["x-stable-error-codes"] = map[string][]string{"401": {"invalid_session"}, "403": {"csrf_required"}, "409": {"username_conflict"}, "422": {"invalid_display_name", "invalid_username"}}
		}
	}
	setMutationResponse(document, "/api/v1/account/password", http.MethodPost, http.StatusNoContent)
	setMutationResponse(document, "/api/v1/auth/password/forgot", http.MethodPost, http.StatusAccepted)
	setMutationResponse(document, "/api/v1/auth/password/reset", http.MethodPost, http.StatusNoContent)
	if operation := document.Paths["/api/v1/auth/password/forgot"].Post; operation != nil {
		operation.Responses["202"] = &huma.Response{Description: "Accepted"}
	}
	setRateLimitResponse(document, "/api/v1/auth/password/forgot")
	setRateLimitResponse(document, "/api/v1/auth/password/reset")
	trimResponses(document, "/api/v1/account", http.MethodGet, "200", "400", "401")
	trimResponses(document, "/api/v1/account", http.MethodPatch, "200", "400", "401", "403", "409", "422")
	trimResponses(document, "/api/v1/account/password", http.MethodPost, "204", "400", "401", "403", "422")
	trimResponses(document, "/api/v1/auth/password/forgot", http.MethodPost, "202", "400", "403", "429", "503")
	trimResponses(document, "/api/v1/auth/password/reset", http.MethodPost, "204", "400", "403", "422", "429", "503")
	compactAccountErrorResponses(document)
	setProfileExample(document)
	decorateOrganizationContract(document)
}

func decorateOrganizationContract(document *huma.OpenAPI) {
	metadata := map[string]struct {
		roles   []string
		errors  map[string][]string
		csrf    bool
		example any
	}{
		"/api/v1/organizations":      {roles: []string{"Owner", "Superadmin"}, errors: map[string][]string{"401": {"invalid_session"}, "403": {"organization_administration_forbidden", "csrf_required"}, "422": {"first_station_required", "invalid_organization_request"}}, csrf: true, example: map[string]any{"name": "Pom Org", "legal_name": "Pom Org PT", "address": "Jakarta", "contact_email": "admin@example.test", "timezone": "Asia/Jakarta", "first_station": map[string]any{"name": "Main", "timezone": "Asia/Jakarta"}}},
		"/api/v1/organizations/{id}": {roles: []string{"Owner", "Superadmin"}, errors: map[string][]string{"401": {"invalid_session"}, "403": {"organization_administration_forbidden", "organization_enabled_change_forbidden", "csrf_required"}, "404": {"organization_not_found"}, "422": {"invalid_organization_request"}}, csrf: true, example: map[string]any{"name": "Pom Org Updated", "enabled": true}},
	}
	if item := document.Paths["/api/v1/organizations"]; item != nil {
		if item.Get != nil {
			decorateOperation(item.Get, []string{"Owner", "Superadmin"}, map[string][]string{"401": {"invalid_session"}, "403": {"organization_administration_forbidden"}}, nil, false)
			ensureProblemResponses(item.Get, "400", "401", "403", "500")
		}
		if item.Post != nil {
			decorateOperation(item.Post, []string{"Superadmin"}, metadata["/api/v1/organizations"].errors, metadata["/api/v1/organizations"].example, true)
			ensureProblemResponses(item.Post, "400", "401", "403", "422", "500")
		}
		trimResponses(document, "/api/v1/organizations", http.MethodGet, "200", "400", "401", "403", "500")
		setMutationResponse(document, "/api/v1/organizations", http.MethodPost, http.StatusCreated)
		trimResponses(document, "/api/v1/organizations", http.MethodPost, "201", "400", "401", "403", "422", "500")
	}
	if item := document.Paths["/api/v1/organizations/{id}"]; item != nil {
		m := metadata["/api/v1/organizations/{id}"]
		if item.Get != nil {
			decorateOperation(item.Get, m.roles, map[string][]string{"401": {"invalid_session"}, "403": {"organization_administration_forbidden"}, "404": {"organization_not_found"}}, nil, false)
			ensureProblemResponses(item.Get, "400", "401", "403", "404", "500")
		}
		if item.Patch != nil {
			decorateOperation(item.Patch, m.roles, m.errors, m.example, true)
			ensureProblemResponses(item.Patch, "400", "401", "403", "404", "422", "500")
		}
		trimResponses(document, "/api/v1/organizations/{id}", http.MethodGet, "200", "400", "401", "403", "404", "500")
		trimResponses(document, "/api/v1/organizations/{id}", http.MethodPatch, "200", "400", "401", "403", "404", "422", "500")
	}
	if item := document.Paths["/api/v1/organizations/{id}/disable"]; item != nil && item.Post != nil {
		decorateOperation(item.Post, []string{"Superadmin"}, map[string][]string{"401": {"invalid_session"}, "403": {"organization_administration_forbidden", "csrf_required"}, "404": {"organization_not_found"}}, nil, true)
		ensureProblemResponses(item.Post, "400", "401", "403", "404", "500")
		setMutationResponse(document, "/api/v1/organizations/{id}/disable", http.MethodPost, http.StatusNoContent)
		trimResponses(document, "/api/v1/organizations/{id}/disable", http.MethodPost, "204", "400", "401", "403", "404", "500")
	}
}

func ensureProblemResponses(operation *huma.Operation, statuses ...string) {
	for _, status := range statuses {
		if _, exists := operation.Responses[status]; exists {
			continue
		}
		operation.Responses[status] = &huma.Response{Description: http.StatusText(statusCode(status)), Content: map[string]*huma.MediaType{"application/problem+json": {Schema: &huma.Schema{Ref: "#/components/schemas/ErrorModel"}}}}
	}
}

func statusCode(status string) int {
	for code := 400; code <= 599; code++ {
		if fmt.Sprint(code) == status {
			return code
		}
	}
	return http.StatusInternalServerError
}

func setMutationResponse(document *huma.OpenAPI, path, method string, status int) {
	item := document.Paths[path]
	if item == nil {
		return
	}
	var operation *huma.Operation
	if method == http.MethodPost {
		operation = item.Post
	}
	if operation == nil {
		return
	}
	if response := operation.Responses["200"]; response != nil {
		operation.Responses[fmt.Sprint(status)] = response
		delete(operation.Responses, "200")
	}
}

func setRateLimitResponse(document *huma.OpenAPI, path string) {
	item := document.Paths[path]
	if item == nil || item.Post == nil {
		return
	}
	if response := item.Post.Responses["400"]; response != nil {
		item.Post.Responses["429"] = response
	}
}

func trimResponses(document *huma.OpenAPI, path, method string, statuses ...string) {
	item := document.Paths[path]
	if item == nil {
		return
	}
	var operation *huma.Operation
	switch method {
	case http.MethodGet:
		operation = item.Get
	case http.MethodPatch:
		operation = item.Patch
	case http.MethodPost:
		operation = item.Post
	}
	if operation == nil {
		return
	}
	keep := make(map[string]struct{}, len(statuses))
	for _, status := range statuses {
		keep[status] = struct{}{}
	}
	for status := range operation.Responses {
		if _, ok := keep[status]; !ok {
			delete(operation.Responses, status)
		}
	}
}

func compactAccountErrorResponses(document *huma.OpenAPI) {
	if document.Components.Responses == nil {
		document.Components.Responses = map[string]*huma.Response{}
	}
	document.Components.Responses["AccountError"] = &huma.Response{Description: "RFC 7807 error", Content: map[string]*huma.MediaType{"application/problem+json": {Schema: &huma.Schema{Ref: "#/components/schemas/ErrorModel"}}}}
	for _, path := range []string{"/api/v1/account", "/api/v1/account/password", "/api/v1/auth/password/forgot", "/api/v1/auth/password/reset"} {
		item := document.Paths[path]
		if item == nil {
			continue
		}
		addDependencyResponse := path == "/api/v1/auth/password/forgot" || path == "/api/v1/auth/password/reset"
		operations := []*huma.Operation{item.Get, item.Patch, item.Post}
		for _, operation := range operations {
			if operation == nil {
				continue
			}
			for _, status := range []string{"400", "401", "403", "409", "422", "429", "503"} {
				if _, ok := operation.Responses[status]; ok || (status == "503" && addDependencyResponse) {
					operation.Responses[status] = &huma.Response{Ref: "#/components/responses/AccountError"}
				}
			}
		}
	}
}

func setProfileExample(document *huma.OpenAPI) {
	operation := document.Paths["/api/v1/account"].Get
	if operation == nil || operation.Responses["200"] == nil {
		return
	}
	media := operation.Responses["200"].Content["application/json"]
	if media == nil {
		return
	}
	media.Examples = map[string]*huma.Example{"example": {Summary: "Profile example", Value: map[string]any{
		"user_id": "11111111-1111-4111-8111-111111111111", "email": "operator@example.test", "username": "operator-1", "display_name": "Operator",
		"org": map[string]any{"id": "22222222-2222-4222-8222-222222222222", "name": "Example Org"}, "roles": []string{"Operator"},
		"stations": []map[string]any{{"station_id": "33333333-3333-4333-8333-333333333333", "name": "Main", "timezone": "Asia/Jakarta"}},
	}}}
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
	if operation.OperationID == "forgotPassword" || operation.OperationID == "resetPassword" {
		operation.Description = "Anonymous requests are limited to 5 per minute per client address."
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
	registerOpenAPIOperation[openAPIEmptyInput, openAPIOutput[appaccount.Profile]](api, http.MethodGet, "/api/v1/account", "account", "Read the own account profile")
	registerOpenAPIOperation[openAPIBodyInput[appaccount.UpdateRequest], openAPIOutput[appaccount.Profile]](api, http.MethodPatch, "/api/v1/account", "updateAccount", "Update the own account profile")
	registerOpenAPIOperation[openAPIBodyInput[appaccount.PasswordRequest], openAPIEmptyOutput](api, http.MethodPost, "/api/v1/account/password", "changeAccountPassword", "Change the own account password")
	registerOpenAPIOperation[openAPIEmptyInput, openAPIOutput[[]apporganization.Organization]](api, http.MethodGet, "/api/v1/organizations", "listOrganizations", "List organizations")
	registerOpenAPIOperation[openAPIBodyInput[apporganization.CreateRequest], openAPIOutput[apporganization.Organization]](api, http.MethodPost, "/api/v1/organizations", "createOrganization", "Create an organization")
	registerOpenAPIOperation[openAPIIDInput, openAPIOutput[apporganization.Organization]](api, http.MethodGet, "/api/v1/organizations/{id}", "getOrganization", "Read an organization")
	registerOpenAPIOperation[openAPIIDBodyInput[apporganization.OrganizationUpdateRequest], openAPIOutput[apporganization.OrganizationDetail]](api, http.MethodPatch, "/api/v1/organizations/{id}", "updateOrganization", "Update an organization")
	registerOpenAPIOperation[openAPIIDInput, openAPIEmptyOutput](api, http.MethodPost, "/api/v1/organizations/{id}/disable", "disableOrganization", "Disable an organization")
	registerOpenAPIOperation[openAPIBodyInput[accountapi.ForgotRequest], openAPIEmptyOutput](api, http.MethodPost, "/api/v1/auth/password/forgot", "forgotPassword", "Request a password reset email")
	registerOpenAPIOperation[openAPIBodyInput[accountapi.ResetRequest], openAPIEmptyOutput](api, http.MethodPost, "/api/v1/auth/password/reset", "resetPassword", "Reset a password with a token")

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
	if path != "/health" && path != "/ready" && path != "/api/v1/login" && path != "/api/v1/auth/invitations/accept" && path != "/api/v1/auth/password/forgot" && path != "/api/v1/auth/password/reset" {
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
