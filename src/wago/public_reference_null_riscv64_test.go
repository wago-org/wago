//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64NullReferenceCarrierModule(vector bool) []byte {
	typ := wasm.I64
	constant := []byte{0x42, 0x7f}
	if vector {
		typ = wasm.V128
		constant = append([]byte{0xfd, 12}, make([]byte, 16)...)
		for i := 2; i < len(constant); i++ {
			constant[i] = byte(i * 17)
		}
	}
	// Fill the scalar or SWAR local bound with one numeric parameter and
	// null-initialized full-width reference locals.
	last := uint32(255)
	if vector {
		last = 127
	}
	body := append([]byte{1}, wasmtest.ULEB(last)...)
	body = append(body, 0x70, 0x20)
	body = append(body, wasmtest.ULEB(last)...)
	body = append(body, 0xd1, 0x20, 0, 0x04, 0x70, 0xd0, 0x70, 0x05, 0x02, 0x70, 0x20)
	body = append(body, wasmtest.ULEB(last)...)
	body = append(body, 0x0c, 0, 0x0b, 0x0b, 0x22)
	body = append(body, wasmtest.ULEB(last-1)...)
	body = append(body, 0xd0, 0x70, 0x20, 0, 0x1c, 1, 0x70, 0xd1, 0x71, 0x20)
	body = append(body, wasmtest.ULEB(last-1)...)
	// Preserve the reference below a mixed scalar/vector call and its result.
	body = append(body, constant...)
	body = append(body, 0x10, 1, 0x1a, 0xd1, 0x71, 0x10, 2, 0x71, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{3, 0, 1, 2}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("carrier", 0, 0), wasmtest.ExportEntry("leaf", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...), wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0xd0, 0x70, 0xd1, 0x0b}))),
	)
}

func TestRV64PublicNullReferenceCarriersWithoutState(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				c := rv64TableStageCompile(t, fmt.Sprintf("ref-null-carriers-vector%t", vector), rv64NullReferenceCarrierModule(vector), artifact)
				rt := NewRuntime()
				defer rt.Close()
				in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
				defer in.Close()
				if !rv64HeaderOnlyContext(in) || in.tableDescPtr != 0 || len(in.globalCells) != 0 || !in.usesIndependentExecution() {
					t.Fatal("pure reference carrier storage/entry contract")
				}
				fn, err := in.WasmFunc("carrier")
				if err != nil {
					t.Fatal(err)
				}
				for _, input := range []uint64{0, 1, 0xdeadbeef00000000, ^uint64(0)} {
					importedGlobalInvoke(t, in, "carrier", []uint64{1}, input)
					got, err := fn.Invoke(input)
					if err != nil || !slices.Equal(got, []uint64{1}) {
						t.Fatal("prepared reference carrier", got, err)
					}
					session, err := fn.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					got, err = session.Invoke1(input)
					session.Close()
					if err != nil || !slices.Equal(got, []uint64{1}) {
						t.Fatal("session reference carrier", got, err)
					}
				}
				importedGlobalInvoke(t, in, "leaf", []uint64{1})
			})
		}
	}
}

func TestRV64PublicNullReferenceCarrierFeatureGate(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source feature admission")
	}
	for _, vector := range []bool{false, true} {
		c, err := Compile(NewRuntimeConfig().WithFeature(CoreFeatureReferenceTypes, false), rv64NullReferenceCarrierModule(vector))
		if c != nil {
			c.Close()
		}
		if c != nil || err == nil {
			t.Fatal("disabled reference feature admitted", vector, err)
		}
	}
}
