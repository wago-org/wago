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

func TestConfigureAndPluginSetDeepCopyMutableMetadata(t *testing.T) {
	definition := wago.PluginDefinition{
		ID: "github.com/acme/isolated", Name: "Isolated", Version: "1.2.3",
		Compatibility: wago.Compatibility{
			Engines:   map[string]string{"wago": "*"},
			Platforms: []string{"linux/amd64"},
		},
		Provenance: wago.PluginProvenance{
			Repository: "https://github.com/acme/isolated", License: "MIT", Authors: []string{"Acme"},
		},
		ConfigSchema: json.RawMessage(`{"additionalProperties":false,"type":"object"}`),
	}
	digest, err := wago.DefinitionDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	set := wago.PluginSet{
		Providers: []wago.PluginProvider{{Definition: definition, New: func() wago.Plugin { return inertPlugin{} }}},
		Selections: []wago.PluginSelection{{
			ID: definition.ID, DefinitionDigest: digest, Direct: true,
			Dependencies: map[string]string{}, Config: json.RawMessage(`{"enabled":true}`),
		}},
	}
	Configure(set)
	t.Cleanup(func() { Configure(wago.PluginSet{}) })

	set.Providers[0].Definition.Compatibility.Engines["wago"] = "mutated"
	set.Providers[0].Definition.Provenance.Authors[0] = "mutated"
	set.Providers[0].Definition.ConfigSchema[0] = '['
	set.Selections[0].Dependencies["example.com/mutated"] = "v1.0.0"
	set.Selections[0].Config[0] = '['

	first := PluginSet()
	if got := first.Providers[0].Definition.Compatibility.Engines["wago"]; got != "*" {
		t.Fatalf("configured engine constraint = %q, want independent copy", got)
	}
	if got := first.Providers[0].Definition.Provenance.Authors[0]; got != "Acme" {
		t.Fatalf("configured author = %q, want independent copy", got)
	}
	if string(first.Providers[0].Definition.ConfigSchema) != `{"additionalProperties":false,"type":"object"}` ||
		len(first.Selections[0].Dependencies) != 0 || string(first.Selections[0].Config) != `{"enabled":true}` {
		t.Fatalf("configured nested metadata was mutated: %#v", first)
	}

	first.Providers[0].Definition.Compatibility.Platforms[0] = "mutated"
	first.Selections[0].Config[0] = '['
	second := PluginSet()
	if got := second.Providers[0].Definition.Compatibility.Platforms[0]; got != "linux/amd64" {
		t.Fatalf("returned platform mutated linked set: %q", got)
	}
	if got := string(second.Selections[0].Config); got != `{"enabled":true}` {
		t.Fatalf("returned config mutated linked set: %q", got)
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
