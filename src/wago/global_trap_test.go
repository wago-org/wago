package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	wruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGlobalWritesSurviveTrap(t *testing.T) {
	for _, regABI := range []bool{false, true} {
		for _, typ := range []wasm.ValType{wasm.I32, wasm.I64} {
			constOp, addOp, typeByte := byte(0x41), byte(0x6a), byte(0x7f)
			if typ == wasm.I64 {
				constOp, addOp, typeByte = 0x42, 0x7c, 0x7e
			}
			mod := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType(nil, []wasm.ValType{typ}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
				wasmtest.Section(6, wasmtest.Vec([]byte{typeByte, 1, constOp, 1, 0x0b})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("write", 0, 0), wasmtest.ExportEntry("get", 0, 1))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
					0x02, 0x40, 0x03, 0x40, // block; loop
					0x20, 0, 0x45, 0x0d, 1, // exit when count == 0
					0x23, 0, constOp, 1, addOp, 0x24, 0, // global++
					0x20, 0, 0x41, 1, 0x6b, 0x21, 0, // count--
					0x0c, 0, 0x0b, 0x0b, // repeat; end loop; end block
					0x00, 0x0b, // trap after the writes
				}), wasmtest.Code([]byte{0x23, 0, 0x0b}))),
			)
			c, err := NewRuntimeConfig().WithOptimization("reg-abi", regABI).Compile(mod)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = c.Close() })
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = in.Close() })
			for _, want := range []uint64{4, 7} {
				_, err = in.Invoke("write", 3)
				var trap *wruntime.TrapError
				if !errors.As(err, &trap) || trap.Code != wruntime.TrapUnreachable {
					t.Fatalf("write: %v, want unreachable trap", err)
				}
				got, err := in.Invoke("get")
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("regABI=%v type=%v: get=%v, %v; want %d", regABI, typ, got, err, want)
				}
			}
		}
	}
}
