package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/cli/internal/project"
	pluginbuild "github.com/wago-org/wago/cli/manager/internal/plugin/build"
)

func TestPluginBuildFileHelpers(t *testing.T) {
	dir := t.TempDir()
	input := pluginbuild.Input{
		ProviderImports: []string{"example.com/a/register", "example.com/z/register"},
		Selections:      []project.PluginSelection{{ID: "example.com/a", DefinitionDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Direct: true, Dependencies: map[string]string{}}},
	}
	if err := pluginbuild.WriteMain(dir, input, pluginBuildConfig()); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil || !strings.Contains(string(body), "runtime.MainWithPluginSet(version, buildIdentity, set)") ||
		!strings.Contains(string(body), "wago.InspectPluginPlan(set)") ||
		!strings.Contains(string(body), "provider0.Providers()") || strings.Contains(string(body), "\t_ \"example.com/a/register\"") {
		t.Fatalf("generated main = %s, %v", body, err)
	}
	first := pluginbuild.Hash(input, pluginBuildConfig())
	if second := pluginbuild.Hash(input, pluginBuildConfig()); first != second {
		t.Fatal("build helper determinism mismatch")
	}
}
