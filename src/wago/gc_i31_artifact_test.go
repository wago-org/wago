package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func i31ElementArtifactModule(heap wasm.AbsHeapType, passive, importedGlobal bool, expr []byte) []byte {
	refType := wasm.RefVal(wasm.AbsRef(heap))
	types := [][]byte{wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})}
	funcs := [][]byte{wasmtest.ULEB(0)}
	exports := [][]byte{wasmtest.ExportEntry("get", byte(wasm.ExternFunc), 0)}
	codes := [][]byte{}
	get := []byte{0x20, 0x00, 0x25, 0x00} // local.get 0; table.get 0
	if heap == wasm.HeapAny {
		get = append(get, 0xfb, 0x17, byte(wasm.HeapI31)) // ref.cast i31
	}
	get = append(get, 0xfb, 0x1e, 0x0b) // i31.get_u; end
	codes = append(codes, wasmtest.Code(get))

	var elem []byte
	if passive {
		types = append(types, wasmtest.FuncType(nil, nil))
		funcs = append(funcs, wasmtest.ULEB(1))
		exports = append(exports, wasmtest.ExportEntry("init", byte(wasm.ExternFunc), 1))
		codes = append(codes, wasmtest.Code([]byte{
			0x41, 0x00, // destination
			0x41, 0x00, // source
			0x41, 0x01, // length
			0xfc, 0x0c, 0x00, 0x00, // table.init element 0 table 0
			0x0b,
		}))
		elem = append([]byte{0x05}, wasm.MustEncodeValType(refType))
	} else {
		elem = []byte{0x06, 0x00, 0x41, 0x00, 0x0b} // active table 0 at offset 0
		elem = append(elem, wasm.MustEncodeValType(refType))
	}
	elem = append(elem, wasmtest.Vec(expr)...)
	table := []byte{wasm.MustEncodeValType(refType), 0x00, 0x01}

	moduleSections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...))}
	if importedGlobal {
		moduleSections = append(moduleSections, wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "value", wasm.I32, false))))
	}
	moduleSections = append(moduleSections,
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(4, wasmtest.Vec(table)),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(9, wasmtest.Vec(elem)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
	return wasmtest.Module(moduleSections...)
}

func i31GlobalArtifactModule(expr []byte) []byte {
	refType := wasm.RefVal(wasm.AbsRef(wasm.HeapI31))
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(refType, false, expr))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0xfb, 0x1e, 0x0b}))),
	)
}

func TestI31ExecutionProductPublicArtifactLifecycle(t *testing.T) {
	requireCompleteCore3Backend(t)
	direct := []byte{0x41, 0x07, 0xfb, 0x1c, 0x0b}
	deferred := []byte{0x41, 0x01, 0x41, 0x02, 0x6a, 0xfb, 0x1c, 0x0b}
	globalWrap := []byte{0x23, 0x00, 0xfb, 0x1c, 0x0b}

	for _, tc := range []struct {
		name    string
		module  []byte
		imports Imports
		passive bool
		getArgs []uint64
		want    uint64
	}{
		{name: "exact-active-direct", module: i31ElementArtifactModule(wasm.HeapI31, false, false, direct), getArgs: []uint64{I32(0)}, want: 7},
		{name: "exact-passive-direct", module: i31ElementArtifactModule(wasm.HeapI31, true, false, direct), passive: true, getArgs: []uint64{I32(0)}, want: 7},
		{name: "exact-active-imported-global", module: i31ElementArtifactModule(wasm.HeapI31, false, true, globalWrap), imports: Imports{"env.value": GlobalImport{Type: ValI32, Bits: 11}}, getArgs: []uint64{I32(0)}, want: 11},
		{name: "exact-active-deferred", module: i31ElementArtifactModule(wasm.HeapI31, false, false, deferred), getArgs: []uint64{I32(0)}, want: 3},
		{name: "anyref-active-direct", module: i31ElementArtifactModule(wasm.HeapAny, false, false, direct), getArgs: []uint64{I32(0)}, want: 7},
		{name: "global-direct", module: i31GlobalArtifactModule(direct), want: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(tc.module)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			defer compiled.Close()
			loaded := publicArtifactRoundTrip(t, compiled)
			defer loaded.Close()
			instance, err := Instantiate(loaded, InstantiateOptions{Imports: tc.imports})
			if err != nil {
				t.Fatalf("instantiate loaded artifact: %v", err)
			}
			defer instance.Close()
			if tc.passive {
				if _, err := instance.Invoke("init"); err != nil {
					t.Fatalf("initialize passive element: %v", err)
				}
			}
			result, err := instance.Invoke("get", tc.getArgs...)
			if err != nil || len(result) != 1 || result[0] != tc.want {
				t.Fatalf("loaded value = %v, %v; want [%d], nil", result, err, tc.want)
			}
		})
	}
}
