package wagocli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/wago-org/wago"
)

func TestWagoSchemaTracksPluginCapabilities(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		ID   string `json:"$id"`
		Defs map[string]struct {
			Enum []string `json:"enum"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatalf("invalid schema.json: %v", err)
	}
	if schema.ID != manifestSchemaURI {
		t.Fatalf("schema $id = %q", schema.ID)
	}
	got := append([]string(nil), schema.Defs["pluginCapability"].Enum...)
	want := []string{
		string(wago.PluginHostImports),
		string(wago.PluginHostEnvironment),
		string(wago.PluginRuntimeHooks),
		string(wago.PluginCompileHooks),
		string(wago.PluginInstanceHooks),
		string(wago.PluginInvokeHooks),
		string(wago.PluginManagedInstances),
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema capabilities = %v, runtime capabilities = %v", got, want)
	}
}
