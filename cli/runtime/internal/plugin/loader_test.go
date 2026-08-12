//go:build !wago_minimal

package plugin

import (
	"encoding/json"
	"testing"

	"github.com/wago-org/wago"
)

type inertPlugin struct{}

func (inertPlugin) Register(*wago.Registrar) error { return nil }

func TestConfigureExposesOnlyExplicitReviewedPluginSet(t *testing.T) {
	definition := wago.PluginDefinition{
		ID: "github.com/acme/metrics", Name: "Metrics", Version: "1.2.3",
		Stability:     wago.Experimental,
		Compatibility: wago.Compatibility{Engines: map[string]string{"wago": "*"}},
		Provenance:    wago.PluginProvenance{Repository: "https://github.com/acme/metrics", License: "MIT"},
	}
	digest, err := wago.DefinitionDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	set := wago.PluginSet{
		Providers:  []wago.PluginProvider{{Definition: definition, New: func() wago.Plugin { return inertPlugin{} }}},
		Selections: []wago.PluginSelection{{ID: definition.ID, DefinitionDigest: digest, Direct: true, Dependencies: map[string]string{}, Config: json.RawMessage(`{}`)}},
	}
	Configure(set)
	t.Cleanup(func() { Configure(wago.PluginSet{}) })

	// Mutating the caller's slices cannot mutate the configured catalog.
	set.Providers = nil
	set.Selections = nil
	if got := Definitions(); len(got) != 1 || got[0].ID != definition.ID {
		t.Fatalf("definitions = %#v", got)
	}
	if _, ok := Definition(definition.ID); !ok {
		t.Fatalf("Definition(%q) missing", definition.ID)
	}
	plan, err := Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Plugins) != 1 || plan.Plugins[0].DefinitionDigest != digest {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestVerifyRejectsDefinitionDigestDriftWithoutRunningProvider(t *testing.T) {
	definition := wago.PluginDefinition{
		ID: "github.com/acme/drift", Version: "1.0.0",
		Compatibility: wago.Compatibility{Engines: map[string]string{"wago": "*"}},
		Provenance:    wago.PluginProvenance{Repository: "https://github.com/acme/drift", License: "MIT"},
	}
	ran := false
	Configure(wago.PluginSet{
		Providers:  []wago.PluginProvider{{Definition: definition, New: func() wago.Plugin { ran = true; return inertPlugin{} }}},
		Selections: []wago.PluginSelection{{ID: definition.ID, DefinitionDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Direct: true, Dependencies: map[string]string{}}},
	})
	t.Cleanup(func() { Configure(wago.PluginSet{}) })
	if err := Verify(); err == nil {
		t.Fatal("Verify accepted a stale definition digest")
	}
	if ran {
		t.Fatal("side-effect-free verification invoked provider factory")
	}
}
