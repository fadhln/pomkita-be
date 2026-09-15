package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAPIContract_ContainsVersionedRoutes(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(root, "docs", "openapi.json"))
	if err != nil {
		t.Fatalf("read OpenAPI contract: %v", err)
	}
	var document struct {
		OpenAPI    string                            `json:"openapi"`
		Generator  string                            `json:"x-generated-by"`
		Paths      map[string]map[string]interface{} `json:"paths"`
		Components struct {
			Schemas         map[string]map[string]interface{} `json:"schemas"`
			SecuritySchemes map[string]map[string]interface{} `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal(contents, &document); err != nil {
		t.Fatalf("decode OpenAPI contract: %v", err)
	}
	if document.OpenAPI != "3.0.3" {
		t.Fatalf("OpenAPI version: got %q, want 3.0.3", document.OpenAPI)
	}
	if document.Generator != "huma" {
		t.Fatalf("OpenAPI generator: got %q, want huma", document.Generator)
	}
	for _, scheme := range []string{"sessionCookie", "bearerAuth"} {
		if _, ok := document.Components.SecuritySchemes[scheme]; !ok {
			t.Fatalf("OpenAPI contract does not define %s", scheme)
		}
	}
	for _, route := range []struct{ path, method string }{{"/api/v1/session", "get"}, {"/api/v1/shifts", "post"}, {"/api/v1/audit", "get"}} {
		operation := document.Paths[route.path][route.method]
		security, ok := operation.(map[string]interface{})["security"].([]interface{})
		if !ok || len(security) != 2 {
			t.Fatalf("OpenAPI route %s %s does not declare cookie and bearer security", route.method, route.path)
		}
	}
	for _, path := range []string{"/api/v1/login", "/api/v1/session", "/api/v1/shifts", "/api/v1/drafts/claim", "/api/v1/drafts/heartbeat", "/api/v1/drafts/readings", "/api/v1/drafts/sales", "/api/v1/drafts/losses", "/api/v1/drafts/evidence", "/api/v1/submissions", "/api/v1/reports/{id}", "/api/v1/reports/{id}/printout", "/api/v1/reports/{id}/acknowledgement", "/api/v1/amendments", "/api/v1/amendments/{id}/approve", "/api/v1/amendments/{id}/reject", "/api/v1/policies/revisions", "/api/v1/policies/history", "/api/v1/anomalies", "/api/v1/anomalies/export", "/api/v1/audit", "/api/v1/audit/export", "/api/v1/audit/verify", "/health", "/ready"} {
		if _, ok := document.Paths[path]; !ok {
			t.Fatalf("OpenAPI contract does not define %s", path)
		}
	}
	for _, path := range []string{"/api/v1/anomalies/export", "/api/v1/audit/export"} {
		operation := document.Paths[path]["get"]
		responses, ok := operation.(map[string]interface{})["responses"].(map[string]interface{})
		if !ok {
			t.Fatalf("CSV route %s has no responses", path)
		}
		success, ok := responses["200"].(map[string]interface{})
		content, contentOK := success["content"].(map[string]interface{})
		if !ok || !contentOK || content["text/csv"] == nil {
			t.Fatalf("CSV route %s does not declare text/csv", path)
		}
	}
	report, ok := document.Components.Schemas["ReportView"]
	if !ok {
		t.Fatal("OpenAPI contract does not define ReportView")
	}
	if report["additionalProperties"] != false {
		t.Fatal("ReportView must reject unknown fields")
	}
	properties, ok := report["properties"].(map[string]interface{})
	if !ok || properties["sales"] == nil || properties["losses"] == nil {
		t.Fatal("ReportView does not define report child collections")
	}
}
