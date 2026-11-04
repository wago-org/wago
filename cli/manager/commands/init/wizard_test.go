package initcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/project"
)

func TestSetupPickerOffersRunOrPlugin(t *testing.T) {
	mode := setupModePicker()
	if mode.Selected() != modeRun {
		t.Fatalf("default setup mode = %q", mode.Selected())
	}
	for _, want := range []string{"What would you like to do?", "› ◉ Run WebAssembly", "○ Set up a plugin", "minimal Wago project", "publishable Wago plugin"} {
		if !strings.Contains(mode.Frame(), want) {
			t.Errorf("mode picker missing %q:\n%s", want, mode.Frame())
		}
	}
	if stabilityPicker().Selected() != "experimental" {
		t.Fatal("experimental should be the default package stability")
	}
}

func TestPluginManifestIncludesPublishMetadata(t *testing.T) {
	existing := map[string]any{"plugins": map[string]any{"github.com/wago-org/workers": "^0.0.0"}}
	fields, _, err := pluginManifest(answers{
		name: "Clock", description: "Clock imports for guests.", module: "github.com/acme/wago-clock", version: "0.1.0",
		license: "MIT", repository: "https://github.com/acme/wago-clock", homepage: "https://example.com/clock",
		category: "utilities", tags: "clock, time", author: "A. Maintainer", stability: "stable",
		plugins: "github.com/wago-org/wasi, github.com/acme/clock@>=1.2.3 <2.0.0 || ^3.0.0",
	}, existing)
	if err != nil {
		t.Fatal(err)
	}
	pkg := fields["package"].(map[string]any)
	for key, want := range map[string]any{
		"module": "github.com/acme/wago-clock", "version": "0.1.0", "license": "MIT",
		"repository": "https://github.com/acme/wago-clock", "stability": "stable",
	} {
		if pkg[key] != want {
			t.Errorf("%s = %#v, want %#v", key, pkg[key], want)
		}
	}
	if tags := pkg["tags"].([]string); strings.Join(tags, ",") != "clock,time" {
		t.Fatalf("tags = %#v", tags)
	}
	plugins := fields["plugins"].(map[string]any)
	if len(plugins) != 3 || plugins["github.com/wago-org/wasi"] != "*" || plugins["github.com/acme/clock"] != ">=1.2.3 <2.0.0 || ^3.0.0" || plugins["github.com/wago-org/workers"] != "^0.0.0" {
		t.Fatalf("plugins = %#v", plugins)
	}
}

func TestRunModeRejectsPluginOptions(t *testing.T) {
	ctx := command.NewContext(nil, map[string]string{"name": "not needed"}, map[string]bool{"run": true})
	if _, err := explicitMode(ctx); err == nil || !strings.Contains(err.Error(), "plugin setup options") {
		t.Fatalf("explicitMode error = %v", err)
	}
}

