package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReviewRejectOtherModuleAsInstalledSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".wago", "src")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/wago-org/wago-plugin-example\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := InstalledSource(); got != "" {
		t.Fatalf("unrelated module accepted as Wago source: %s", got)
	}
}

func TestReviewRejectOtherCurrentModule(t *testing.T) {
	t.Setenv("WAGO_SRC", "")
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/wago-org/wago-plugin-example\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if got, err := ModuleDir(); err == nil && got == dir {
		t.Fatalf("unrelated current module accepted as Wago source: %s", got)
	}
}

func TestWagoModuleDirective(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"module github.com/wago-org/wago\n", true},
		{"module\t\"github.com/wago-org/wago\" // engine\n", true},
		{"// module github.com/wago-org/wago\nmodule example.com/plugin\n", false},
		{"module github.com/wago-org/wago-plugin-example\n", false},
		{"module github.com/wago-org/wago/v2\n", false},
		{"require github.com/wago-org/wago v1.0.0\n", false},
	} {
		if got := isWagoModule([]byte(tc.text)); got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.text, got, tc.want)
		}
	}
}
