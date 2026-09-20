//go:build linux && amd64

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math/bits"
	"math/rand"
	"testing"
)

func TestNestedIntegerExpressions(t *testing.T) {
	for seed := int64(0); seed < 3000; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			r := rand.New(rand.NewSource(seed))
			input := r.Uint32()
			body := []byte{0}
			var expr func(int) uint32
			expr = func(d int) uint32 {
				if d == 0 || r.Intn(5) == 0 {
					if r.Intn(2) == 0 {
						body = append(body, 0x20, 0)
						return input
					}
					v := r.Uint32()
					body = append(body, 0x41)
					body = append(body, wasmtest.SLEB32(int32(v))...)
					return v
				}
				a := expr(d - 1)
				b := expr(d - 1)
				op := r.Intn(11)
				body = append(body, []byte{0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78}[op])
				switch op {
				case 0:
					return a + b
				case 1:
					return a - b
				case 2:
					return a * b
				case 3:
					return a & b
				case 4:
					return a | b
				case 5:
					return a ^ b
				case 6:
					return a << (b & 31)
				case 7:
					return uint32(int32(a) >> (b & 31))
				case 8:
					return a >> (b & 31)
				case 9:
					return bits.RotateLeft32(a, int(b&31))
				default:
					return bits.RotateLeft32(a, -int(b&31))
				}
			}
			want := expr(7)
			body = append(body, 0x0b)
			m := mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32}, body)
			got := uint32(runAmd64(t, m, int32(input)))
			if got != want {
				t.Fatalf("input=%x body=%x got=%x want=%x", input, body, got, want)
			}
		})
	}
}