func TestRunSetupNeedsNoName(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	ctx := command.NewContext(nil, nil, map[string]bool{"run": true})
	got, err := run(ctx, strings.NewReader(""), &bytes.Buffer{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Created || got.Mode != modeRun {
		t.Fatalf("result = %#v", got)
	}
	manifest, err := project.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if manifest["$schema"] != project.SchemaURI {
		t.Fatalf("manifest = %#v", manifest)
	}
	if _, exists := manifest["name"]; exists {
		t.Fatalf("run-only manifest unexpectedly requires a name: %#v", manifest)
	}
	if _, err := os.Stat(filepath.Join(dir, project.File)); err != nil {
		t.Fatal(err)
	}
}

func TestPluginSetupFlagsSelectPluginModeWithoutPrompt(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	ctx := command.NewContext(nil, map[string]string{
		"name": "Flagged plugin", "description": "A useful plugin.", "module": "github.com/acme/flagged", "license": "MIT", "author": "A. Maintainer",
	}, nil)
	got, err := run(ctx, strings.NewReader(""), &bytes.Buffer{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != modePlugin {
		t.Fatalf("mode = %q, want plugin", got.Mode)
	}
	manifest, err := project.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	pkg, _ := manifest["package"].(map[string]any)
	if pkg["name"] != "Flagged plugin" {
		t.Fatalf("manifest = %#v", manifest)
	}
	for _, name := range []string{"register.go", "register_test.go"} {
		if _, err := os.Stat(filepath.Join(dir, "register", name)); err != nil {
			t.Fatalf("missing scaffold %s: %v", name, err)
		}
	}
	catalog, err := os.ReadFile(filepath.Join(dir, "wago.providers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(catalog, []byte(`"$schema": "https://wago.sh/v1/providers.schema.json"`)) || !bytes.Contains(catalog, []byte(`"id": "github.com/acme/flagged"`)) {
		t.Fatalf("provider catalog = %s", catalog)
	}
}

func TestPluginSetupPreservesPackageMetadata(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	pkg := map[string]any{
		"module": "github.com/acme/clock", "version": "1.2.3", "name": "Clock",
		"description": "Clock imports.", "license": "MIT", "repository": "https://github.com/acme/clock",
		"stability": "stable", "homepage": "https://example.com/clock", "category": "utilities",
		"tags":      []any{"clock", "time"},
		"engines":   map[string]any{"wago": "^0.1.0", "go": ">=1.22.0"},
		"platforms": []any{"linux/amd64", "darwin/arm64"},
		"authors": []any{
			map[string]any{"name": "A. Maintainer", "email": "a@example.com", "github": "maintainer"},
			map[string]any{"name": "B. Maintainer", "url": "https://example.com/b"},
		},
		"subpackages": []any{map[string]any{
			"module": "github.com/acme/clock/timer", "name": "Timer", "description": "Timer imports.",
			"engines": map[string]any{"wago": "^0.1.0"}, "platforms": []any{"linux/amd64"},
		}},
	}
	manifest := map[string]any{
		"package":  pkg,
		"plugins":  map[string]any{"github.com/acme/dependency": "^1.0.0"},
		"settings": map[string]any{"runtime": map[string]any{"parallel": "2"}},
	}
	if err := project.Write(dir, manifest); err != nil {
		t.Fatal(err)
	}
	ctx := command.NewContext(nil, map[string]string{
		"name": "Clock", "description": "Updated clock imports.", "module": "github.com/acme/clock",
		"version": "1.2.3", "license": "MIT", "repository": "https://github.com/acme/clock",
		"author": "A. Maintainer", "stability": "stable",
	}, map[string]bool{"plugin": true, "yes": true})
	got, err := run(ctx, strings.NewReader(""), &bytes.Buffer{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Created || got.Mode != modePlugin || got.Plugins != 1 {
		t.Fatalf("result = %#v", got)
	}
	saved, err := project.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	wantPackage := make(map[string]any, len(pkg))
	for key, value := range pkg {
		wantPackage[key] = value
	}
	wantPackage["description"] = "Updated clock imports."
	gotPackage := saved["package"].(map[string]any)
	if len(gotPackage) != len(wantPackage) {
		t.Errorf("package contains %d fields, want %d", len(gotPackage), len(wantPackage))
	}
	for key, want := range wantPackage {
		if !reflect.DeepEqual(gotPackage[key], want) {
			t.Errorf("package.%s = %#v, want %#v", key, gotPackage[key], want)
		}
	}
	for _, key := range []string{"plugins", "settings"} {
		if !reflect.DeepEqual(saved[key], manifest[key]) {
			t.Errorf("%s = %#v, want %#v", key, saved[key], manifest[key])
		}
	}

	// Keeping subpackages must also keep their validation: renaming the parent
	// to an unrelated module cannot silently erase them to make the update fit.
	before, err := os.ReadFile(project.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	ctx = command.NewContext(nil, map[string]string{
		"name": "Other", "description": "Other imports.", "module": "github.com/acme/other",
		"version": "1.2.3", "license": "MIT", "author": "A. Maintainer",
	}, map[string]bool{"plugin": true, "yes": true})
	if _, err := run(ctx, strings.NewReader(""), &bytes.Buffer{}, false); err == nil || !strings.Contains(err.Error(), "must be below package.module") {
		t.Fatalf("incompatible module update error = %v", err)
	}
	after, err := os.ReadFile(project.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("rejected module update changed wago.json")
	}
}

func TestPluginManifestExplicitEditsDoNotMutateExistingPackage(t *testing.T) {
	existing := map[string]any{"package": map[string]any{
		"name": "Old name", "homepage": "https://example.com/old", "category": "old", "tags": []any{"old"},
		"authors": []any{map[string]any{"name": "Old author", "email": "old@example.com"}},
	}}
	fields, _, err := pluginManifest(answers{
		name: "New name", description: "Clock imports.", module: "github.com/acme/clock", license: "MIT",
		homepage: "https://example.com/new", category: "new", tags: "clock, time", author: "New author",
	}, existing)
	if err != nil {
		t.Fatal(err)
	}
	pkg := fields["package"].(map[string]any)
	want := map[string]any{
		"name": "New name", "homepage": "https://example.com/new", "category": "new", "tags": []string{"clock", "time"},
		"authors": []any{map[string]any{"name": "New author"}},
		"engines": map[string]any{"wago": "*"},
	}
	for key, value := range want {
		if !reflect.DeepEqual(pkg[key], value) {
			t.Errorf("package.%s = %#v, want %#v", key, pkg[key], value)
		}
	}
	if original := existing["package"].(map[string]any); len(original) != 5 || original["name"] != "Old name" || firstAuthor(original["authors"]) != "Old author" {
		t.Fatalf("pluginManifest mutated the original package: %#v", original)
	}
}

func TestPluginManifestRejectsRelativePluginID(t *testing.T) {
	_, _, err := pluginManifest(answers{
		name: "Clock", description: "Clock imports.", module: "github.com/acme/clock", version: "1.0.0",
		license: "MIT", repository: "https://github.com/acme/clock", author: "A. Maintainer", plugins: "acme/wasi@^1.0.0",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "fully qualified") {
		t.Fatalf("relative ID error = %v", err)
	}
}
