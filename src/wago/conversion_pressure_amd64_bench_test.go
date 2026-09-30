//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func BenchmarkCompileConversionPressureAMD64(b *testing.B) {
	for _, tc := range []struct {
		name   string
		module []byte
	}{
		{"unsigned-scalar", integerConversionPressureModule(0, wasm.I64, []byte{0xba, 0xb0})},
		{"unsigned-pressure", integerConversionPressureModule(7, wasm.I64, []byte{0xba, 0xb0})},
		{"saturation-scalar", signedSaturationPressureModule(0, wasm.F64, true)},
		{"saturation-pressure", signedSaturationPressureModule(13, wasm.F64, true)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			cfg := NewRuntimeConfig()
			var size int
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				compiled, err := Compile(cfg, tc.module)
				if err != nil {
					b.Fatal(err)
				}
				size = compiled.CodeSize()
				compiled.Close()
			}
			b.ReportMetric(float64(size), "code-B")
		})
	}
}

// Scalar cases guard the ordinary low-pressure path. The corresponding tests
// verify the live-value pressure cases before their performance is interpreted.
func BenchmarkScalarConversionSpillAMD64(b *testing.B) {
	for _, tc := range []struct {
		name   string
		module []byte
		args   []uint64
		want   uint64
	}{
		{"unsigned", integerConversionPressureModule(0, wasm.I64, []byte{0xba, 0xb0}), []uint64{7}, 7},
		{"saturation", signedSaturationPressureModule(0, wasm.F64, true), []uint64{F64(3), F64(7)}, F64(7)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			compiled, instance := compileConversionPressure(b, tc.module, true)
			got, err := instance.Invoke("run", tc.args...)
			if err != nil || len(got) != 1 || got[0] != tc.want {
				b.Fatalf("result = %v, %v; want %d", got, err, tc.want)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := instance.Invoke("run", tc.args...); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(compiled.CodeSize()), "code-B")
		})
	}
}
