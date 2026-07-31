package plugin

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago"
)

type testExtension struct{}

func (testExtension) Info() wago.ExtensionInfo {
	return wago.ExtensionInfo{ID: "test.report", RequiresCapabilities: []wago.PluginCapability{wago.PluginHostImports}}
}

func (testExtension) Register(registry *wago.Registry) error {
	registry.Capability(wago.CapMetricsWrite)
	registry.ImportModule("env").Func("f", func(wago.HostModule, []uint64, []uint64) {}).
		Params(wago.ValI32).Results(wago.ValI64).Capability(wago.CapMetricsWrite).Docs("counts calls")
	return nil
}

func TestBuild(t *testing.T) {
	result := BuildReport("test.report", testExtension{})
	if result.Name != "test.report" || result.ID != "test.report" ||
		!reflect.DeepEqual(result.RequiresCapabilities, []string{string(wago.PluginHostImports)}) {
		t.Fatalf("report identity = %#v", result)
	}
	if !reflect.DeepEqual(result.Capabilities, []string{string(wago.CapMetricsWrite)}) {
		t.Fatalf("capabilities = %v", result.Capabilities)
	}
	if len(result.Imports) != 1 || result.Imports[0].Module != "env" ||
		result.Imports[0].Name != "f" ||
		result.Imports[0].Capability != string(wago.CapMetricsWrite) ||
		result.Imports[0].Docs != "counts calls" {
		t.Fatalf("imports = %#v", result.Imports)
	}
	if _, err := json.Marshal(result); err != nil {
		t.Fatalf("marshal: %v", err)
	}
}

func TestPresentation(t *testing.T) {
	compatibility := wago.Compatibility{
		Engines:   map[string]string{"wago": ">=1", "tinygo": "*"},
		Platforms: []string{"linux/amd64", "darwin/arm64"},
	}
	if got := CompatibilityDetail(compatibility); got != "engines: tinygo, wago >=1 · platforms: linux/amd64, darwin/arm64" {
		t.Fatalf("compatibility = %q", got)
	}
	if got := Signature([]string{"i32", "i64"}, []string{"f32"}); got != "(i32, i64) -> f32" {
		t.Fatalf("signature = %q", got)
	}
	if got := Signature(nil, nil); got != "()" {
		t.Fatalf("void signature = %q", got)
	}
	if got := strings.Join(engineTerms(map[string]string{"wago": ">=0.1.0", "tinygo": "*", "go": ""}), ", "); got != "go, tinygo, wago >=0.1.0" {
		t.Fatalf("engine terms = %q", got)
	}
}
