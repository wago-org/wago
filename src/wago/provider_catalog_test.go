package wago

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const providerCatalogTestModule = "github.com/acme/plugin"

type providerCatalogTestPlugin struct{}

func (providerCatalogTestPlugin) Register(*Registrar) error { return nil }

func providerCatalogTestFactory() Plugin { return providerCatalogTestPlugin{} }

func providerCatalogTestDefinition(id string) PluginDefinition {
	return PluginDefinition{
		ID:          id,
		Name:        "Catalog test",
		Version:     "1.2.3",
		Description: "Exercises provider artifacts.",
		Stability:   Stable,
		Compatibility: Compatibility{
			Engines:   map[string]string{"wago": "^0.1.0"},
			Platforms: []string{"linux/amd64", "darwin/arm64"},
		},
		Provenance: PluginProvenance{
			Repository: "https://github.com/acme/plugin",
			Homepage:   "https://example.com/plugin",
			License:    "Apache-2.0",
			Authors:    []string{"Zulu", "Alpha"},
		},
		Requires: []PluginRequirement{
			{ID: "github.com/acme/z-dependency", Version: "^1.0.0"},
			{ID: "github.com/acme/a-dependency", Version: "~2.0.0"},
		},
		Authorities: []AuthorityRequest{
			{Name: AuthorityRuntimeCloseObserve, Mode: AuthorityOptional, Reason: "observe shutdown"},
			{Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "define imports", Scope: AuthorityScope{Modules: []string{"z", "a"}}},
		},
		ConfigSchema: json.RawMessage(` { "type": "object", "additionalProperties": false, "properties": {"enabled": {"type":"boolean"}} } `),
		Provides: []ContractSpec{
			{ID: "github.com/acme/contracts/z", Major: 1},
			{ID: "github.com/acme/contracts/a", Major: 1},
		},
		Consumes: []ContractRequirement{
			{ID: "github.com/acme/contracts/z-input", Major: 1, Mode: ContractOptional},
			{ID: "github.com/acme/contracts/a-input", Major: 1, Mode: ContractRequired},
		},
	}
}

func providerCatalogTestProvider(id string) PluginProvider {
	return PluginProvider{Definition: providerCatalogTestDefinition(id), New: providerCatalogTestFactory}
}

func TestEncodeProviderCatalogIsDeterministicAndOwnsSubpackages(t *testing.T) {
	root := providerCatalogTestProvider(providerCatalogTestModule)
	nested := providerCatalogTestProvider(providerCatalogTestModule + "/metrics/v2")
	rootBefore, err := json.Marshal(root.Definition)
	if err != nil {
		t.Fatal(err)
	}

	first, err := EncodeProviderCatalog(providerCatalogTestModule+"/register", []PluginProvider{nested, root})
	if err != nil {
		t.Fatalf("EncodeProviderCatalog: %v", err)
	}
	second, err := EncodeProviderCatalog(providerCatalogTestModule+"/register", []PluginProvider{root, nested})
	if err != nil {
		t.Fatalf("EncodeProviderCatalog reordered: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("provider order changed artifact:\n%s\n---\n%s", first, second)
	}
	if !bytes.HasSuffix(first, []byte("\n")) || bytes.HasSuffix(first, []byte("\n\n")) {
		t.Fatalf("artifact must end in exactly one newline: %q", first[len(first)-2:])
	}
	if !bytes.Contains(first, []byte(`"$schema": "`+ProviderCatalogSchemaURI+`"`)) {
		t.Fatalf("artifact schema missing:\n%s", first)
	}
	rootAfter, err := json.Marshal(root.Definition)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rootBefore, rootAfter) {
		t.Fatal("encoding mutated the linked provider definition")
	}

	document, err := DecodeProviderCatalog(first)
	if err != nil {
		t.Fatalf("DecodeProviderCatalog(EncodeProviderCatalog): %v\n%s", err, first)
	}
	if document.Schema != ProviderCatalogSchemaURI || len(document.Providers) != 2 {
		t.Fatalf("decoded document = %+v", document)
	}
	if got := []string{document.Providers[0].Definition.ID, document.Providers[1].Definition.ID}; !reflect.DeepEqual(got, []string{providerCatalogTestModule, providerCatalogTestModule + "/metrics/v2"}) {
		t.Fatalf("provider order = %v", got)
	}
	for _, entry := range document.Providers {
		if entry.ImportPath != providerCatalogTestModule+"/register" {
			t.Errorf("%s importPath = %q", entry.Definition.ID, entry.ImportPath)
		}
		digest, err := DefinitionDigest(entry.Definition)
		if err != nil {
			t.Fatal(err)
		}
		if entry.DefinitionDigest != digest {
			t.Errorf("%s digest = %q, want %q", entry.Definition.ID, entry.DefinitionDigest, digest)
		}
		if !sortOrder(entry.Definition.Provenance.Authors) || !sortOrder(entry.Definition.Compatibility.Platforms) || !sortOrder(entry.Definition.Authorities[0].Scope.Modules) {
			t.Errorf("%s definition was not canonicalized: %+v", entry.Definition.ID, entry.Definition)
		}
	}
}

