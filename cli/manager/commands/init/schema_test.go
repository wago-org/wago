package initcmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/cli/internal/project"
)

func TestWagoSchemaTracksPluginManifest(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		ID         string                     `json:"$id"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatalf("invalid schema.json: %v", err)
	}
	if schema.ID != project.SchemaURI {
		t.Fatalf("schema $id = %q", schema.ID)
	}
	if _, exists := schema.Properties["schema"]; exists {
		t.Fatal("schema.json still exposes the removed manifest schema version field")
	}
	if _, exists := schema.Properties["settings"]; !exists {
		t.Fatal("schema.json omits project-local runtime settings")
	}
	var plugins struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(schema.Properties["plugins"], &plugins); err != nil || plugins.Type != "object" {
		t.Fatalf("plugins type = %q, %v; want object", plugins.Type, err)
	}
}
