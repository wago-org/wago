//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestGCNativeI31NoneObjectFallbackLivePrefix(t *testing.T) {
	for _, op := range []byte{20, 21} {
		for _, heap := range []byte{0x6c, 0x71} {
			params := make([]wasm.ValType, 17)
			body := []byte{}
			args := make([]uint64, len(params))
			for i := range params {
				params[i] = wasm.I32
				body = append(body, 0x20, byte(i))
				args[i] = uint64(100 + i)
			}
			body = append(body, 0x23, 0, 0xfb, op, heap, 0x1a, 0x0b)
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0}, wasmtest.FuncType(params, params))),
				wasmtest.Section(3, wasmtest.Vec([]byte{1})),
				wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xfb, 1, 0, 0x0b})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if err != nil {
				t.Fatal(err)
			}
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			for round := 0; round < 2; round++ {
				got, err := in.Invoke("run", args...)
				if err != nil || len(got) != len(args) {
					t.Fatalf("op%d heap%x: %v,%v", op, heap, got, err)
				}
				for i := range args {
					if got[i] != args[i] {
						t.Fatalf("op%d heap%x prefix%d=%d,want%d", op, heap, i, got[i], args[i])
					}
				}
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
			}
			in.Close()
			c.Close()
		}
	}
}
