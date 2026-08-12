package build

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/project"
)

func TestInputFromLockLinksSharedSourceOnceAndRejectsConflicts(t *testing.T) {
	const moduleID = "github.com/acme/bundle"
	lock := project.NewLockDocument()
	for _, suffix := range []string{"alpha", "beta"} {
		id := moduleID + "/" + suffix
		lock.Plugins[id] = project.LockEntry{
			Direct:               true,
			Source:               project.PluginSource{Module: moduleID, Version: "v1.0.0", Checksum: "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
			Provider:             project.ProviderSource{ImportPath: moduleID + "/register"},
			DefinitionDigest:     "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ReleaseFingerprint:   "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Dependencies:         map[string]string{},
			RequestedAuthorities: []project.AuthorityRequest{},
			Grants:               []project.AuthorityGrant{},
			Contracts:            project.ContractSet{Provides: []project.ContractProvider{}, Requires: []project.ContractRequirement{}},
			Bindings:             []project.ContractBinding{},
			Config:               []byte(`{}`),
		}
	}
	input, err := InputFromLock(lock)
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Sources) != 1 || len(input.ProviderImports) != 1 || len(input.Selections) != 2 {
		t.Fatalf("shared source input = %#v", input)
	}
	for _, selection := range input.Selections {
		if !selection.Direct || len(selection.Dependencies) != 0 {
			t.Fatalf("selection omitted reviewed root/dependencies: %#v", selection)
		}
	}

	beta := lock.Plugins[moduleID+"/beta"]
	beta.Source.Checksum = "h1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	lock.Plugins[moduleID+"/beta"] = beta
	if _, err := InputFromLock(lock); err == nil || !strings.Contains(err.Error(), "conflicting release") {
		t.Fatalf("conflicting shared source build input error = %v", err)
	}
}

func TestBuildModuleFilesystemAndGoModHelpers(t *testing.T) {
	tmp := t.TempDir()
	goMod := "module example.test/plugin\n\ngo 1.23\n\nreplace example.test/local => ./local\nreplace example.test/remote v1.0.0 => example.test/remote v1.2.0\n"
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tmp, "local"), 0o700); err != nil {
		t.Fatal(err)
	}
	if mod, ok := readGoMod(tmp); !ok || mod.Go != "1.23" {
		t.Fatalf("readGoMod = %#v, %v", mod, ok)
	}
	if got := wagoGoDirective(tmp); got != "1.23" {
		t.Fatalf("go directive = %q", got)
	}
	replaces := mirroredReplaces(tmp)
	if len(replaces) != 2 || !strings.Contains(replaces[0], "example.test/local=") || replaces[1] != "example.test/remote@v1.0.0=example.test/remote@v1.2.0" {
		t.Fatalf("mirrored replaces = %#v", replaces)
	}
	for _, path := range []string{".", "..", "./x", "../x", tmp} {
		if !isFilesystemPath(path) {
			t.Fatalf("%q not recognized as filesystem path", path)
		}
	}
	wantSuffix := ""
	if runtime.GOOS == "windows" {
		wantSuffix = ".exe"
	}
	if isFilesystemPath("example.test/mod") || exeSuffix() != wantSuffix {
		t.Fatal("module path helpers changed")
	}

	t.Setenv("WAGO_SRC", tmp)
	if got, err := ModuleDir(); err != nil || got != tmp {
		t.Fatalf("WAGO_SRC module dir = %q, %v", got, err)
	}
	if got, ok := SourceDir(); !ok || got != tmp {
		t.Fatalf("WAGO_SRC source dir = %q, %v", got, ok)
	}
	if err := RunGo(tmp, false, "version"); err != nil {
		t.Fatalf("goRun version: %v", err)
	}
}

func TestGeneratedModuleGoCommandsIgnoreParentWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.22\n\nthis is invalid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "generated")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/generated\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readGoModStrict(dir); err != nil {
		t.Fatalf("generated module inherited parent go.work: %v", err)
	}
}

func TestNewBuildIdentityIsPerBinary(t *testing.T) {
	first := newBuildIdentity("stable-build-inputs")
	second := newBuildIdentity("stable-build-inputs")
	if len(first) != 64 || len(second) != 64 {
		t.Fatalf("identity lengths = %d/%d, want 64/64", len(first), len(second))
	}
	if first == second {
		t.Fatal("separate plugin binary builds reused one cache identity")
	}
}

