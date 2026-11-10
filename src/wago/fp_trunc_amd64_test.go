//go:build linux && amd64 && !tinygo

package wago

import (
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math"
	"testing"
)

func TestAMD64ScalarTruncationEdges(t *testing.T) {
	for _, src64 := range []bool{false, true} {
		for _, dst64 := range []bool{false, true} {
			for _, signed := range []bool{false, true} {
				t.Run(fmt.Sprintf("src64=%v/dst64=%v/signed=%v", src64, dst64, signed), func(t *testing.T) {
					src, dst := wasm.F32, wasm.I32
					op := byte(0xa8)
					if src64 {
						src = wasm.F64
						op += 2
					}
					if dst64 {
						dst = wasm.I64
						op += 6
					}
					if !signed {
						op++
					}
					data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{src}, []wasm.ValType{dst}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, op, 0x0b}))))
					c, err := Compile(NewRuntimeConfig(), data)
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c, InstantiateOptions{})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					width := 32
					if dst64 {
						width = 64
					}
					upper := math.Ldexp(1, width)
					lower := float64(-1)
					if signed {
						upper = math.Ldexp(1, width-1)
						lower = -upper - 1
					}
					next := func(x, toward float64) float64 {
						if src64 {
							return math.Nextafter(x, toward)
						}
						return float64(math.Nextafter32(float32(x), float32(toward)))
					}
					vals := []struct {
						x    float64
						trap bool
					}{{0, false}, {math.Copysign(0, -1), false}, {1.75, false}, {-0.75, false}, {next(upper, math.Inf(-1)), false}, {upper, true}, {math.Inf(1), true}, {math.Inf(-1), true}, {math.NaN(), true}}
					if signed {
						vals = append(vals, struct {
							x    float64
							trap bool
						}{-3.75, false}, struct {
							x    float64
							trap bool
						}{-upper, false}, struct {
							x    float64
							trap bool
						}{next(lower, math.Inf(-1)), true})
					} else {
						vals = append(vals, struct {
							x    float64
							trap bool
						}{-1, true})
					}
					check := func(bits uint64, x float64, wantTrap bool) {
						t.Helper()
						got, err := in.Invoke("run", bits)
						if wantTrap {
							var trap *TrapError
							if !errors.As(err, &trap) || trap.Code != TrapTruncOverflow {
								t.Fatalf("bits %x: %v,%v; want trunc trap", bits, got, err)
							}
							return
						}
						var want uint64
						if signed {
							want = uint64(int64(math.Trunc(x)))
						} else {
							want = uint64(math.Trunc(x))
						}
						if !dst64 {
							want = uint64(uint32(want))
						}
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("bits %x: %v,%v; want%x", bits, got, err, want)
						}
					}
					for _, v := range vals {
						x := v.x
						bits := math.Float64bits(x)
						if !src64 {
							x = float64(float32(x))
							bits = uint64(math.Float32bits(float32(x)))
						}
						check(bits, x, v.trap)
					}
					if src64 {
						check(0x7ff0000000000001, 0, true)
						check(0xfff0000000000001, 0, true)
					} else {
						check(0x7f800001, 0, true)
						check(0xff800001, 0, true)
					}
				})
			}
		}
	}
}
