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

func TestSetupPickersUseQuickAndFullRadioChoices(t *testing.T) {
	mode := setupModePicker()
	if mode.Selected() != modeQuick {
		t.Fatalf("default setup mode = %q", mode.Selected())
	}
	for _, want := range []string{"How would you like to set up Wago?", "› ◉ Quick", "○ Full", "minimal project", "project details"} {
		if !strings.Contains(mode.Frame(), want) {
			t.Errorf("mode picker missing %q:\n%s", want, mode.Frame())
		}
	}
	if projectKindPicker().Selected() != kindApplication {
		t.Fatal("application should be the default full-setup project kind")
	}
	if stabilityPicker().Selected() != "experimental" {
		t.Fatal("experimental should be the default package stability")
	}
}

func TestFullApplicationManifestPreservesAndAddsPlugins(t *testing.T) {
	existing := map[string]any{"plugins": map[string]any{"wago-org/workers": "^0.0.0"}}
	fields, count, err := fullManifest(answers{
		kind: kindApplication, name: "Demo", description: "Wasm demo",
		plugins: "wago-org/wasi, acme/clock@^1.2.3",
	}, existing)
	if err != nil {
		t.Fatal(err)
	}
	plugins := fields["plugins"].(map[string]any)
	if count != 3 || plugins["wago-org/wasi"] != "^0.0.0" || plugins["acme/clock"] != "^1.2.3" || plugins["wago-org/workers"] != "^0.0.0" {
		t.Fatalf("plugins = %#v, count %d", plugins, count)
	}
	if fields["name"] != "Demo" || fields["description"] != "Wasm demo" {
		t.Fatalf("fields = %#v", fields)
	}
}

func TestFullPluginManifestIncludesPublishMetadata(t *testing.T) {
	fields, _, err := fullManifest(answers{
		kind: kindPlugin, name: "Clock", module: "github.com/acme/wago-clock", version: "0.1.0",
		license: "MIT", repository: "https://github.com/acme/wago-clock", homepage: "https://example.com/clock",
		category: "utilities", tags: "clock, time", author: "A. Maintainer", stability: "stable",
	}, nil)
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
}

func TestQuickSetupRemainsNonInteractive(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	ctx := command.NewContext(nil, nil, map[string]bool{"quick": true})
	got, err := run(ctx, strings.NewReader(""), &bytes.Buffer{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Created || got.Mode != modeQuick {
		t.Fatalf("result = %#v", got)
	}
	manifest, err := project.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if manifest["$schema"] != project.SchemaURI {
		t.Fatalf("manifest = %#v", manifest)
	}
	if _, err := os.Stat(filepath.Join(dir, project.File)); err != nil {
		t.Fatal(err)
	}
}

func TestFullSetupFlagsSelectFullModeWithoutPrompt(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	ctx := command.NewContext(nil, map[string]string{"name": "Flagged project"}, nil)
	got, err := run(ctx, strings.NewReader(""), &bytes.Buffer{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != modeFull {
		t.Fatalf("mode = %q, want full", got.Mode)
	}
	manifest, err := project.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if manifest["name"] != "Flagged project" {
		t.Fatalf("manifest = %#v", manifest)
	}
}
