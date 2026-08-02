package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsRoundTripAndDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	config, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !config.Features["simd"] || config.Runtime.Parallel != "1" || !config.Runtime.DeferredBoundsChecking {
		t.Fatalf("defaults = %#v", config)
	}
	if err := Set(&config, "simd", "off"); err != nil {
		t.Fatal(err)
	}
	if err := Set(&config, "runtime.parallel", "auto"); err != nil {
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

func TestSettingsRejectPreviewAndUnknown(t *testing.T) {
	config := Default()
	if err := Set(&config, "tail-call", "on"); err == nil {
		t.Fatal("preview-only feature was enabled")
	}
	if err := Set(&config, "not-a-setting", "on"); err == nil {
		t.Fatal("unknown setting was accepted")
	}
	if err := Set(&config, "runtime.parallel", "many"); err == nil {
		t.Fatal("invalid parallel default was accepted")
	}
}

func TestResetRestoresBuiltInValue(t *testing.T) {
	config := Default()
	if err := Set(&config, "features.simd", "off"); err != nil {
		t.Fatal(err)
	}
	if err := Reset(&config, "features.simd"); err != nil {
		t.Fatal(err)
	}
	if !config.Features["simd"] {
		t.Fatal("reset did not restore SIMD")
	}
}
