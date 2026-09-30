package exampleplugin

import (
	"testing"

	wago "github.com/wago-org/wago"
)

type testPlugin struct{}

func (testPlugin) Register(*wago.Registrar) error { return nil }

func TestMustSetCopiesRequestedScopes(t *testing.T) {
	definition := wago.PluginDefinition{
		ID:      "example.com/test",
		Version: "1.0.0",
		Provenance: wago.PluginProvenance{
			Repository: "https://example.com/test",
			License:    "Apache-2.0",
		},
		Authorities: []wago.AuthorityRequest{{
			Name:   wago.AuthorityHostImportDefine,
			Mode:   wago.AuthorityRequired,
			Reason: "define the test import",
			Scope:  wago.AuthorityScope{Modules: []string{"test"}},
		}},
	}
	set := MustSet(definition, func() wago.Plugin { return testPlugin{} })
	if got := set.Selections[0].Grants[0].Scope.Modules; len(got) != 1 || got[0] != "test" {
		t.Fatalf("granted modules = %v", got)
	}
}