func sortOrder(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index-1] > values[index] {
			return false
		}
	}
	return true
}

func TestCanonicalPluginDefinitionReturnsIndependentCanonicalCopy(t *testing.T) {
	original := providerCatalogTestDefinition(providerCatalogTestModule)
	canonical, err := CanonicalPluginDefinition(original)
	if err != nil {
		t.Fatal(err)
	}
	if !sortOrder(canonical.Provenance.Authors) || !sortOrder(canonical.Authorities[0].Scope.Modules) {
		t.Fatalf("canonical definition = %+v", canonical)
	}
	canonical.Provenance.Authors[0] = "Changed"
	canonical.Compatibility.Engines["wago"] = "*"
	if original.Provenance.Authors[0] == "Changed" || original.Compatibility.Engines["wago"] == "*" {
		t.Fatal("canonical definition aliases its input")
	}
}

func TestEncodeProviderCatalogRejectsInvalidOwnershipAndFactories(t *testing.T) {
	root := providerCatalogTestProvider(providerCatalogTestModule)
	nested := providerCatalogTestProvider(providerCatalogTestModule + "/metrics")
	withoutFactory := root
	withoutFactory.New = nil
	duplicate := root
	foreign := providerCatalogTestProvider("github.com/other/plugin")

	tests := map[string]struct {
		importPath string
		providers  []PluginProvider
	}{
		"empty":              {providerCatalogTestModule + "/register", nil},
		"nil factory":        {providerCatalogTestModule + "/register", []PluginProvider{withoutFactory}},
		"duplicate id":       {providerCatalogTestModule + "/register", []PluginProvider{root, duplicate}},
		"missing root":       {providerCatalogTestModule + "/register", []PluginProvider{nested}},
		"foreign provider":   {providerCatalogTestModule + "/register", []PluginProvider{root, foreign}},
		"alternate register": {providerCatalogTestModule + "/internal/register", []PluginProvider{root}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := EncodeProviderCatalog(test.importPath, test.providers); err == nil {
				t.Fatal("EncodeProviderCatalog succeeded")
			}
		})
	}

	tooMany := make([]PluginProvider, maxProviderCatalogEntries+1)
	tooMany[0] = root
	for index := 1; index < len(tooMany); index++ {
		tooMany[index] = providerCatalogTestProvider(fmt.Sprintf("%s/sub%d", providerCatalogTestModule, index))
	}
	if _, err := EncodeProviderCatalog(providerCatalogTestModule+"/register", tooMany); err == nil {
		t.Fatal("EncodeProviderCatalog accepted more than 128 providers")
	}
}

