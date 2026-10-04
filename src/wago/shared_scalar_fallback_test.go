//go:build (amd64 || arm64) && !tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestSharedScalarFallbackScratchEdgeValues(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, constant, add, divide, mask := wasm.I32, byte(0x41), byte(0x6a), byte(0x6d), uint64(0xffffffff)
		if wide {
			typ, constant, add, divide, mask = wasm.I64, 0x42, 0x7c, 0x7f, ^uint64(0)
		}
		for _, firstShared := range []bool{false, true} {
			var funcs, exports, bodies [][]byte
			for i := 0; i < 3; i++ {
				body := []byte{0x20, 0, constant, 2, divide, 0x0b}
				if (i%2 == 0) == firstShared {
					body = []byte{0x20, 0, constant, 1, add, 0x0b}
				}
				funcs = append(funcs, wasmtest.ULEB(0))
				exports = append(exports, wasmtest.ExportEntry(string(rune('a'+i)), 0, uint32(i)))
				bodies = append(bodies, wasmtest.Code(body))
			}
			module := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}))), wasmtest.Section(3, wasmtest.Vec(funcs...)), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
			for _, workers := range []int{1, 2} {
				c, err := Compile(NewRuntimeConfig().WithFunctionWorkers(workers), module)
				if err != nil {
					t.Fatal(err)
				}
				in, err := Instantiate(c)
				if err != nil {
					c.Close()
					t.Fatal(err)
				}
				for i := 0; i < 3; i++ {
					for _, x := range []uint64{0, 1, 3, 0x80000000, 0xffffffff, 1 << 63, ^uint64(0)} {
						x &= mask
						want := (x + 1) & mask
						if (i%2 == 0) != firstShared {
							if wide {
								want = uint64(int64(x) / 2)
							} else {
								want = uint64(uint32(int32(x) / 2))
							}
						}
						got, err := in.Invoke(string(rune('a'+i)), x)
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("wide=%v firstShared=%v workers=%d func=%d x=%x got=%x err=%v want=%x", wide, firstShared, workers, i, x, got, err, want)
						}
					}
				}
				in.Close()
				c.Close()
			}
		}
	}
}
