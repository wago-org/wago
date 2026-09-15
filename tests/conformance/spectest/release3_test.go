package spectest

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

var release3Sentinels = []string{
	"i32.wast",
	"const.wast",
	"return_call.wast",
	"call_ref.wast",
	"gc/struct.wast",
	"exceptions/throw.wast",
	"multi-memory/memory-multi.wast",
	"memory64/memory64.wast",
	"memory64/table64.wast",
	"relaxed-simd/relaxed_laneselect.wast",
}

func writeRelease3Sentinels(t *testing.T, checkout string) string {
	t.Helper()
	core := filepath.Join(checkout, "test", "core")
	for _, name := range release3Sentinels {
		path := filepath.Join(core, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return core
}

func TestDiscoverRelease3RequiresOfficialCoreLayout(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := DiscoverRelease3(missing); err == nil || !strings.Contains(err.Error(), "WebAssembly 3.0") {
		t.Fatalf("DiscoverRelease3(%q) error = %v, want a clear WebAssembly 3.0 corpus error", missing, err)
	}

	checkout := t.TempDir()
	if err := os.WriteFile(filepath.Join(checkout, "i32.wast"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverRelease3(checkout); err == nil {
		t.Fatal("DiscoverRelease3 accepted the legacy testsuite root instead of test/core")
	}

	core := writeRelease3Sentinels(t, checkout)
	got, err := DiscoverRelease3(checkout)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := make([]string, len(release3Sentinels))
	for i, name := range release3Sentinels {
		wantFiles[i] = strings.TrimSuffix(filepath.FromSlash(name), ".wast")
	}
	// Discovery is lexically sorted, independent of fixture creation order.
	sort.Strings(wantFiles)
	want := Release3Suite{CoreDir: core, Files: wantFiles}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiscoverRelease3() = %#v, want %#v", got, want)
	}
}

func TestDiscoverRelease3RejectsPartialCore(t *testing.T) {
	checkout := t.TempDir()
	core := writeRelease3Sentinels(t, checkout)
	if err := os.Remove(filepath.Join(core, "gc", "struct.wast")); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverRelease3(checkout); err == nil || !strings.Contains(err.Error(), "missing gc") {
		t.Fatalf("DiscoverRelease3(partial) error = %v, want missing mandatory-family sentinel", err)
	}
}
