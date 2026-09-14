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
		OpenAPI string                            `json:"openapi"`
		Paths   map[string]map[string]interface{} `json:"paths"`
	}
	if err := json.Unmarshal(contents, &document); err != nil {
		t.Fatalf("decode OpenAPI contract: %v", err)
	}
	if document.OpenAPI != "3.0.3" {
		t.Fatalf("OpenAPI version: got %q, want 3.0.3", document.OpenAPI)
	}
	for _, path := range []string{"/api/v1/login", "/api/v1/session", "/api/v1/shifts", "/health", "/ready"} {
		if _, ok := document.Paths[path]; !ok {
			t.Fatalf("OpenAPI contract does not define %s", path)
		}
	}
}
