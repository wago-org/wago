package registry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
)

func TestSourceSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "one"), make([]byte, 1025), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := walkedSize(dir); got != 1025 {
		t.Fatalf("walkedSize = %d", got)
	}
	if got := gitTrackedSize(dir); got != -1 {
		t.Fatalf("gitTrackedSize non-repo = %d", got)
	}
	if got := UnpackedKB(dir); got != 2 {
		t.Fatalf("UnpackedKB = %d", got)
	}
	if got := UnpackedKB(filepath.Join(dir, "missing")); got != 0 {
		t.Fatalf("missing UnpackedKB = %d", got)
	}
	if GitOutput("definitely-not-a-git-command") != "" {
		t.Fatal("failed GitOutput was non-empty")
	}
}

func TestValidatePublishProvidersRequiresManifestCatalogParity(t *testing.T) {
	raw := []byte(`{
		"$schema":"https://wago.sh/v1/schema.json",
		"package":{
			"module":"github.com/acme/root","version":"1.2.3","name":"Root","description":"Useful.","stability":"stable",
			"license":"MIT","repository":"https://github.com/acme/root","homepage":"https://acme.example/root",
			"authors":[{"name":"A"}],"engines":{"wago":"^0.1.0"},"platforms":["linux/amd64"]
		}
	}`)
	_, metadata, _, err := parsePublishManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	definition := wago.PluginDefinition{
		ID: "github.com/acme/root", Name: "Root", Version: "1.2.3", Description: "Useful.", Stability: wago.Stable,
		Compatibility: wago.Compatibility{Engines: map[string]string{"wago": "^0.1.0"}, Platforms: []string{"linux/amd64"}},
		Provenance:    wago.PluginProvenance{Repository: "https://github.com/acme/root", Homepage: "https://acme.example/root", License: "MIT", Authors: []string{"A"}},
	}
	digest, err := wago.DefinitionDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	providers := []publishProvider{{ImportPath: "github.com/acme/root/register", Definition: definition, DefinitionDigest: digest}}
	if err := validatePublishProviders(providers, metadata, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	providers[0].Definition.Provenance.Repository = "https://github.com/other/root"
	if err := validatePublishProviders(providers, metadata, "1.2.3"); err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("provenance drift error = %v", err)
	}
}

func TestParsePublishManifestUsesNestedV1PackageAndExplicitCatalogs(t *testing.T) {
	raw := []byte(`{
		"$schema":"https://wago.sh/v1/schema.json",
		"plugins":{},
		"package":{
			"module":"github.com/acme/root","version":"1.2.3","name":"Root","description":"Useful.",
			"license":"MIT","repository":"https://github.com/acme/root","authors":[{"name":"A"}],
			"subpackages":[{"module":"github.com/acme/root/metrics","name":"Metrics","description":"Metrics."}]
		}
	}`)
	manifest, metadata, imports, err := parsePublishManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if manifest["$schema"] != "https://wago.sh/v1/schema.json" || metadata.Module != "github.com/acme/root" || metadata.Version != "1.2.3" {
		t.Fatalf("parsed manifest = %#v, %#v", manifest, metadata)
	}
	if got := strings.Join(imports, ","); got != "github.com/acme/root/register" {
		t.Fatalf("provider imports = %q", got)
	}
	for _, invalid := range []string{
		`{"module":"github.com/acme/root"}`,
		`{"$schema":"https://wago.sh/v0/schema.json","package":{"module":"github.com/acme/root"}}`,
		`{"$schema":"https://wago.sh/v1/schema.json","package":{"module":"acme/root"}}`,
	} {
		if _, _, _, err := parsePublishManifest([]byte(invalid)); err == nil {
			t.Fatalf("accepted invalid publish manifest: %s", invalid)
		}
	}
}

func TestCanonicalGoVersionAndIsolatedWorkspace(t *testing.T) {
	for input, want := range map[string]string{
		"0.1.0":   "v0.1.0",
		"v0.1.0":  "v0.1.0",
		" 1.2.3 ": "v1.2.3",
	} {
		if got := canonicalGoVersion(input); got != want {
			t.Errorf("canonicalGoVersion(%q) = %q, want %q", input, got, want)
		}
	}
	environment := isolatedGoEnvironment([]string{"A=1", "GOWORK=/tmp/local.work", "GOWORK=off", "B=2"})
	if got := strings.Join(environment, ","); got != "A=1,B=2,GOWORK=off" {
		t.Fatalf("isolated environment = %q", got)
	}
}

