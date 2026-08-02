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
	existing := map[string]any{"plugins": map[string]any{"wago-org/workers": "^0.0.0"}}
	fields, _, err := pluginManifest(answers{
		name: "Clock", module: "github.com/acme/wago-clock", version: "0.1.0",
		license: "MIT", repository: "https://github.com/acme/wago-clock", homepage: "https://example.com/clock",
		category: "utilities", tags: "clock, time", author: "A. Maintainer", stability: "stable",
		plugins: "wago-org/wasi, acme/clock@^1.2.3",
	}, existing)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"module": "github.com/acme/wago-clock", "version": "0.1.0", "license": "MIT",
		"repository": "https://github.com/acme/wago-clock", "stability": "stable", "private": false,
	} {
		if fields[key] != want {
			t.Errorf("%s = %#v, want %#v", key, fields[key], want)
		}
	}
	if tags := fields["tags"].([]string); strings.Join(tags, ",") != "clock,time" {
		t.Fatalf("tags = %#v", tags)
	}
	plugins := fields["plugins"].(map[string]any)
	if len(plugins) != 3 || plugins["wago-org/wasi"] != "^0.0.0" || plugins["acme/clock"] != "^1.2.3" || plugins["wago-org/workers"] != "^0.0.0" {
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
		"name": "Flagged plugin", "module": "github.com/acme/flagged", "license": "MIT",
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
	if manifest["name"] != "Flagged plugin" {
		t.Fatalf("manifest = %#v", manifest)
	}
}
