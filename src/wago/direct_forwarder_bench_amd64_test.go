//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func directForwarderLoopModule(hops int) []byte {
	types := [][]byte{{1}}
	bodies := [][]byte{wasmtest.Code([]byte{0x20, 0, 0xfb, 2, 0, 0, 0x0b})}
	for i := 0; i < hops; i++ {
		types = append(types, []byte{1})
		bodies = append(bodies, wasmtest.Code([]byte{0x20, 0, 0x10, byte(i), 0x0b}))
	}
	types = append(types, []byte{2})
	loop := []byte{2, 1, 0x63, 0, 2, 0x7f,
		0x41, 7, 0xfb, 0, 0, 0x21, 1,
		0x02, 0x40, 0x03, 0x40, 0x20, 2, 0x20, 0, 0x4f, 0x0d, 1,
		0x20, 3, 0x20, 1, 0x10, byte(hops), 0x6a, 0x21, 3,
		0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x0c, 0, 0x0b, 0x0b, 0x20, 3, 0x0b}
	bodies = append(bodies, append(wasmtest.ULEB(uint32(len(loop))), loop...))
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, []byte{0x60, 1, 0x63, 0, 1, 0x7f}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(types...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("loop", 0, uint32(hops+1)))),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func BenchmarkDirectForwarder(b *testing.B) {
	for _, hops := range []int{1, 4} {
		for _, enabled := range []bool{false, true} {
			b.Run(fmt.Sprintf("hops%d/enabled%t", hops, enabled), func(b *testing.B) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("direct-forwarder", enabled), directForwarderLoopModule(hops))
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				if status := c.GCNativeRootAdmission(); !status.Required || !status.Exact {
					b.Fatalf("benchmark requires exact roots: %+v", status)
				}
				in, err := Instantiate(c)
				if err != nil {
					b.Fatal(err)
				}
				defer in.Close()
				invoke := func() {
					got, err := in.Invoke("loop", 8192)
					if err != nil || len(got) != 1 || got[0] != 7*8192 {
						b.Fatalf("loop=%v,%v", got, err)
					}
				}
				invoke()
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					invoke()
				}
			})
		}
	}
}
