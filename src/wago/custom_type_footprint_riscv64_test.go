//go:build linux && riscv64 && !tinygo && !wago_precompiled

package wago

import (
	"context"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func rv64CustomWarmAllocations(t *testing.T, in *Instance, want uint64) float64 {
	t.Helper()
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	session, err := fn.OpenSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	return testing.AllocsPerRun(64, func() {
		got, err := session.Invoke0()
		if err != nil || len(got) != 1 || got[0] != want {
			panic(fmt.Sprintf("custom warm checksum failed: got=%v want=%d err=%v", got, want, err))
		}
	})
}
func TestRV64CustomWidePreparedFootprint(t *testing.T) {
	plain := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0x0b}))))
	control, err := Compile(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	controlIn, err := Instantiate(control)
	if err != nil {
		t.Fatal(err)
	}
	defer controlIn.Close()
	baseline := rv64CustomWarmAllocations(t, controlIn, 7)
	for _, size := range []int32{32, 64, 128, 256, 512} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			if err := rt.Use(rv64WideDataExtension{size: size, carrier: WasmI32}); err != nil {
				t.Fatal(err)
			}
			mod, blob := rv64PrepareWideData(t, rt, 0x7f, []byte{0x10, 0, 0x10, 2, 0x0b})
			defer mod.Close()
			loaded, err := LoadTrustedArtifact(blob)
			if err != nil {
				t.Fatal(err)
			}
			loadedMod, err := rt.AdoptModule(loaded)
			if err != nil {
				t.Fatal(err)
			}
			defer loadedMod.Close()
			for _, entry := range []struct {
				name string
				mod  *Module
			}{{"source", mod}, {"artifact", loadedMod}} {
				t.Run(entry.name, func(t *testing.T) {
					in, err := rt.Instantiate(context.Background(), entry.mod)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					allocations := rv64CustomWarmAllocations(t, in, rv64WideSum(size, 1))
					t.Logf("RV64 emulation: size=%d native-code-bytes=%d artifact-bytes=%d warm-session-allocations=%g plain-control-allocations=%g", size, len(entry.mod.c.code), len(blob), allocations, baseline)
					if allocations > baseline {
						t.Fatalf("custom lowering adds warm allocations: custom=%g control=%g", allocations, baseline)
					}
				})
			}
		})
	}
}
