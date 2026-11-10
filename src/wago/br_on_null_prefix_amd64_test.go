//go:build linux && amd64 && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestAMD64BrOnNullPreservesReferenceDuringPrefixFlush(t *testing.T) {
	for _, beforeBlock := range []bool{false, true} {
		for pressure := 0; pressure < 18; pressure++ {
			for _, null := range []bool{false, true} {
				t.Run(fmt.Sprintf("before=%v/pressure=%d/null=%v", beforeBlock, pressure, null), func(t *testing.T) {
					body := []byte{0x41, 0, 0x24, 0} // reset marker
					if !beforeBlock {
						body = append(body, 0x02, 0x40)
					}
					for range pressure {
						body = append(body, 0x41, 0, 0xfb, 28)
					}
					body = append(body, 0x41, 1, 0x20, 0, 0x77) // deferred variable rotate
					if null {
						body = append(body, 0xd0, 0x6c) // ref.null i31
					} else {
						body = append(body, 0x41, 0, 0xfb, 28) // ref.i31 0 is non-null
					}
					if beforeBlock {
						body = append(body, 0x02, 0)
					}
					body = append(body, 0xd5, 0, 0x1a, 0x41, 1, 0x24, 0)
					if beforeBlock {
						body = append(body, 0x0b)
					}
					for range pressure + 1 {
						body = append(body, 0x1a)
					}
					if !beforeBlock {
						body = append(body, 0x0b)
					}
					body = append(body, 0x23, 0, 0x0b)
					data := wasmtest.Module(
						wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 1, 0x63, 0x6c, 0}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
						wasmtest.Section(3, wasmtest.Vec([]byte{1})),
						wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}))),
						wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
						wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
					)
					c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c, InstantiateOptions{})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					want := uint64(1)
					if null {
						want = 0
					}
					for _, count := range []uint64{0, 1, 31} {
						got, err := in.Invoke("run", count)
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("count=%d: got %v, %v; want marker%d", count, got, err, want)
						}
					}
				})
			}
		}
	}
}
