//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// A full flush must preserve a spilled child of a deferred root when an earlier
// root writes its canonical home. Distinct finite values expose the overwrite.
func TestFullFlushPreservesDeferredSpillValues(t *testing.T) {
	for _, count := range []int{1, 8, 16, 33} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			body := []byte{0, 0x20, 0} // no locals; keep the f64 parameter below the expressions
			for i := 0; i < count; i++ {
				body = append(body, 0x20, 1, 0xfc, 1, 0x67) // local.get 1; i32.trunc_sat_f32_u; i32.clz
			}
			body = append(body, 0x02, 0x40, 0x0b) // empty block forces a full canonicalization
			for i := 1; i < count; i++ {
				body = append(body, 0x1a)
			}
			body = append(body, 0x0b)
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.F64, wasm.F32}, []wasm.ValType{wasm.F64, wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
			)
			compiled, err := Compile(nil, module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			in, err := Instantiate(compiled)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for _, input := range []float32{8, 256} {
				got, err := in.Invoke("run", math.Float64bits(2), uint64(math.Float32bits(input)))
				want := uint64(28)
				if input == 256 {
					want = 23
				}
				if err != nil || len(got) != 2 || got[0] != math.Float64bits(2) || got[1] != want {
					t.Fatalf("input=%v: got %x, %v; want [4000000000000000 %x]", input, got, err, want)
				}
			}
		})
	}
}
