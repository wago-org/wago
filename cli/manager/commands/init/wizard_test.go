package initcmd

import (
	"bytes"
	"os"
	"path/filepath"
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

func TestPluginManifestRejectsRelativePluginID(t *testing.T) {
	_, _, err := pluginManifest(answers{
		name: "Clock", description: "Clock imports.", module: "github.com/acme/clock", version: "1.0.0",
		license: "MIT", repository: "https://github.com/acme/clock", author: "A. Maintainer", plugins: "acme/wasi@^1.0.0",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "fully qualified") {
		t.Fatalf("relative ID error = %v", err)
	}
}
