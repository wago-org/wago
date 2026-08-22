//go:build !tinygo

package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/tests/wasmtest"
)

func pluginGCImportOnlyModule() []byte {
	arrayType := []byte{0x5e, 0x78, 0x01}                  // (array (mut i8))
	importType := []byte{0x60, 0x00, 0x01, 0x63, 0x00} // () -> (ref null 0)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, importType)),
		wasmtest.Section(2, wasmtest.Vec(pluginGCImport("plugin_gc", "null_result", 1))),
	)
}

func TestPluginGCHostImportOnlyModuleUsesEmptyExactRootSet(t *testing.T) {
	requireCompleteCore3Backend(t)
	rt, _ := newPluginGCTestRuntime(t)
	defer rt.Close()

	mod, err := rt.Compile(pluginGCImportOnlyModule())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()

	admission := mod.c.GCNativeRootAdmission()
	if !admission.Required || !admission.Exact || admission.Callsites != 0 || admission.Safepoints != 0 {
		t.Fatalf("import-only native root admission = %+v; want exact empty root set", admission)
	}
	if !mod.c.needsRuntimeGCCollectorDomain() {
		t.Fatal("import-only GC plugin module did not request a Runtime GC domain")
	}

	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("import-only GC plugin module failed instantiation: %v", err)
	}
	defer in.Close()
	if in.gc == nil || in.gcInvocationDomain() == nil {
		t.Fatal("import-only GC plugin instance has no Runtime GC domain")
	}
}