func TestBuildHashTracksMirroredFilesystemReplacementContent(t *testing.T) {
	source := t.TempDir()
	replacement := filepath.Join(t.TempDir(), "plugin")
	if err := os.MkdirAll(replacement, 0o755); err != nil {
		t.Fatal(err)
	}
	goMod := "module github.com/wago-org/wago\n\ngo 1.22\n\nreplace example.test/plugin => " + filepath.ToSlash(replacement) + "\n"
	if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	pluginFile := filepath.Join(replacement, "plugin.go")
	if err := os.WriteFile(pluginFile, []byte("package plugin\n\nconst Value = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAGO_SRC", source)
	config := Config{RuntimeVersion: "test", Profile: "test"}
	first := Hash(Input{}, config)
	if err := os.WriteFile(pluginFile, []byte("package plugin\n\nconst Value = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := Hash(Input{}, config)
	if first == second {
		t.Fatal("build hash ignored mutable filesystem replacement content")
	}
}

func TestInstalledSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	if dir := InstalledSource(); dir != "" {
		t.Fatalf("no source yet: got %q", dir)
	}
	source := filepath.Join(home, ".wago", "src")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module example.com/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dir := InstalledSource(); dir != "" {
		t.Fatalf("non-wago go.mod: got %q", dir)
	}
	if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module github.com/wago-org/wago\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dir := InstalledSource(); dir != source {
		t.Fatalf("InstalledSource = %q, want %q", dir, source)
	}
}

func TestEnsureModuleCreatesReusableGoModule(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "generated")
	if err := EnsureModule(dir); err != nil {
		t.Fatalf("EnsureModule: %v", err)
	}
	goMod := filepath.Join(dir, "go.mod")
	first, err := os.ReadFile(goMod)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), "module "+buildModuleName) {
		t.Fatalf("generated go.mod = %s", first)
	}
	if err := EnsureModule(dir); err != nil {
		t.Fatalf("repeat EnsureModule: %v", err)
	}
	second, err := os.ReadFile(goMod)
	if err != nil || string(second) != string(first) {
		t.Fatalf("repeat go.mod = %q, %v; want unchanged", second, err)
	}
}

func TestSyncModuleRefreshesWagoSource(t *testing.T) {
	buildDir := filepath.Join(t.TempDir(), "generated")
	sourceRoot := t.TempDir()
	first := filepath.Join(sourceRoot, "first")
	second := filepath.Join(sourceRoot, "second")
	for _, source := range []string{first, second} {
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module github.com/wago-org/wago\n\ngo 1.22\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("WAGO_SRC", first)
	changed, err := syncBuildModule(buildDir)
	if err != nil || !changed {
		t.Fatalf("initial sync = changed %v, err %v", changed, err)
	}
	body, err := os.ReadFile(filepath.Join(buildDir, "go.mod"))
	if err != nil || !strings.Contains(string(body), filepath.ToSlash(first)) {
		t.Fatalf("initial go.mod = %q, %v", body, err)
	}

	changed, err = syncBuildModule(buildDir)
	if err != nil || changed {
		t.Fatalf("repeat sync = changed %v, err %v", changed, err)
	}

	t.Setenv("WAGO_SRC", second)
	changed, err = syncBuildModule(buildDir)
	if err != nil || !changed {
		t.Fatalf("source refresh = changed %v, err %v", changed, err)
	}
	body, err = os.ReadFile(filepath.Join(buildDir, "go.mod"))
	if err != nil || !strings.Contains(string(body), filepath.ToSlash(second)) ||
		strings.Contains(string(body), filepath.ToSlash(first)) {
		t.Fatalf("refreshed go.mod = %q, %v", body, err)
	}
}

func TestSyncModuleDropsRemovedMirroredReplace(t *testing.T) {
	buildDir := filepath.Join(t.TempDir(), "generated")
	source := t.TempDir()
	local := filepath.Join(source, "local")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSourceModule := func(replace bool) {
		t.Helper()
		body := "module github.com/wago-org/wago\n\ngo 1.22\n"
		if replace {
			body += "\nreplace example.test/local => ./local\n"
		}
		if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writeSourceModule(true)
	t.Setenv("WAGO_SRC", source)
	changed, err := syncBuildModule(buildDir)
	if err != nil || !changed {
		t.Fatalf("initial sync = changed %v, err %v", changed, err)
	}
	body, err := os.ReadFile(filepath.Join(buildDir, "go.mod"))
	if err != nil || !strings.Contains(string(body), "replace example.test/local => "+filepath.ToSlash(local)) {
		t.Fatalf("mirrored go.mod = %q, %v", body, err)
	}

	writeSourceModule(false)
	changed, err = syncBuildModule(buildDir)
	if err != nil || !changed {
		t.Fatalf("reconciled sync = changed %v, err %v", changed, err)
	}
	body, err = os.ReadFile(filepath.Join(buildDir, "go.mod"))
	if err != nil || strings.Contains(string(body), "example.test/local") {
		t.Fatalf("reconciled go.mod retained stale replace = %q, %v", body, err)
	}
}