func TestDecodeProviderCatalogRejectsNoncanonicalArtifacts(t *testing.T) {
	encoded, err := EncodeProviderCatalog(providerCatalogTestModule+"/register", []PluginProvider{
		providerCatalogTestProvider(providerCatalogTestModule),
		providerCatalogTestProvider(providerCatalogTestModule + "/metrics"),
	})
	if err != nil {
		t.Fatal(err)
	}
	valid, err := DecodeProviderCatalog(encoded)
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]func(*ProviderCatalogDocument){
		"wrong schema": func(document *ProviderCatalogDocument) { document.Schema = "https://wago.sh/v2/providers.schema.json" },
		"empty":        func(document *ProviderCatalogDocument) { document.Providers = nil },
		"wrong digest": func(document *ProviderCatalogDocument) { document.Providers[0].DefinitionDigest = "sha256:wrong" },
		"unsorted providers": func(document *ProviderCatalogDocument) {
			document.Providers[0], document.Providers[1] = document.Providers[1], document.Providers[0]
		},
		"duplicate provider": func(document *ProviderCatalogDocument) {
			document.Providers = append(document.Providers, document.Providers[1])
		},
		"missing source provider": func(document *ProviderCatalogDocument) {
			document.Providers = document.Providers[1:]
		},
		"alternate register": func(document *ProviderCatalogDocument) {
			for index := range document.Providers {
				document.Providers[index].ImportPath = providerCatalogTestModule + "/internal/register"
			}
		},
		"foreign provider": func(document *ProviderCatalogDocument) {
			definition := document.Providers[1].Definition
			definition.ID = "github.com/other/plugin"
			canonical, canonicalErr := CanonicalPluginDefinition(definition)
			if canonicalErr != nil {
				t.Fatal(canonicalErr)
			}
			document.Providers[1].Definition = canonical
			document.Providers[1].DefinitionDigest, canonicalErr = DefinitionDigest(canonical)
			if canonicalErr != nil {
				t.Fatal(canonicalErr)
			}
		},
		"noncanonical definition": func(document *ProviderCatalogDocument) {
			authors := document.Providers[0].Definition.Provenance.Authors
			authors[0], authors[1] = authors[1], authors[0]
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			document := cloneProviderCatalogDocument(t, valid)
			mutate(&document)
			raw, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeProviderCatalog(raw); err == nil {
				t.Fatal("DecodeProviderCatalog succeeded")
			}
		})
	}

	tooMany := ProviderCatalogDocument{Schema: ProviderCatalogSchemaURI, Providers: make([]ProviderCatalogEntry, maxProviderCatalogEntries+1)}
	rawTooMany, err := json.Marshal(tooMany)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeProviderCatalog(rawTooMany); err == nil {
		t.Fatal("DecodeProviderCatalog accepted more than 128 providers")
	}

	for name, raw := range map[string][]byte{
		"unknown document field":   bytes.Replace(encoded, []byte(`"providers":`), []byte(`"future":true,"providers":`), 1),
		"unknown entry field":      bytes.Replace(encoded, []byte(`"importPath":`), []byte(`"future":true,"importPath":`), 1),
		"unknown definition field": bytes.Replace(encoded, []byte(`"version": "1.2.3"`), []byte(`"version": "1.2.3", "future": true`), 1),
		"trailing value":           append(append([]byte(nil), encoded...), []byte("{}")...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeProviderCatalog(raw); err == nil {
				t.Fatal("DecodeProviderCatalog succeeded")
			}
		})
	}
	if _, err := DecodeProviderCatalog(make([]byte, (2<<20)+1)); err == nil {
		t.Fatal("DecodeProviderCatalog accepted an artifact larger than 2 MiB")
	}
}

func cloneProviderCatalogDocument(t *testing.T, document ProviderCatalogDocument) ProviderCatalogDocument {
	t.Helper()
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	var clone ProviderCatalogDocument
	if err := json.Unmarshal(raw, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func TestProviderCatalogPublicConstants(t *testing.T) {
	if ProviderCatalogSchemaURI != "https://wago.sh/v1/providers.schema.json" {
		t.Fatalf("ProviderCatalogSchemaURI = %q", ProviderCatalogSchemaURI)
	}
	if ProviderCatalogFile != "wago.providers.json" {
		t.Fatalf("ProviderCatalogFile = %q", ProviderCatalogFile)
	}
	if strings.TrimSpace(ProviderCatalogFile) != ProviderCatalogFile {
		t.Fatal("ProviderCatalogFile is not canonical")
	}
}
