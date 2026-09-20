//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func mixedCallPressureModule(t testing.TB, f64 bool, nargs int) (*wasm.Module, uint64) {
	return mixedCallSpillModule(t, f64, nargs, false, false)
}

func mixedCallSpillModule(t testing.TB, f64 bool, nargs int, computed, wide bool) (*wasm.Module, uint64) {
	t.Helper()
	typ, add, mul, convert := wasm.F32, byte(0x92), byte(0x94), byte(0xb2)
	if f64 {
		typ, add, mul, convert = wasm.F64, 0xa0, 0xa2, 0xb7
	}
	constant := func(body []byte, value float64) []byte {
		if f64 {
			return binary.LittleEndian.AppendUint64(append(body, 0x44), math.Float64bits(value))
		}
		return binary.LittleEndian.AppendUint32(append(body, 0x43), math.Float32bits(float32(value)))
	}
	// Twenty-eight float locals fill the 27-register pin bank. Local 27 holds
	// the call result; all other locals are read again after the call.
	typeByte := byte(0x7d)
	if f64 {
		typeByte = 0x7c
	}
	body := []byte{1, 28, typeByte}
	if wide {
		body = []byte{2, 28, typeByte, 1, 0x7b}
	}
	for i := byte(0); i < 27; i++ {
		body = constant(body, float64(i+1))
		body = append(body, 0x21, i)
	}
	body = constant(body, 0.5)
	body = append(body, 0xfd, 0x0c) // v128.const, live below the arguments
	if wide {
		for _, lane := range []uint32{17, 29, 43, 61} {
			body = binary.LittleEndian.AppendUint32(body, ^lane)
		}
		body = append(body, 0xfd, 0x4d) // v128.not owns a register before argument evaluation
	} else {
		body = binary.LittleEndian.AppendUint64(body, 17)
		body = binary.LittleEndian.AppendUint64(body, 0)
	}
	params := make([]wasm.ValType, nargs)
	callee := []byte{0}
	want := 378.0 + 17 + 0.5
	for i := 0; i < nargs; i++ {
		params[i] = typ
		body = append(body, 0x20, byte(nargs-i-1))
		if computed {
			body = append(body, 0x20, byte(nargs-i-1), add)
		}
		callee = append(callee, 0x20, byte(i))
		callee = constant(callee, float64(i+1))
		callee = append(callee, mul)
		if i > 0 {
			callee = append(callee, add)
		}
		factor := 1.0
		if computed {
			factor = 2
		}
		want += factor * float64((nargs-i)*(i+1))
	}
	if computed {
		// Check each argument separately as well as the weighted result.
		checks := []byte{0}
		for i := 0; i < nargs; i++ {
			checks = append(checks, 0x20, byte(i))
			checks = constant(checks, 2*float64(nargs-i))
			ne := byte(0x5c)
			if f64 {
				ne = 0x62
			}
			checks = append(checks, ne, 0x04, 0x40, 0x00, 0x0b)
		}
		callee = append(checks, callee[1:]...)
	}
	params = append(params, wasm.I32)
	callee = append(callee, 0x20, byte(nargs), convert, add)
	body = append(body, 0x41, 11)
	want += 11
	body = append(body, 0x10, 1, 0x21, 27) // call; local.set result
	if wide {
		body = append(body, 0x21, 28)
		for lane, value := range []byte{17, 29, 43, 61} {
			body = append(body, 0x20, 28, 0xfd, 0x1b, byte(lane), 0x41, value, 0x47, 0x04, 0x40, 0x00, 0x0b)
		}
		body = append(body, 0x20, 28)
	}
	body = append(body, 0xfd, 0x1b, 0, convert, add) // extract lane; convert; add
	body = append(body, 0x20, 27, add)
	for i := byte(0); i < 27; i++ {
		body = append(body, 0x20, i, add)
	}
	body = append(body, 0x0b)
	callee = append(callee, 0x0b)
	bits := uint64(math.Float32bits(float32(want)))
	if f64 {
		bits = math.Float64bits(want)
	}
	return modFuncs(t,
		funcDef{results: []wasm.ValType{typ}, body: body},
		funcDef{params: params, results: []wasm.ValType{typ}, body: callee},
	), bits
}

