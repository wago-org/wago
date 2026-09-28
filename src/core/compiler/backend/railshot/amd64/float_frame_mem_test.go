//go:build linux && amd64

package amd64

import (
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestFloatFrameMemoryOperands(t *testing.T) {
	for _, f64 := range []bool{false, true} {
		typ, valByte, add := wasm.F32, byte(0x7d), byte(0x92)
		if f64 {
			typ, valByte, add = wasm.F64, 0x7c, 0xa0
		}
		for op, fn := range []func(float64, float64) float64{
			func(a, b float64) float64 { return a + b }, func(a, b float64) float64 { return a - b },
			func(a, b float64) float64 { return a * b }, func(a, b float64) float64 { return a / b },
		} {
			for _, shape := range []string{"local", "slot", "sink"} {
				body := []byte{1, 63, valByte, 0x20, 0}
				if shape == "sink" {
					body = []byte{0, 0x20, 0}
				}
				if shape != "local" {
					body = append(body, 0x02, valByte)
				}
				body = append(body, 0x20, 1)
				if shape != "local" {
					body = append(body, 0x0b)
				}
				body = append(body, add+byte(op))
				if shape == "sink" {
					body = append(body, 0x21, 0, 0x20, 0)
				}
				body = append(body, 0x0b)
				m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
				for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
					t.Run(fmt.Sprintf("f64=%t/op=%d/%s/features=%x", f64, op, shape, features), func(t *testing.T) {
						var stats ModuleStats
						cm, err := CompileModuleWith(m, CompileOptions{AMD64Features: features, AMD64FeaturesSet: true, Stats: &stats, CompactNative: true, Optimizations: map[string]bool{"reg-merge": false, "float-frame-mem": true}})
						if err != nil {
							t.Fatal(err)
						}
						if cm.CodeImage != nil {
							defer cm.CodeImage.Close()
						}
						if stats.Funcs[0].Peephole["float-frame-mem"] == 0 {
							t.Fatalf("frame operand not folded: %v", stats.Funcs[0].Peephole)
						}
						for _, pair := range [][2]float64{{6.5, 2.5}, {math.Copysign(0, -1), 0}, {math.Inf(1), 2}, {1, math.NaN()}} {
							a, b := pair[0], pair[1]
							ab, bb := math.Float64bits(a), math.Float64bits(b)
							if !f64 {
								a, b = float64(float32(a)), float64(float32(b))
								ab, bb = uint64(math.Float32bits(float32(a))), uint64(math.Float32bits(float32(b)))
							}
							bits := runCompiledAmd64u(t, cm, ab, bb)
							got, want := math.Float64frombits(bits), fn(a, b)
							if !f64 {
								got, want = float64(math.Float32frombits(uint32(bits))), float64(float32(want))
							}
							if math.IsNaN(want) {
								if !math.IsNaN(got) {
									t.Fatalf("got %v, want NaN", got)
								}
								continue
							}
							if got != want || math.Signbit(got) != math.Signbit(want) {
								t.Fatalf("%v op %v: got %v, want %v", a, b, got, want)
							}
						}
					})
				}
			}
		}
	}
}

func TestFloatFrameMemoryOption(t *testing.T) {
	m := mod1(t, []wasm.ValType{wasm.F64, wasm.F64}, []wasm.ValType{wasm.F64}, []byte{1, 63, 0x7c, 0x20, 0, 0x20, 1, 0xa0, 0x0b})
	for _, enabled := range []bool{false, true} {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"float-frame-mem": enabled}})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			cm.CodeImage.Close()
		}
		if folded := stats.Funcs[0].Peephole["float-frame-mem"] != 0; folded != enabled {
			t.Fatalf("enabled=%t: folded=%t", enabled, folded)
		}
	}
}
