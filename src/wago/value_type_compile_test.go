package wago

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

// This synthetic module exercises imported/defined globals, imported/defined
// tables, and passive typed elements, with indexed Core 3 reference shapes.
func mixedValueTypeModule(n, cluster int) []byte {
	var types, imports, globals, tables, elems [][]byte
	for i := 0; i < n; i++ {
		types = append(types, []byte{0x60, 0, 0})
		ref := append([]byte{0x63}, wasmtest.SLEB32(int32(i))...)
		init := append([]byte{0xd0}, wasmtest.SLEB32(int32(i))...)
		init = append(init, 0x0b)
		for j := 0; j < cluster; j++ {
			name := fmt.Sprintf("g%d_%d", i, j)
			imp := append([]byte{1, 'm', byte(len(name))}, []byte(name)...)
			imp = append(imp, 3)
			imp = append(imp, ref...)
			imports = append(imports, append(imp, 0))
			name = fmt.Sprintf("t%d_%d", i, j)
			imp = append([]byte{1, 'm', byte(len(name))}, []byte(name)...)
			imp = append(imp, 1)
			imp = append(imp, ref...)
			imports = append(imports, append(imp, 0, 0))
			global := append(slices.Clone(ref), 0)
			globals = append(globals, append(global, init...))
			tables = append(tables, append(slices.Clone(ref), 0, 0))
			elem := append([]byte{5}, ref...)
			elems = append(elems, append(elem, 0))
		}
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(4, wasmtest.Vec(tables...)), wasmtest.Section(6, wasmtest.Vec(globals...)), wasmtest.Section(9, wasmtest.Vec(elems...)))
}

func valueTypeCompileSources(tb testing.TB) map[string][]byte {
	tb.Helper()
	sources := map[string][]byte{
		"synthetic_mixed_128x4":     mixedValueTypeModule(128, 4),
		"synthetic_globals_128x16":  indexedGlobalClusterModule(128, 16),
		"synthetic_globals_1024x16": indexedGlobalClusterModule(1024, 16),
	}
	// Remove the second pass to measure genuinely all-unique descriptors.
	var types, globals [][]byte
	for i := 0; i < 1024; i++ {
		types = append(types, []byte{0x60, 0, 0})
		g := append([]byte{0x63}, wasmtest.SLEB32(int32(i))...)
		g = append(g, 0, 0xd0)
		g = append(g, wasmtest.SLEB32(int32(i))...)
		globals = append(globals, append(g, 0x0b))
	}
	sources["synthetic_unique_1024"] = wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(6, wasmtest.Vec(globals...)))
	for name, path := range map[string]string{
		"json_as":     "../../corpus/workloads/assemblyscript/json-as.wasm",
		"linked_list": "../../corpus/workloads/compute/linked_list.wasm",
		"nanosvg":     "../../corpus/repro/nanosvg/reduced.wasm",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			tb.Fatal(err)
		}
		sources[name] = data
	}
	return sources
}

func BenchmarkCompileValueTypeWorkloads(b *testing.B) {
	if !SupportedFeatures().IsEnabled(CoreFeaturesV3) {
		b.Skip("requires Core 3 features")
	}
	for name, source := range valueTypeCompileSources(b) {
		b.Run(name, func(b *testing.B) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, err := Compile(cfg, source)
				if err != nil {
					b.Fatal(err)
				}
				if err := c.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
