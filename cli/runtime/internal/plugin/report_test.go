package plugin

import (
	"encoding/json"
	"testing"

	"github.com/wago-org/wago"
)

func TestBuildReportUsesImmutableDefinitionAndPlan(t *testing.T) {
	definition := wago.PluginDefinition{ID: "github.com/acme/report", Name: "Report", Version: "1.0.0"}
	entry := &wago.PluginPlanEntry{ID: definition.ID, DefinitionDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	result := BuildReport(definition, entry)
	if result.Definition.ID != definition.ID || result.Plan != entry {
		t.Fatalf("report = %#v", result)
	}
	if _, err := json.Marshal(result); err != nil {
		t.Fatalf("marshal: %v", err)
	}
}
