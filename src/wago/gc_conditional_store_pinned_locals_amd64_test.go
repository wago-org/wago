//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Keep integer and floating-point locals live across a conditional native GC
// store. The two register banks together can pin more than sixteen locals.
func gcConditionalStorePinnedLocalsModule(array bool) []byte {
	aggregate := []byte{0x5f, 0x01, 0x6d, 0x01} // struct with a mutable eqref field
	if array {
		aggregate = []byte{0x5e, 0x6d, 0x01} // array of mutable eqref
	}
	params := make([]wasm.ValType, 19)
	for i := range params {
		params[i] = wasm.F64
		if i < 8 {
			params[i] = wasm.I32
		}
	}
	body := []byte{0x01, 0x01, 0x63, 0x00} // local 19: (ref null 0)
	if array {
		body = append(body, 0x41, 0x01, 0xfb, 0x07, 0x00) // array.new_default 0
	} else {
		body = append(body, 0xfb, 0x01, 0x00) // struct.new_default 0
	}
	body = append(body, 0x21, 0x13, 0x02, 0x40, 0x01, 0x0b, 0x20, 0x13)
	if array {
		body = append(body, 0x41, 0x00) // array index
	}
	body = append(body, 0x41, 0x2a, 0xfb, 0x1c) // i31 value 42
	if array {
		body = append(body, 0xfb, 0x0e, 0x00, 0x20, 0x13, 0x41, 0x00, 0xfb, 0x0b, 0x00)
	} else {
		body = append(body, 0xfb, 0x05, 0x00, 0x00, 0x20, 0x13, 0xfb, 0x02, 0x00, 0x00)
	}
	body = append(body, 0xfb, 0x16, 0x6c, 0xfb, 0x1d, 0xb7) // cast i31, read, convert to f64
	for i := byte(0); i < 19; i++ {
		body = append(body, 0x20, i)
		if i < 8 {
			body = append(body, 0xb7) // f64.convert_i32_s
		}
		body = append(body, 0xa0) // f64.add
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(aggregate, wasmtest.FuncType(params, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCConditionalStorePreservesWidePinnedLocals(t *testing.T) {
	for _, array := range []bool{false, true} {
		name := "struct"
		if array {
			name = "array"
		}
		t.Run(name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcConditionalStorePinnedLocalsModule(array))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			in, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			args := make([]uint64, 19)
			want := 42.0
			for i := range args {
				value := float64(i + 1)
				want += value
				args[i] = math.Float64bits(value)
				if i < 8 {
					args[i] = uint64(i + 1)
				}
			}
			got, err := in.Invoke("run", args...)
			if err != nil || len(got) != 1 || math.Float64frombits(got[0]) != want {
				t.Fatalf("run = %v, %v; want f64(%g)", got, err, want)
			}
		})
	}
}
