package registry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/internal/httpclient"
)

func init() {
	if os.Getenv("WAGO_TEST_FAKE_GO") == "1" && len(os.Args) >= 2 && os.Args[1] == "run" {
		data, err := os.ReadFile(os.Getenv("WAGO_TEST_CATALOG"))
		if err == nil {
			err = os.WriteFile(os.Args[len(os.Args)-1], data, 0o600)
		}
		if err != nil {
			_, _ = io.WriteString(os.Stderr, err.Error())
			os.Exit(1)
		}
		os.Exit(0)
	}
	if os.Getenv("WAGO_TEST_FAKE_GO") == "1" && len(os.Args) >= 3 && os.Args[1] == "mod" && os.Args[2] == "download" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{
			"Path": "github.com/acme/root", "Version": "v1.2.3", "Sum": "h1:artifact",
			"Dir": os.Getenv("WAGO_TEST_ARTIFACT"),
		})
		os.Exit(0)
	}
	if os.Getenv("WAGO_TEST_FAKE_GO") == "1" && os.Getenv("WAGO_TEST_PUBLISH_SIZE_ERROR") != "1" {
		_, _ = io.WriteString(os.Stderr, "unexpected fake go invocation")
		os.Exit(2)
	}
	if os.Getenv("WAGO_TEST_PUBLISH_SIZE_ERROR") == "1" {
		automation.Configure(automation.Options{NoInput: true})
		registryPublishContext(context.Background(), PublishRequest{Manifest: os.Getenv("WAGO_TEST_MANIFEST")})
		os.Exit(2)
	}
}

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

