package settings

import (
	"os"
	"path/filepath"
	"strings"
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
	var experimental BoolSetting
	for _, setting := range Experimental() {
		if setting.Available {
			experimental = setting
			break
		}
	}
	if experimental.Key == "" {
		t.Fatal("no available experimental setting")
	}
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
	if err := Set(&config, "not-a-setting", "on", false); err == nil {
		t.Fatal("unknown setting was accepted")
	}
	if err := Set(&config, "runtime.parallel", "many", false); err == nil {
		t.Fatal("invalid parallel default was accepted")
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