func TestMixedCallFloatRegisterPressureARM64(t *testing.T) {
	for _, f64 := range []bool{false, true} {
		for _, nargs := range []int{4, 5, 8} {
			t.Run(fmt.Sprintf("f64=%t/args=%d", f64, nargs), func(t *testing.T) {
				m, want := mixedCallPressureModule(t, f64, nargs)
				stats := &ModuleStats{}
				got, err := runArm64WrapperWithOptions(t, m, CompileOptions{
					Stats:         stats,
					Optimizations: map[string]bool{"inline": false, "stack-reg": true, "ext-fp-pins": true},
				})
				if err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Fatalf("result = %#x, want %#x", got, want)
				}
				if stats.Funcs[0].PinnedLocals != 27 || stats.Funcs[0].Calls["mixed"] != 1 {
					t.Fatalf("expected 27 pinned locals and one mixed call: %+v", stats.Funcs[0])
				}
			})
		}
	}
}

func TestMixedCallFloatRegisterPressureEagerCompileARM64(t *testing.T) {
	for _, f64 := range []bool{false, true} {
		m, _ := mixedCallPressureModule(t, f64, 8)
		if _, err := CompileModuleWith(m, CompileOptions{
			Optimizations: map[string]bool{"inline": false, "stack-reg": false, "ext-fp-pins": true},
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func BenchmarkCompileMixedCallPressureARM64(b *testing.B) {
	for _, nargs := range []int{4, 8} {
		b.Run(fmt.Sprintf("args=%d", nargs), func(b *testing.B) {
			m, _ := mixedCallPressureModule(b, true, nargs)
			opts := CompileOptions{Workers: 1, Optimizations: map[string]bool{"inline": false}}
			var stats ModuleStats
			diagnostic := opts
			diagnostic.Stats = &stats
			if _, err := CompileModuleWith(m, diagnostic); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := CompileModuleWith(m, opts); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(stats.Funcs[0].MaxSpillSlots), "spill-slots")
			b.ReportMetric(float64(stats.Funcs[0].FrameBytes), "frame-B")
			b.ReportMetric(float64(stats.Funcs[0].CodeBytes), "code-B")
		})
	}
}

func TestMixedCallExistingSpillsARM64(t *testing.T) {
	for _, f64 := range []bool{false, true} {
		for _, wide := range []bool{false, true} {
			t.Run(fmt.Sprintf("f64=%t/wide=%t", f64, wide), func(t *testing.T) {
				nargs := 8
				if wide {
					// Five owned arguments spill the vector, but no argument,
					// before call preparation. This isolates the flush order bug.
					nargs = 5
				}
				m, want := mixedCallSpillModule(t, f64, nargs, true, wide)
				stats := &ModuleStats{}
				got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Stats: stats,
					Optimizations: map[string]bool{"inline": false, "stack-reg": true, "ext-fp-pins": true},
				})
				if err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Fatalf("result = %#x, want %#x", got, want)
				}
				if stats.Funcs[0].PinnedLocals != 27 || stats.Funcs[0].Calls["mixed"] != 1 {
					t.Fatal("test did not retain 27 local pins and a mixed call")
				}
			})
		}
	}
}

func BenchmarkCompileMixedCallExistingSpillsARM64(b *testing.B) {
	for _, wide := range []bool{false, true} {
		b.Run(fmt.Sprintf("wide=%t", wide), func(b *testing.B) {
			nargs := 8
			if wide {
				nargs = 5
			}
			m, _ := mixedCallSpillModule(b, true, nargs, true, wide)
			opts := CompileOptions{Workers: 1, Optimizations: map[string]bool{"inline": false}}
			var stats ModuleStats
			diagnostic := opts
			diagnostic.Stats = &stats
			if _, err := CompileModuleWith(m, diagnostic); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := CompileModuleWith(m, opts); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(stats.Funcs[0].MaxSpillSlots), "spill-slots")
			b.ReportMetric(float64(stats.Funcs[0].FrameBytes), "frame-B")
			b.ReportMetric(float64(stats.Funcs[0].CodeBytes), "code-B")
		})
	}
}
