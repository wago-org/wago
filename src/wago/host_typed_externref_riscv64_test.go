//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"strings"
	"testing"
)

func rv64HostTypedExternResultModule(typ wasm.ValType) []byte {
	resultType := []byte{0x60, 0, 1}
	if encoded, ok := wasm.EncodeValType(typ); ok {
		resultType = append(resultType, encoded)
	} else {
		marker := byte(0x64)
		if typ.Ref().Nullable() {
			marker = 0x63
		}
		resultType = append(resultType, marker, byte(typ.Ref().Heap().Abs()))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(resultType, wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "result", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("marker", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x1a, 0x41, 1, 0x24, 0, 0x0b}))),
	)
}
func TestRV64HostTypedExternResultBeforeResume(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, tc := range []struct {
			name                     string
			heap                     wasm.AbsHeapType
			nullable, nonnull, valid bool
		}{
			{"bottom-null", wasm.HeapNoExtern, true, false, true},
			{"bottom-token", wasm.HeapNoExtern, true, true, false},
			{"nonnull-null", wasm.HeapExtern, false, false, false},
			{"nonnull-token", wasm.HeapExtern, false, true, true},
		} {
			t.Run(fmt.Sprintf("%s/artifact%v", tc.name, artifact), func(t *testing.T) {
				typ := wasm.RefVal(wasm.AbsRef(tc.heap).WithNullable(tc.nullable))
				cfg := rv64NativeTailConfig().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTailCall | CoreFeatureTypedFunctionReferences)
				cfg.railshotState.i31Products = true
				c, compileErr := cfg.Compile(rv64HostTypedExternResultModule(typ))
				if compileErr != nil {
					t.Fatal(compileErr)
				}
				defer c.Close()
				if artifact {
					blob, err := c.MarshalBinary()
					if err != nil {
						t.Fatal(err)
					}
					loaded, err := LoadTrustedArtifact(blob)
					if err != nil {
						t.Fatal(err)
					}
					defer loaded.Close()
					c = loaded
				}
				rt := NewRuntime(WithRuntimeConfig(cfg))
				defer rt.Close()
				ref := ExternRef{}
				if tc.nonnull {
					ref = issueExternref(t, rt, "typed host result")
				}
				calls := 0
				in := rv64NativeTokenInstantiate(t, rt, c, testImports("env.result", func(call HostCall) { calls++; call.SetExternRef(0, ref) }))
				got, err := in.Invoke("run")
				if tc.valid {
					if err != nil {
						t.Fatalf("valid result=%v/%v", got, err)
					}
				} else if err == nil || got != nil || !strings.Contains(err.Error(), "exact structural type") {
					t.Fatalf("invalid result=%v/%v", got, err)
				}
				marker, markerErr := in.Global("marker")
				want := uint64(0)
				if tc.valid {
					want = 1
				}
				if markerErr != nil || marker != want || calls != 1 {
					t.Fatalf("marker=%v/%v calls=%d", marker, markerErr, calls)
				}
			})
		}
	}
}
