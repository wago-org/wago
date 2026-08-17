package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/project"
	pluginbuild "github.com/wago-org/wago/cli/manager/internal/plugin/build"
	"github.com/wago-org/wago/internal/wagopaths"
)

func TestParsePluginSpecExpandsGitHubShorthand(t *testing.T) {
	id, constraint, err := parsePluginSpec("wago-org/wasi@^1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if id != "github.com/wago-org/wasi" || constraint != "^1.2.3" {
		t.Fatalf("parsePluginSpec shorthand = %q, %q; want github.com/wago-org/wasi, ^1.2.3", id, constraint)
	}
}

func TestPluginRuntimeBinaryResolvesGlobalBuild(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	t.Setenv("WAGO_BARE", "")
	t.Setenv("WAGO_GLOBAL", "")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	emptyProject := t.TempDir()
	if err := os.Chdir(emptyProject); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	source := t.TempDir()
	for path, body := range map[string]string{
		"go.mod": "module github.com/wago-org/wago\n\ngo 1.22\n",
		"wago.go": `package wago
type PluginSelection struct{}
type PluginProvider struct{}
type PluginSet struct { Providers []PluginProvider; Selections []PluginSelection }
func ValidatePluginSet(PluginSet) error { return nil }
`,
		"cli/runtime/runtime.go": `package runtime
import wago "github.com/wago-org/wago"
func MainWithPluginSet(string, string, wago.PluginSet) {}
`,
		"register/register.go": `package register
import wago "github.com/wago-org/wago"
func Providers() []wago.PluginProvider { return nil }
`,
	} {
		fullPath := filepath.Join(source, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("WAGO_SRC", source)

	buildDir, err := buildDirFor(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := pluginbuild.EnsureModule(buildDir); err != nil {
		t.Fatal(err)
	}
	manifestDir := sharedGlobalPluginDir(wago.DirsFor(managerVersion()))
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const plugin = "github.com/wago-org/wago"
	if _, err := project.AddDependency(manifestDir, plugin, "^0.0.0"); err != nil {
		t.Fatal(err)
	}

	lock := project.NewLockDocument()
	entry := testManagerLockEntry(plugin)
	lock.Plugins[plugin] = entry
	if err := project.WriteLock(manifestDir, lock); err != nil {
		t.Fatal(err)
	}
	got, configured, err := pluginRuntimeBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !configured || got != pluginbuild.BinaryPath(buildDir) {
		t.Fatalf("plugin runtime = %q, %v; want %q, true", got, configured, pluginbuild.BinaryPath(buildDir))
	}
}

func TestLockedPluginResolutionRequiresPinnedVersionsBeforeBuilding(t *testing.T) {
	automation.Reset()
	t.Cleanup(automation.Reset)
	manifestDir := t.TempDir()
	if _, err := project.AddDependency(manifestDir, "github.com/wago-org/wasi", "^1.0.0"); err != nil {
		t.Fatal(err)
	}
	buildDir := filepath.Join(t.TempDir(), "not-created")
	t.Setenv(automation.EnvLocked, "1")

	_, err := syncLockedPluginVersions(buildDir, manifestDir, false)
	if err == nil || !strings.Contains(err.Error(), "wago-org/wasi") {
		t.Fatalf("locked resolution error = %v", err)
	}
	if _, err := os.Stat(buildDir); !os.IsNotExist(err) {
		t.Fatalf("locked resolution touched build state: %v", err)
	}
}

func TestVerifySourceChecksumsReconcilesGeneratedModule(t *testing.T) {
	buildDir := t.TempDir()
	// Go 1.26 normalizes a two-component go directive to three components and
	// otherwise rejects `go list` before it can report the selected modules.
	// Generated plugin modules must be reconciled before checksum verification.
	if err := os.WriteFile(filepath.Join(buildDir, "go.mod"), []byte("module wago.local/build\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifySourceChecksums(buildDir, nil); err != nil {
		t.Fatalf("verifySourceChecksums: %v", err)
	}
}

func TestLockedPluginResolutionRejectsPreexistingSourceReplace(t *testing.T) {
	const plugin = "github.com/acme/plugin"
	manifestDir := t.TempDir()
	if _, err := project.AddDependency(manifestDir, plugin, "^1.0.0"); err != nil {
		t.Fatal(err)
	}
	lock := project.NewLockDocument()
	entry := testManagerLockEntry(plugin)
	entry.Source.Version = "v1.0.0"
	lock.Plugins[plugin] = entry
	if err := project.WriteLock(manifestDir, lock); err != nil {
		t.Fatal(err)
	}

	buildDir := t.TempDir()
	local := filepath.Join(buildDir, "local-plugin")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "go.mod"), []byte("module "+plugin+"\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	goMod := "module wago.local/build\n\ngo 1.22\n\nrequire " + plugin + " v1.0.0\n\nreplace " + plugin + " => ./local-plugin\n"
	if err := os.WriteFile(filepath.Join(buildDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := syncLockedPluginVersions(buildDir, manifestDir, false)
	if err == nil || !strings.Contains(err.Error(), "locked plugin source "+plugin+"@v1.0.0") || !strings.Contains(err.Error(), "go.mod replace") {
		t.Fatalf("locked replacement = changed %v, err %v", changed, err)
	}
	body, readErr := os.ReadFile(filepath.Join(buildDir, "go.mod"))
	if readErr != nil || !strings.Contains(string(body), "replace "+plugin+" => ./local-plugin") {
		t.Fatalf("rejected replacement was silently reconciled = %q, %v", body, readErr)
	}
}

type testEnvironment struct{}

func (testEnvironment) SelectScope(global, local, bare bool) error {
	return Select(global, local, bare)
}

func (testEnvironment) RuntimeBinary() (string, bool, error) {
	return RuntimeBinary()
}

func TestRuntimePathForInvocationLeavesMinimalRuntimeAlone(t *testing.T) {
	const base = "/runtime/wago"
	got, err := Resolve(base, wagopaths.ProfileMinimal, []string{"run", "module.wasm"}, testEnvironment{})
	if err != nil {
		t.Fatal(err)
	}
	if got != base {
		t.Fatalf("minimal runtime path = %q, want %q", got, base)
	}
}

func testManagerLockEntry(id string) project.LockEntry {
	return project.LockEntry{
		Direct: true, Source: project.PluginSource{Module: id, Version: "v0.0.0", Checksum: "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
		Provider:           project.ProviderSource{ImportPath: id + "/register"},
		DefinitionDigest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ReleaseFingerprint: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Dependencies:       map[string]string{}, RequestedAuthorities: []project.AuthorityRequest{}, Grants: []project.AuthorityGrant{},
		Contracts: project.ContractSet{Provides: []project.ContractProvider{}, Requires: []project.ContractRequirement{}},
		Bindings:  []project.ContractBinding{}, Config: []byte(`{}`),
	}
}
