//go:build (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestSharedScalarZeroPoolVersionsAndJoins(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, constant, add, result, first, last := wasm.I32, byte(0x41), byte(0x6a), byte(0x7f), byte(1), byte(2)
		value := int64(0x70000007)
		immediate := wasmtest.SLEB32(int32(value))
		if wide {
			typ, constant, add, result, first, last = wasm.I64, 0x42, 0x7c, 0x7e, 3, 4
			value = 1<<40 | 7
			immediate = wasmtest.SLEB64(value)
		}
		set := append([]byte{constant}, immediate...)
		set = append(set, 0x21, first)
		join := []byte{0x20, first}
		join = append(join, set...)
		join = append(join, 0x20, 0, 0x04, result, 0x20, first, 0x05, 0x20, last, 0x0b, add, 0x20, last, add, 0x0b)
		nested := []byte{0x20, first, 0x20, 0, 0x04, 0x40, 0x20, 0, 0x04, 0x40}
		nested = append(nested, set...)
		nested = append(nested, 0x0b, 0x0b, 0x20, first, add, 0x0f, 0x0b)
		for _, tc := range []struct {
			name    string
			body    []byte
			nonzero uint64
		}{{"old-zero-at-join", join, uint64(value)}, {"nested-no-else-tail-return", nested, uint64(value)}, {"untouched-tail-return", []byte{0x20, last, 0x0f, 0x0b}, 0}} {
			locals := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I64, wasm.I64}
			summary := shared.AdmitScalar(tc.body, &wasm.CompType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{typ}}, locals)
			if !summary.Eligible {
				t.Fatalf("%s rejected", tc.name)
			}
			code := []byte{2, 2, 0x7f, 2, 0x7e}
			code = append(code, tc.body...)
			module := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{typ}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))))
			for _, regABI := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/wide=%v/regABI=%v", tc.name, wide, regABI), func(t *testing.T) {
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
						want := tc.nonzero
						if condition == 0 {
							want = 0
						}
						got, err := in.Invoke("run", condition)
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("condition=%x got=%x err=%v want=%x", condition, got, err, want)
						}
					}
				})
			}
		}
	}
}