func TestPublishPayloadUsesDownloadedArtifactSize(t *testing.T) {
	automation.Configure(automation.Options{NoInput: true})
	t.Cleanup(automation.Reset)
	t.Setenv("WAGO_TOKEN", testRegistryToken)
	t.Setenv("WAGO_REGISTRY", "https://registry.example")
	checkout := filepath.Join(t.TempDir(), "work")
	remote := filepath.Join(t.TempDir(), "remote.git")
	// Put the fake module cache under the checkout and ignore it, matching CI
	// configurations that set GOMODCACHE to a workspace-local cache.
	artifact := filepath.Join(checkout, ".cache", "go-mod", "github.com", "acme", "root@v1.2.3")
	if err := os.MkdirAll(artifact, 0o755); err != nil {
		t.Fatal(err)
	}
	const module = "github.com/acme/root"
	manifest := []byte(`{
		"$schema":"https://wago.sh/v1/schema.json",
		"package":{
			"module":"github.com/acme/root","version":"1.2.3","name":"Root","description":"Useful.","stability":"stable",
			"license":"MIT","repository":"https://github.com/acme/root","authors":[{"name":"A"}]
		}
	}`)
	definition := wago.PluginDefinition{
		ID: module, Name: "Root", Version: "1.2.3", Description: "Useful.", Stability: wago.Stable,
		Provenance: wago.PluginProvenance{Repository: "https://github.com/acme/root", License: "MIT", Authors: []string{"A"}},
	}
	catalogData, err := wago.EncodeProviderCatalog(module+"/register", []wago.PluginProvider{{
		Definition: definition, New: func() wago.Plugin { return nil },
	}})
	if err != nil {
		t.Fatal(err)
	}
	testGit(t, "", "init", "--bare", "--initial-branch=main", remote)
	testGit(t, "", "init", "--initial-branch=main", checkout)
	testGit(t, checkout, "config", "user.name", "Wago test")
	testGit(t, checkout, "config", "user.email", "wago@example.test")
	if err := os.WriteFile(filepath.Join(checkout, ".gitignore"), []byte(".cache/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{checkout, artifact} {
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+module+"\n\ngo 1.22\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "wago.json"), manifest, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, wago.ProviderCatalogFile), catalogData, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, size := range map[string]int{
		filepath.Join(".git", "release-data"):  2048,
		filepath.Join(".wago", "release-data"): 3072,
	} {
		path := filepath.Join(artifact, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	testGit(t, checkout, "add", ".gitignore", "go.mod", "wago.json", wago.ProviderCatalogFile)
	testGit(t, checkout, "commit", "-m", "release files")
	testGit(t, checkout, "remote", "add", "origin", remote)
	testGit(t, checkout, "push", "-u", "origin", "main")
	testGit(t, checkout, "tag", "v1.2.3")
	testGit(t, checkout, "push", "origin", "refs/tags/v1.2.3")
	if err := os.WriteFile(filepath.Join(checkout, "working-copy-only"), make([]byte, 32<<10), 0o600); err != nil {
		t.Fatal(err)
	}
	testGit(t, checkout, "add", "working-copy-only")
	artifactBytes, err := testArtifactSize(artifact)
	if err != nil {
		t.Fatal(err)
	}
	artifactKB := int((artifactBytes + 1023) / 1024)
	checkoutKB := UnpackedKB(checkout)
	if artifactKB == 0 {
		t.Fatal("test artifact unexpectedly has zero size")
	}
	if artifactKB == checkoutKB {
		t.Fatalf("test setup sizes are equal: %d KB", artifactKB)
	}
	fakeGoDir := t.TempDir()
	fakeGo := filepath.Join(fakeGoDir, "go")
	if runtime.GOOS == "windows" {
		fakeGo += ".exe"
	}
	copyTestExecutable(t, fakeGo)
	t.Setenv("PATH", fakeGoDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WAGO_TEST_FAKE_GO", "1")
	catalogPath := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(catalogPath, catalogData, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_TEST_CATALOG", catalogPath)
	t.Setenv("WAGO_TEST_ARTIFACT", artifact)

	t.Run("rejects incomplete artifact walk", func(t *testing.T) {
		unreadable := filepath.Join(artifact, "unreadable")
		if err := os.Mkdir(unreadable, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(unreadable, "data"), []byte("must not be omitted"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(unreadable, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Chmod(unreadable, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(unreadable); err != nil {
				t.Fatal(err)
			}
		})
		if probe, err := os.Open(unreadable); err == nil {
			_ = probe.Close()
			t.Skip("current user can read mode-000 directories")
		}
		command := exec.Command(os.Args[0])
		command.Dir = checkout
		command.Env = append(os.Environ(),
			"WAGO_TEST_PUBLISH_SIZE_ERROR=1", "WAGO_TEST_MANIFEST="+filepath.Join(checkout, "wago.json"),
			"WAGO_REGISTRY=http://127.0.0.1:1")
		output, err := command.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "exact source artifact size") {
			t.Fatalf("publication with unreadable artifact returned %v:\n%s", err, output)
		}
	})

	t.Run("includes complete downloaded tree", func(t *testing.T) {
		previousHTTP := registryHTTP
		var payload map[string]any
		registryHTTP = httpclient.New(httpclient.Config{HTTPClient: &http.Client{Transport: registryRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodPost || request.URL.Path != "/api/publish" || request.Header.Get("Authorization") != "Bearer "+testRegistryToken {
				t.Fatalf("publish request = %s %s authorization %q", request.Method, request.URL.Path, request.Header.Get("Authorization"))
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		})}})
		t.Cleanup(func() { registryHTTP = previousHTTP })

		registryPublishContext(context.Background(), PublishRequest{Manifest: filepath.Join(checkout, "wago.json")})
		if got := int(payload["unpackedKB"].(float64)); got != artifactKB {
			t.Fatalf("published unpackedKB = %v, want downloaded artifact size %d KB (checkout is %d KB)", got, artifactKB, checkoutKB)
		}
	})
}

func testArtifactSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func copyTestExecutable(t *testing.T, destination string) {
	t.Helper()
	sourcePath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	target, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(target, source); err != nil {
		_ = target.Close()
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
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

func TestParsePublishManifestExplainsApplicationManifest(t *testing.T) {
	_, _, _, err := parsePublishManifest([]byte(`{"$schema":"https://wago.sh/v1/schema.json","plugins":{}}`))
	if err == nil || !strings.Contains(err.Error(), "configures an application") || !strings.Contains(err.Error(), "required package object") {
		t.Fatalf("application manifest error = %v", err)
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

func TestUnresolvedReleaseTagInstructions(t *testing.T) {
	message := unresolvedReleaseTagInstructions("v1.2.3")
	for _, want := range []string{
		"cannot resolve release tag v1.2.3",
		"must match package.version in wago.json",
		"git fetch origin tag v1.2.3",
		"git tag v1.2.3",
		"git push origin HEAD v1.2.3",
		"wago plugin publish",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("release tag instructions missing %q:\n%s", want, message)
		}
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
	if _, err := ValidateCatalogPlan(context.Background(), CatalogRequest{Manifest: manifest, Check: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidatePublishPlan(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	write("wago.providers.json", "{}\n")
	if err := generateProviderCatalog(context.Background(), CatalogRequest{Manifest: manifest, Check: true}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale check error = %v", err)
	}
	if _, err := ValidateCatalogPlan(context.Background(), CatalogRequest{Manifest: manifest, Check: true}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("catalog dry-run stale check error = %v", err)
	}
	if _, err := ValidatePublishPlan(context.Background(), manifest); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("publish dry-run stale check error = %v", err)
	}
	catalogPath := filepath.Join(root, wago.ProviderCatalogFile)
	if err := os.Remove(catalogPath); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateCatalogPlan(context.Background(), CatalogRequest{Manifest: manifest}); err != nil {
		t.Fatalf("catalog generation dry run = %v", err)
	}
	if _, err := os.Stat(catalogPath); !os.IsNotExist(err) {
		t.Fatalf("catalog dry run wrote %s: %v", catalogPath, err)
	}
	if _, err := ValidatePublishPlan(context.Background(), manifest); err == nil || !strings.Contains(err.Error(), "missing or unreadable") {
		t.Fatalf("publish dry-run missing catalog error = %v", err)
	}
}
