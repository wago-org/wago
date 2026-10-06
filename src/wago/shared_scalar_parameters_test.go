//go:build (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestSharedScalarEightIncomingParameters(t *testing.T) {
	// All integer argument registers are live. Keep an older parameter read on
	// the stack while replacing that local, then reconcile across both if edges.
	params := []wasm.ValType{wasm.I64, wasm.I32, wasm.I64, wasm.I32, wasm.I64, wasm.I32, wasm.I64, wasm.I32}
	ft := &wasm.CompType{Params: params, Results: []wasm.ValType{wasm.I64}}
	body := []byte{0x20, 6, 0x42, 7, 0x21, 6, 0x20, 7, 0x04, 0x7e, 0x20, 0, 0x05, 0x20, 2, 0x0b, 0x7c, 0x20, 6, 0x7c, 0x20, 8, 0x7c, 0x0f, 0x0b}
	types := append(append([]wasm.ValType{}, params...), wasm.I64)
	if !shared.AdmitScalar(body, ft, types).Eligible {
		t.Fatal("boundary fixture fell back")
	}
	code := append([]byte{1, 1, 0x7e}, body...)
	module := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(ft.Params, ft.Results))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))))
	for _, regABI := range []bool{false, true} {
		t.Run(fmt.Sprintf("regABI=%v", regABI), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithOptimization("reg-abi", regABI), module)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for _, condition := range []uint64{0, 1, 0xffffffff} {
				args := []uint64{1 << 40, 0xaaaaaaaa, 1 << 50, 0xbbbbbbbb, 1 << 60, 0xcccccccc, ^uint64(0), condition}
				selected := args[0]
				if condition == 0 {
					selected = args[2]
				}
				want := args[6] + selected + 7
				got, err := in.Invoke("run", args...)
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("args=%x got=%x err=%v want=%x", args, got, err, want)
				}
			}
		})
	}
}

func TestSharedScalarRawI32Return(t *testing.T) {
	// Incoming carriers are uint64. The low i32 bits must be returned even when
	// get/tee returns the incoming/result register without any arithmetic.
	for _, body := range [][]byte{
		{0x20, 0, 0x0b},
		{0x20, 0, 0x22, 0, 0x0f, 0x0b},
		{0x20, 0, 0x22, 1, 0x0b},
	} {
		for _, regABI := range []bool{false, true} {
			c, err := Compile(NewRuntimeConfig().WithOptimization("reg-abi", regABI), scalarPilotModule(wasm.I32, body, 1))
			if err != nil {
				t.Fatal(err)
			}
			in, err := Instantiate(c)
			if err != nil {
				c.Close()
				t.Fatal(err)
			}
			for _, x := range []uint64{0x1234567800000000, 0xdeadbeef00000002, ^uint64(0), 1 << 63} {
				got, err := in.Invoke("run", x)
				if err != nil || len(got) != 1 || got[0] != uint64(uint32(x)) {
					t.Fatalf("body=%x regABI=%v x=%x got=%x err=%v", body, regABI, x, got, err)
				}
			}
			in.Close()
			c.Close()
		}
	}
}
