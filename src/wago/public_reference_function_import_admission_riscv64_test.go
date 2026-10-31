//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceImportCopy(c *Compiled) *Compiled {
	v := rv64ReferenceStateCopy(c)
	v.Imports = slices.Clone(v.Imports)
	v.importFuncSigs = slices.Clone(v.importFuncSigs)
	for i := range v.importFuncSigs {
		v.importFuncSigs[i].Params = slices.Clone(v.importFuncSigs[i].Params)
		v.importFuncSigs[i].Results = slices.Clone(v.importFuncSigs[i].Results)
	}
	v.FuncTypeID = slices.Clone(v.FuncTypeID)
	v.Exports = maps.Clone(v.Exports)
	return v
}

func rv64ReferenceUnusedNumericImport(wide bool) []byte {
	params := []wasm.ValType{wasm.V128}
	if wide {
		params = make([]wasm.ValType, 65)
		for i := range params {
			params[i] = wasm.I32
		}
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, nil), wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}), wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))), wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("unused")...), 0, 0))), wasmtest.Section(3, []byte{2, 1, 2}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x10, 2, 0x1a, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b}))))
}

func TestRV64PublicReferenceFunctionImportsRejectHostShapes(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, wide := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-wide%t", artifact, wide), func(t *testing.T) {
				c := rv64NativeTailCompile(t, fmt.Sprintf("reference-imports-unused-wide%t", wide), rv64ReferenceUnusedNumericImport(wide), artifact)
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rt.Close()
				mod, err := rt.Module(c)
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
				imports := NewImports().Function("env", "unused", HostCallFunc(func(HostCall) { t.Fatal("unsupported unused host called") }))
				in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
				if wide {
					if err != nil || in == nil {
						t.Fatal("wide unused scalar host rejected", err)
					}
					if got, err := in.Invoke("run"); err != nil || len(got) != 0 {
						t.Fatal("unused scalar host execution", got, err)
					}
					in.Close()
				} else {
					if in != nil || err == nil || !strings.Contains(err.Error(), "v128 host callbacks are not supported") {
						t.Fatal("unsupported vector host published", err)
					}
					if cc := c.loadCodeCache(); cc != nil && (cc.sealed || cc.refs != 0) {
						t.Fatal("unsupported vector host acquired executable mapping")
					}
				}
			})
		}
	}
}
