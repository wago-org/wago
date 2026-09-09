package spectest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDiscoverRelease2RequiresOfficialCoreLayout(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := DiscoverRelease2(missing); err == nil || !strings.Contains(err.Error(), "WebAssembly 2.0") {
		t.Fatalf("DiscoverRelease2(%q) error = %v, want a clear WebAssembly 2.0 corpus error", missing, err)
	}

	checkout := t.TempDir()
	if err := os.WriteFile(filepath.Join(checkout, "i32.wast"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverRelease2(checkout); err == nil {
		t.Fatal("DiscoverRelease2 accepted the legacy testsuite root instead of test/core")
	}

	core := filepath.Join(checkout, "test", "core")
	if err := os.MkdirAll(filepath.Join(core, "simd"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"i32.wast", "ref_null.wast", "simd/simd_const.wast"} {
		if err := os.WriteFile(filepath.Join(core, filepath.FromSlash(name)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := DiscoverRelease2(checkout)
	if err != nil {
		t.Fatal(err)
	}
	want := Release2Suite{
		CoreDir: core,
		Files:   []string{"i32", "ref_null", filepath.Join("simd", "simd_const")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiscoverRelease2() = %#v, want %#v", got, want)
	}
}

func TestDiscoverRelease2RejectsEmptyCore(t *testing.T) {
	checkout := t.TempDir()
	if err := os.MkdirAll(filepath.Join(checkout, "test", "core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverRelease2(checkout); err == nil || !strings.Contains(err.Error(), "no .wast files") {
		t.Fatalf("DiscoverRelease2(empty) error = %v, want no-.wast-files error", err)
	}
}
