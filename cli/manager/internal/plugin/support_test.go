package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pluginbuild "github.com/wago-org/wago/cli/manager/internal/plugin/build"
)

func TestRuntimePluginLoaderNames(t *testing.T) {
	if got := strings.TrimPrefix("github.com/wago-org/wasi", "github.com/"); got != "wago-org/wasi" {
		t.Fatalf("canonical plugin name = %q", got)
	}
}

func TestPluginBuildFileHelpers(t *testing.T) {
	dir := t.TempDir()
	if err := pluginbuild.WriteMain(dir, []string{"example.com/z", "example.com/a"}, pluginBuildConfig()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil ||
		!strings.Contains(string(b), "runtime.Main(version)") ||
		!strings.Contains(string(b), "example.com/a/register") ||
		strings.Index(string(b), "example.com/a/register") > strings.Index(string(b), "example.com/z/register") {
		t.Fatalf("generated main = %s, %v", b, err)
	}
	if pluginbuild.Hash([]string{"b", "a"}, pluginBuildConfig()) != pluginbuild.Hash([]string{"a", "b"}, pluginBuildConfig()) {
		t.Fatal("build helper determinism mismatch")
	}
}
