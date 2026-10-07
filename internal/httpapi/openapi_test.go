package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAPIContainsImplementedPublicRoutes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		OpenAPI string                    `json:"openapi"`
		Paths   map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("OpenAPI JSON/YAML subset is invalid: %v", err)
	}
	if doc.OpenAPI != "3.1.0" {
		t.Fatalf("openapi=%q", doc.OpenAPI)
	}
	for path, method := range map[string]string{
		"/api/v1/health":         "get",
		"/api/v1/server":         "get",
		"/api/v1/libraries":      "get",
		"/api/v1/items":          "get",
		"/api/v1/items/{itemId}": "get",
	} {
		if doc.Paths[path] == nil || doc.Paths[path][method] == nil {
			t.Fatalf("contract missing %s %s", method, path)
		}
	}
}
