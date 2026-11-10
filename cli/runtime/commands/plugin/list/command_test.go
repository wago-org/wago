package list

import (
	"testing"

	"github.com/wago-org/wago"
)

func TestPlanEntry(t *testing.T) {
	plan := &wago.PluginPlan{Plugins: []wago.PluginPlanEntry{{ID: "github.com/acme/a"}, {ID: "github.com/acme/b"}}}
	if got := planEntry(plan, "github.com/acme/b"); got == nil || got.ID != "github.com/acme/b" {
		t.Fatalf("planEntry = %#v", got)
	}
	if got := planEntry(plan, "github.com/acme/missing"); got != nil {
		t.Fatalf("missing planEntry = %#v", got)
	}
}
