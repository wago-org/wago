package settings

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/internal/atomicfile"
)

func TestGlobalSettingsIgnoreRetiredV1Optimizations(t *testing.T) {
	for _, name := range project.RetiredOptimizationNames() {
		for _, enabled := range []bool{false, true} {
			path := filepath.Join(t.TempDir(), "settings.json")
			data := fmt.Sprintf(`{"version":1,"optimizations":{%q:%t}}`, name, enabled)
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			config, err := LoadFile(path)
			if err != nil {
				t.Fatalf("%s=%v: %v", name, enabled, err)
			}
			if _, ok := config.Optimizations[name]; ok {
				t.Fatalf("retired option %s retained in active settings", name)
			}
		}
	}
}

func TestSettingsRoundTripAndDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	config, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !config.Features["simd"] || config.Runtime.Parallel != "1" || !config.Runtime.DeferredBoundsChecking {
		t.Fatalf("defaults = %#v", config)
	}
	if err := Set(&config, "simd", "off", false); err != nil {
		t.Fatal(err)
	}
	if err := Set(&config, "runtime.parallel", "auto", false); err != nil {
		t.Fatal(err)
	}
	if err := SaveFile(path, config); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Features["simd"] || loaded.Runtime.Parallel != "auto" {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestSettingsSavePreservesExistingOnReplaceFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := []byte(`{"version":1}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("replace failed")
	previous := replaceSettingsFile
	replaceSettingsFile = func(_ string, _ atomicfile.Options, write func(io.Writer) error) error {
		if err := write(io.Discard); err != nil {
			t.Fatal(err)
		}
		return injected
	}
	t.Cleanup(func() { replaceSettingsFile = previous })
	if err := SaveFile(path, Default()); !errors.Is(err, injected) {
		t.Fatalf("SaveFile error = %v, want %v", err, injected)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("failed save changed existing settings to %q", got)
	}
}

func TestPartialSettingsKeepBuiltInDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"features":{"simd":false},"runtime":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	config, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Features["simd"] || !config.Features["multi-value"] || !config.Runtime.DeferredBoundsChecking || config.Runtime.Parallel != "1" {
		t.Fatalf("partial config did not preserve defaults: %#v", config)
	}
}

func TestSettingsRejectDuplicateMembers(t *testing.T) {
	for _, data := range []string{
		`{"version":1,"version":1}`,
		`{"version":1,"Version":1}`,
		`{"version":1,"features":{"simd":true,"simd":false}}`,
		`{"version":1,"features":{"simd":false,"SIMD":false}}`,
		`{"version":1,"features":{"tail-call":false,"tail_call":false}}`,
		`{"version":1,"optimizations":{"inline":false,"INLINE":false}}`,
	} {
		path := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadFile(path); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("duplicate settings members accepted: %s: %v", data, err)
		}
	}
}

func TestSettingsRejectPreviewAndUnknown(t *testing.T) {
	config := Default()
	var experimental BoolSetting
	for _, setting := range Experimental() {
		if setting.Available {
			experimental = setting
			break
		}
	}
	if experimental.Key != "" {
		if err := Set(&config, experimental.Key, "on", false); err == nil {
			t.Fatal("experimental setting was enabled without the flag")
		}
		if err := Set(&config, experimental.Key, "on", true); err != nil {
			t.Fatalf("experimental setting was not enabled with flag: %v", err)
		}
		name := experimental.Key[strings.IndexByte(experimental.Key, '.')+1:]
		if strings.HasPrefix(experimental.Key, "features.") && !config.Features[name] {
			t.Fatal("experimental feature was not stored")
		}
		if strings.HasPrefix(experimental.Key, "optimizations.") && !config.Optimizations[name] {
			t.Fatal("experimental optimization was not stored")
		}
	}
	if err := Set(&config, "not-a-setting", "on", false); err == nil {
		t.Fatal("unknown setting was accepted")
	}
	if err := Set(&config, "runtime.parallel", "many", false); err == nil {
		t.Fatal("invalid parallel default was accepted")
	}
}

func TestUnavailableFeaturesMayRemainDisabled(t *testing.T) {
	var unavailable BoolSetting
	for _, setting := range allFeatures() {
		if !setting.Available {
			unavailable = setting
			break
		}
	}
	if unavailable.Key == "" {
		t.Skip("all features are available on this platform")
	}

	values := map[string]bool{unavailable.name: false}
	if err := ValidateFeatureValues(values); err != nil {
		t.Fatalf("disabled unavailable feature should be portable: %v", err)
	}
	values[unavailable.name] = true
	if err := ValidateFeatureValues(values); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("enabled unavailable feature should be rejected clearly, got %v", err)
	}

	path := filepath.Join(t.TempDir(), "settings.json")
	contents := fmt.Sprintf(`{"version":1,"features":{"%s":false},"runtime":{}}`, unavailable.name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err != nil {
		t.Fatalf("loading a disabled unavailable feature should be portable: %v", err)
	}
	contents = fmt.Sprintf(`{"version":1,"features":{"%s":true},"runtime":{}}`, unavailable.name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("loading an enabled unavailable feature should fail clearly, got %v", err)
	}
}

func TestResetRestoresBuiltInValue(t *testing.T) {
	config := Default()
	if err := Set(&config, "features.simd", "off", false); err != nil {
		t.Fatal(err)
	}
	if err := Reset(&config, "features.simd", false); err != nil {
		t.Fatal(err)
	}
	if !config.Features["simd"] {
		t.Fatal("reset did not restore SIMD")
	}
}
