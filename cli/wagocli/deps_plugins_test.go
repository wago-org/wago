package wagocli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wago-org/wago"
)

func TestProjectPluginsParsesCapabilitiesAndOrdering(t *testing.T) {
	dir := t.TempDir()
	manifest := `{
  "dependencies": ["github.com/wago-org/workers"],
  "plugins": [{
    "name": "workers",
    "capabilities": ["instance.manage", "runtime.lifecycle"],
    "after": ["metrics"],
    "config": {"maxWorkers": 4}
  }]
}`
	if err := os.WriteFile(filepath.Join(dir, projectFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := projectPlugins(dir)
	if err != nil {
		t.Fatal(err)
	}
	wantCaps := []wago.PluginCapability{wago.PluginManagedInstances, wago.PluginRuntimeHooks}
	if len(got) != 1 || got[0].Name != "workers" || !reflect.DeepEqual(got[0].Capabilities, wantCaps) || !reflect.DeepEqual(got[0].After, []string{"metrics"}) {
		t.Fatalf("projectPlugins = %#v", got)
	}
	if string(got[0].Config) != `{"maxWorkers":4}` {
		t.Fatalf("config = %s", got[0].Config)
	}
}
