package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/cli/internal/project"
)

func TestLocalSettingsOverrideGlobalAndResetToGlobal(t *testing.T) {
	dir := enterSettingsTestDir(t)
	globalPath := filepath.Join(t.TempDir(), "settings.json")
	t.Setenv("WAGO_CONFIG", globalPath)
	global := Default()
	if err := Set(&global, "simd", "off", false); err != nil {
		t.Fatal(err)
	}
	if err := Save(global); err != nil {
		t.Fatal(err)
	}
	writeTestManifest(t, dir)

	target, err := Open(false, false)
	if err != nil {
		t.Fatal(err)
	}
	value, _ := target.Get("simd")
	if target.Scope() != ScopeLocal || value != "false" {
		t.Fatalf("initial target = scope %q, simd %q", target.Scope(), value)
	}
	if err := target.Set("simd", "on", false); err != nil {
		t.Fatal(err)
	}
	if err := target.Save(); err != nil {
		t.Fatal(err)
	}
	manifest, err := project.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	settingsObject := manifest[localField].(map[string]any)
	features := settingsObject["features"].(map[string]any)
	if features["simd"] != true {
		t.Fatalf("local settings = %#v", settingsObject)
	}

	target, err = Open(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := target.Get("simd"); value != "true" {
		t.Fatalf("resolved local simd = %q", value)
	}
	overrides := target.Overrides()
	if len(overrides) != 1 || overrides[0].Key != "features.simd" || overrides[0].Base != "false" || overrides[0].Value != "true" {
		t.Fatalf("overrides = %#v", overrides)
	}
	t.Setenv(project.GlobalEnv, "1")
	resolved, configured, err := LoadConfigured()
	if err != nil {
		t.Fatal(err)
	}
	if !configured || resolved.Features["simd"] {
		t.Fatalf("explicit global runtime settings = %#v, configured %v", resolved.Features, configured)
	}
	t.Setenv(project.GlobalEnv, "")
	if err := target.Reset("simd", false); err != nil {
		t.Fatal(err)
	}
	if err := target.Save(); err != nil {
		t.Fatal(err)
	}
	manifest, err = project.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := manifest[localField]; exists {
		t.Fatalf("reset left empty local settings: %#v", manifest)
	}
	if manifest["package"].(map[string]any)["name"] != "Test project" {
		t.Fatalf("reset changed unrelated manifest fields: %#v", manifest)
	}
}

func TestLocalRuntimeOverrideIsSparse(t *testing.T) {
	dir := enterSettingsTestDir(t)
	t.Setenv("WAGO_CONFIG", filepath.Join(t.TempDir(), "settings.json"))
	writeTestManifest(t, dir)
	target, err := Open(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Set("runtime.parallel", "auto", false); err != nil {
		t.Fatal(err)
	}
	if err := target.Save(); err != nil {
		t.Fatal(err)
	}
	manifest, err := project.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	runtimeSettings := manifest[localField].(map[string]any)["runtime"].(map[string]any)
	if len(runtimeSettings) != 1 || runtimeSettings["parallel"] != "auto" {
		t.Fatalf("runtime settings = %#v", runtimeSettings)
	}
	target.ResetAll()
	if err := target.Save(); err != nil {
		t.Fatal(err)
	}
	manifest, err = project.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := manifest[localField]; exists {
		t.Fatalf("reset all left local settings: %#v", manifest)
	}
}

func TestLocalSettingsSavePreservesConcurrentManifestUpdate(t *testing.T) {
	dir := enterSettingsTestDir(t)
	t.Setenv("WAGO_CONFIG", filepath.Join(t.TempDir(), "settings.json"))
	writeTestManifest(t, dir)
	target, err := Open(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Set("simd", "off", false); err != nil {
		t.Fatal(err)
	}
	id := "github.com/acme/logger"
	if added, err := project.AddDependency(dir, id, "^1.0.0"); err != nil || !added {
		t.Fatalf("AddDependency = %v, %v", added, err)
	}
	if err := target.Save(); err != nil {
		t.Fatal(err)
	}
	manifest, err := project.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if manifest["plugins"].(map[string]any)[id] != "^1.0.0" {
		t.Fatalf("settings save lost concurrent plugin update: %#v", manifest)
	}
}

func TestGlobalTargetIgnoresLocalManifest(t *testing.T) {
	dir := enterSettingsTestDir(t)
	t.Setenv("WAGO_CONFIG", filepath.Join(t.TempDir(), "settings.json"))
	writeTestManifest(t, dir)
	target, err := Open(true, false)
	if err != nil {
		t.Fatal(err)
	}
	if target.Scope() != ScopeGlobal || target.Path() != Path() {
		t.Fatalf("target = scope %q path %q", target.Scope(), target.Path())
	}
}

func TestExplicitLocalRequiresManifest(t *testing.T) {
	enterSettingsTestDir(t)
	t.Setenv("WAGO_CONFIG", filepath.Join(t.TempDir(), "settings.json"))
	if _, err := Open(false, true); err == nil {
		t.Fatal("explicit local target succeeded without wago.json")
	}
}

func TestLocalSettingsIgnoreRetiredV1Optimizations(t *testing.T) {
	dir := enterSettingsTestDir(t)
	t.Setenv("WAGO_CONFIG", filepath.Join(t.TempDir(), "settings.json"))
	writeTestManifest(t, dir)
	manifest, err := project.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	manifest[localField] = map[string]any{"optimizations": map[string]any{
		"swar-idioms":  true,
		"gc-ref-facts": false,
	}}
	if err := project.Write(dir, manifest); err != nil {
		t.Fatal(err)
	}
	target, err := Open(false, false)
	if err != nil {
		t.Fatalf("open previous v1 local settings: %v", err)
	}
	if _, ok := target.Config().Optimizations["swar-idioms"]; ok {
		t.Fatal("retired optimization was retained in the active compiler selection")
	}
}

func enterSettingsTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	return dir
}

func writeTestManifest(t *testing.T, dir string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"$schema": project.SchemaURI, "plugins": map[string]any{},
		"package": map[string]any{
			"module": "github.com/acme/test-project", "name": "Test project", "description": "Settings test project.",
			"license": "MIT", "repository": "https://github.com/acme/test-project", "authors": []any{map[string]any{"name": "Test"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project.Path(dir), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
