//go:build linux && riscv64 && !wago_precompiled && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64TypedFunctionReferenceSignatureTransport(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), []byte{0x60, 1, 0x63, 0, 1, 0x63, 0})),
		wasmtest.Section(3, []byte{3, 0, 1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(9, []byte{1, 3, 0, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 41, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0xd2, 0, 0x10, 1, 0x14, 0, 0x0b}))),
	)
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			compiled := rv64NativeTailCompile(t, "typed-function-signature-transport", source, artifact)
			instance, err := Instantiate(compiled)
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			got, err := instance.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != 41 {
				t.Fatal(got, err)
			}
		})
	}
}

func TestRV64TypedFunctionGlobalTransport(t *testing.T) {
	for _, mutable := range []bool{false, true} {
		mutability := byte(0)
		body := []byte{0x23, 0, 0x14, 0, 0x0b}
		if mutable {
			mutability = 1
			body = append([]byte{0xd0, 0, 0x24, 0, 0xd2, 0, 0x24, 0}, body...)
		}
		source := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, []byte{2, 0, 0}),
			wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, mutability, 0xd2, 0, 0x0b})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
			wasmtest.Section(9, []byte{1, 3, 0, 1, 0}),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x0b}), wasmtest.Code(body))),
		)
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("mutable%t-artifact%t", mutable, artifact), func(t *testing.T) {
				compiled := rv64NativeTailCompile(t, "typed-function-global-transport", source, artifact)
				instance, err := Instantiate(compiled)
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				got, err := instance.Invoke("run")
				if err != nil || len(got) != 1 || got[0] != 42 {
					t.Fatal(got, err)
				}
				allocations := testing.AllocsPerRun(1000, func() {
					result, callErr := instance.Invoke("run")
					if callErr != nil || len(result) != 1 || result[0] != 42 {
						panic("typed global call failed")
					}
				})
				t.Logf("typed global warm invoke: %g allocs/op (1000 QEMU iterations)", allocations)
				if allocations != 0 {
					t.Fatalf("typed global warm invoke allocated: %g", allocations)
				}
			})
		}
	}
}
