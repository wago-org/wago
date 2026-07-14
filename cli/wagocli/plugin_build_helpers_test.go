package wagocli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPluginBuildHelperParsingAndGitignore(t *testing.T) {
	for _, tc := range []struct{ spec, module, version string }{
		{"example.test/plugin@v1.2.3", "example.test/plugin", "v1.2.3"},
		{"example.test/plugin", "example.test/plugin", ""},
		{"@scope/plugin", "@scope/plugin", ""},
	} {
		module, version := splitModuleVersion(tc.spec)
		if module != tc.module || version != tc.version {
			t.Fatalf("splitModuleVersion(%q) = %q, %q", tc.spec, module, version)
		}
	}
	if plural(1) != "" || plural(0) != "s" || plural(2) != "s" {
		t.Fatal("plural helper changed")
	}
	for _, tc := range []struct {
		value string
		want  bool
	}{{"1", true}, {"TRUE", true}, {"yes", true}, {"on", true}, {"0", false}, {"", false}} {
		t.Setenv("WAGO_TEST_TRUTHY", tc.value)
		if got := truthyEnv("WAGO_TEST_TRUTHY"); got != tc.want {
			t.Fatalf("truthyEnv(%q) = %v", tc.value, got)
		}
	}

	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	ensureGitignore(".wago/")
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil || string(data) != ".wago/\n" {
		t.Fatalf("created gitignore = %q, %v", data, err)
	}
	ensureGitignore(".wago/")
	data, err = os.ReadFile(".gitignore")
	if err != nil || string(data) != ".wago/\n" {
		t.Fatalf("duplicate gitignore entry = %q, %v", data, err)
	}
}
