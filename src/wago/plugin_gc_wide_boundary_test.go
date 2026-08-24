//go:build !tinygo

package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

type pluginGCWideBoundaryTestPlugin struct{}

func pluginGCWideBoundaryTestProvider() PluginProvider {
	def := testDefinition("example.com/plugin-gc-wide-boundary")
	def.Authorities = []AuthorityRequest{{
		Name:   AuthorityHostImportDefine,
		Mode:   AuthorityRequired,
		Reason: "define wide GC-reference host imports",
		Scope:  AuthorityScope{Modules: []string{"plugin_gc_wide"}},
	}}
	return PluginProvider{
		Definition: def,
		New:        func() Plugin { return pluginGCWideBoundaryTestPlugin{} },
	}
}

func (pluginGCWideBoundaryTestPlugin) Register(reg *Registrar) error {
	imports, err := reg.HostImports()
	if err != nil {
		return err
	}
	module, err := imports.Module("plugin_gc_wide")
	if err != nil {
		return err
	}
	module.Func("wide_result", func(_ HostModule, _, results []uint64) {
		results[0] = 0
		results[1] = 1
		results[2] = 2
		results[3] = 3
		results[4] = 4
	}).Results(ValAnyRef, ValI32, ValI64, ValI32, ValI32)
	module.Func("null_result", func(_ HostModule, _, results []uint64) {
		results[0] = 0
	}).Results(ValAnyRef)
	params := make([]ValType, 11)
	for i := range params {
		params[i] = ValI32
	}
	module.Func("scalar_11", func(HostModule, []uint64, []uint64) {}).Params(params...)
	return nil
}

func newPluginGCWideBoundaryRuntime(t testing.TB) *Runtime {
	t.Helper()
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	if err := rt.LoadPlugins(context.Background(), testSet(t, pluginGCWideBoundaryTestProvider())); err != nil {
		rt.Close()
		t.Fatal(err)
	}
	return rt
}

func pluginGCWideResultModule() []byte {
	arrayType := []byte{0x5e, 0x78, 0x01} // (array (mut i8))
	wideType := []byte{0x60, 0x00, 0x05, 0x63, 0x00, 0x7f, 0x7e, 0x7f, 0x7f}
	scalar11Type := append([]byte{0x60, 0x0b}, []byte{0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x00}...)
	runType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	body := []byte{
		0x10, 0x00, // call wide_result
		0x1a, 0x1a, 0x1a, 0x1a, 0x1a, // drop all five results
		0x41, 0x07, // i32.const 7
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, wideType, scalar11Type, runType)),
		wasmtest.Section(2, wasmtest.Vec(
			pluginGCImport("plugin_gc_wide", "wide_result", 1),
			pluginGCImport("plugin_gc_wide", "scalar_11", 2),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(3))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func pluginGCFourResultCallerModule() []byte {
	arrayType := []byte{0x5e, 0x78, 0x01}
	importType := []byte{0x60, 0x00, 0x01, 0x63, 0x00}
	runType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32})
	body := []byte{
		0x10, 0x00, // call null_result
		0xd1, // ref.is_null
		0x41, 0x02,
		0x41, 0x03,
		0x41, 0x04,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, importType, runType)),
		wasmtest.Section(2, wasmtest.Vec(pluginGCImport("plugin_gc_wide", "null_result", 1))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestPluginGCHostBoundaryWideWrapperABI(t *testing.T) {
	requireCompleteCore3Backend(t)
	rt := newPluginGCWideBoundaryRuntime(t)
	defer rt.Close()

	for name, wasmBytes := range map[string][]byte{
		"wide-host-result-and-unrelated-wide-import": pluginGCWideResultModule(),
		"four-result-caller":                         pluginGCFourResultCallerModule(),
	} {
		t.Run(name, func(t *testing.T) {
			mod, err := rt.Compile(wasmBytes)
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			admission := mod.c.GCNativeRootAdmission()
			if !admission.Required || !admission.Exact || admission.Callsites == 0 {
				t.Fatalf("wide GC boundary root admission = %+v", admission)
			}
			in, err := rt.Instantiate(context.Background(), mod)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			values, err := in.Call(context.Background(), "run")
			if err != nil {
				t.Fatal(err)
			}
			if name == "wide-host-result-and-unrelated-wide-import" {
				if len(values) != 1 || values[0].I32() != 7 {
					t.Fatalf("run = %v; want i32(7)", values)
				}
				return
			}
			if len(values) != 4 || values[0].I32() != 1 || values[1].I32() != 2 || values[2].I32() != 3 || values[3].I32() != 4 {
				t.Fatalf("run = %v; want [1 2 3 4]", values)
			}
		})
	}
}
