//go:build (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestSharedScalarJoinResultLocalAlias(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, constant, add, result := wasm.I32, byte(0x41), byte(0x6a), byte(0x7f)
		if wide {
			typ, constant, add, result = wasm.I64, 0x42, 0x7c, 0x7e
		}
		for _, tc := range []struct {
			name               string
			then, other, after []byte
			yes, no            uint64
		}{
			{"both-edges", []byte{constant, 7, 0x22, 1}, []byte{constant, 9, 0x22, 1}, []byte{0x20, 1, add}, 14, 18},
			{"different-locals", []byte{constant, 7, 0x22, 1}, []byte{constant, 9, 0x22, 2}, []byte{0x20, 1, add}, 14, 9},
			{"overwrite-before-join", []byte{constant, 7, 0x22, 1}, []byte{constant, 9, 0x22, 1, constant, 3, 0x21, 1}, []byte{0x20, 1, add}, 14, 12},
			{"overwrite-after-join", []byte{constant, 7, 0x22, 1}, []byte{constant, 9, 0x22, 1}, []byte{constant, 3, 0x21, 1, 0x20, 1, add}, 10, 12},
			{"nested", []byte{0x20, 0, 0x41, 1, 0x46, 0x04, result, constant, 7, 0x22, 1, 0x05, constant, 5, 0x22, 1, 0x0b}, []byte{constant, 9, 0x22, 1}, []byte{0x20, 1, add}, 14, 18},
		} {
			body := append([]byte{0x20, 0, 0x04, result}, tc.then...)
			body = append(body, 0x05)
			body = append(body, tc.other...)
			body = append(body, 0x0b)
			body = append(body, tc.after...)
			body = append(body, 0x0b)
			ft := &wasm.CompType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{typ}}
			if !shared.AdmitScalar(body, ft, []wasm.ValType{wasm.I32, typ, typ}).Eligible {
				t.Fatal("fixture rejected")
			}
			code := append([]byte{1, 2, result}, body...)
			module := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(ft.Params, ft.Results))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))))
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
						want := tc.yes
						if condition == 0 {
							want = tc.no
						}
						if tc.name == "nested" && condition == 0xffffffff {
							want = 10
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