func TestFullGitCommit(t *testing.T) {
	if !fullGitCommit(strings.Repeat("a", 40)) {
		t.Fatal("rejected lowercase full commit")
	}
	for _, invalid := range []string{strings.Repeat("a", 39), strings.Repeat("A", 40), strings.Repeat("g", 40)} {
		if fullGitCommit(invalid) {
			t.Fatalf("accepted invalid commit %q", invalid)
		}
	}
}

func TestCatalogVersionInfersOneProviderVersion(t *testing.T) {
	providers := []publishProvider{
		{Definition: wago.PluginDefinition{ID: "github.com/acme/root", Version: "1.2.3"}},
		{Definition: wago.PluginDefinition{ID: "github.com/acme/root/metrics", Version: "v1.2.3"}},
	}
	if got, err := catalogVersion("", providers); err != nil || got != "1.2.3" {
		t.Fatalf("catalogVersion = %q, %v", got, err)
	}
	providers[1].Definition.Version = "1.2.4"
	if _, err := catalogVersion("", providers); err == nil {
		t.Fatal("catalogVersion accepted mismatched providers")
	}
}

func TestComparePublishedPackageManifestIgnoresProjectStateOnly(t *testing.T) {
	local := map[string]any{"package": map[string]any{"module": "github.com/acme/root", "tags": []any{"one"}}, "plugins": map[string]any{"local": "^1"}}
	artifact := map[string]any{"package": map[string]any{"module": "github.com/acme/root", "tags": []any{"one"}}, "plugins": map[string]any{}}
	if err := comparePublishedPackageManifest(local, artifact); err != nil {
		t.Fatal(err)
	}
	artifact["package"].(map[string]any)["tags"] = []any{"invented"}
	if err := comparePublishedPackageManifest(local, artifact); err == nil {
		t.Fatal("accepted package metadata drift")
	}
}

func TestModulePathFromGoMod(t *testing.T) {
	for name, contents := range map[string]string{
		"plain":  "module github.com/acme/root\n\ngo 1.22\n",
		"quoted": "module \"github.com/acme/root\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "go.mod")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := modulePathFromGoMod(path)
			if err != nil || got != "github.com/acme/root" {
				t.Fatalf("modulePathFromGoMod = %q, %v", got, err)
			}
		})
	}
}

func TestGenerateProviderCatalogUsesCurrentModuleAndDetectsDrift(t *testing.T) {
	root := t.TempDir()
	write := func(name, value string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	core, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	write("go.mod", "module github.com/acme/catalogtest\n\ngo 1.22\n\nrequire github.com/wago-org/wago v0.0.0\nreplace github.com/wago-org/wago => "+core+"\n")
	write("wago.json", `{
  "$schema":"https://wago.sh/v1/schema.json",
  "package":{"module":"github.com/acme/catalogtest","version":"1.2.3","name":"Catalog test","description":"Tests catalog generation.","stability":"stable","license":"MIT","repository":"https://github.com/acme/catalogtest","authors":[{"name":"A"}]}
}`)
	write("register/register.go", `package register
import wago "github.com/wago-org/wago"
type plugin struct{}
func (plugin) Register(*wago.Registrar) error { return nil }
func Providers() []wago.PluginProvider { return []wago.PluginProvider{{Definition:wago.PluginDefinition{ID:"github.com/acme/catalogtest",Version:"1.2.3",Name:"Catalog test",Description:"Tests catalog generation.",Stability:wago.Stable,Provenance:wago.PluginProvenance{Repository:"https://github.com/acme/catalogtest",License:"MIT",Authors:[]string{"A"}}},New:func() wago.Plugin{return plugin{}}}} }
`)
	manifest := filepath.Join(root, "wago.json")
	if err := generateProviderCatalog(context.Background(), CatalogRequest{Manifest: manifest}); err != nil {
		t.Fatal(err)
	}
	if err := generateProviderCatalog(context.Background(), CatalogRequest{Manifest: manifest, Check: true}); err != nil {
		t.Fatal(err)
	}
	write("wago.providers.json", "{}\n")
	if err := generateProviderCatalog(context.Background(), CatalogRequest{Manifest: manifest, Check: true}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale check error = %v", err)
	}
}
