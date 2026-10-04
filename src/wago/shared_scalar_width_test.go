//go:build (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestSharedScalarScaledAddWidths(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, constant, shl, add, mask := wasm.I32, byte(0x41), byte(0x74), byte(0x6a), uint64(0xffffffff)
		if wide {
			typ, constant, shl, add, mask = wasm.I64, 0x42, 0x86, 0x7c, ^uint64(0)
		}
		for shift := byte(0); shift <= 3; shift++ {
			body := []byte{0x20, 0, 0x20, 1, constant, shift, shl, add, 0x0b}
			module := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
			decoded, err := wasm.DecodeModule(module)
			if err != nil {
				t.Fatal(err)
			}
			if err = wasm.ValidateModule(decoded); err != nil {
				t.Fatal(err)
			}
			// Prove the regression body belongs to the common scalar subset.
			if !shared.AdmitScalar(body, &wasm.CompType{Params: []wasm.ValType{typ, typ}, Results: []wasm.ValType{typ}}, []wasm.ValType{typ, typ}).Eligible {
				t.Fatal("scaled addition unexpectedly rejected")
			}
			for _, regABI := range []bool{false, true} {
				t.Run(fmt.Sprintf("wide=%v/shift=%d/regABI=%v", wide, shift, regABI), func(t *testing.T) {
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
					for _, args := range [][2]uint64{{1 << 40, 1}, {1, 1 << 40}, {0, 0}, {0xffffffff, 1}, {1 << 63, 1 << 62}, {^uint64(0), ^uint64(0)}, {0x123456789abcdef0, 0xfedcba9876543210}} {
						x, y := args[0]&mask, args[1]&mask
						want := (x + (y << shift)) & mask
						got, err := in.Invoke("run", x, y)
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("x=%x y=%x got=%x err=%v want=%x", x, y, got, err, want)
						}
					}
				})
			}
		}
	}
}
