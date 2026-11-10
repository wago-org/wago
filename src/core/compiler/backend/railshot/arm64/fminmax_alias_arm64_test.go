//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestScalarMinMaxLocalSinkPreservesAliasedRightOperandARM64(t *testing.T) {
	for _, tc := range []struct {
		name        string
		typ         wasm.ValType
		op          byte
		left, right uint64
		want        uint64
	}{
		{"f32_min", wasm.F32, 0x96, uint64(math.Float32bits(5)), uint64(math.Float32bits(2)), uint64(math.Float32bits(2))},
		{"f32_max", wasm.F32, 0x97, uint64(math.Float32bits(2)), uint64(math.Float32bits(5)), uint64(math.Float32bits(5))},
		{"f64_min", wasm.F64, 0xa4, math.Float64bits(5), math.Float64bits(2), math.Float64bits(2)},
		{"f64_max", wasm.F64, 0xa5, math.Float64bits(2), math.Float64bits(5), math.Float64bits(5)},
	} {
		for _, tee := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/tee=%t", tc.name, tee), func(t *testing.T) {
				set := byte(0x21)
				if tee {
					set = 0x22
				}
				body := []byte{0x00, 0x20, 0x00, 0x20, 0x01, tc.op, set, 0x01}
				if tee {
					body = append(body, 0x1a) // discard local.tee's stack result
				}
				body = append(body, 0x20, 0x01, 0x0b)
				m := modFuncs(t, funcDef{
					params:  []wasm.ValType{tc.typ, tc.typ},
					results: []wasm.ValType{tc.typ},
					body:    body,
				})
				got, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, tc.left, tc.right)
				if err != nil {
					t.Fatal(err)
				}
				if got != tc.want {
					t.Fatalf("result bits = %#x; want %#x", got, tc.want)
				}
			})
		}
	}
}
