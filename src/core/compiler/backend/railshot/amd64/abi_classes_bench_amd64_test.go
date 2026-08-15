//go:build (linux || darwin) && amd64

package amd64

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/wasmtest"
)

func BenchmarkMemoryLeafScalarABI(b *testing.B) {
	m := memoryLeafScalarABIModule(b, 128)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"general", false}, {"leaf-scalar", true}} {
		b.Run(tc.name, func(b *testing.B) {
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"abi-classes": tc.on, "inline": false}})
			if err != nil {
				b.Fatal(err)
			}
			eng, err := coreruntime.NewEngine()
			if err != nil {
				b.Fatal(err)
			}
			defer eng.Close()
			jm, err := coreruntime.NewJobMemory(65536)
			if err != nil {
				b.Fatal(err)
			}
			defer jm.Close()
			arena, err := coreruntime.NewArena(4096)
			if err != nil {
				b.Fatal(err)
			}
			defer arena.Close()
			code, entry, err := coreruntime.MapCode(cm.Code)
			if err != nil {
				b.Fatal(err)
			}
			defer coreruntime.Unmap(code)
			args, results, trap := arena.Alloc(8), arena.Alloc(8), arena.Alloc(8)
			args[0] = 7
			nativeBytes := 0
			for _, fnStats := range stats.Funcs {
				nativeBytes += fnStats.CodeBytes
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.ReportMetric(float64(nativeBytes), "native-bytes")
			for i := 0; i < b.N; i++ {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCompileMemoryLeafScalarABI(b *testing.B) {
	m := memoryLeafScalarABIModule(b, 16)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"general", false}, {"leaf-scalar", true}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"abi-classes": tc.on, "inline": false}}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkLeafFPABI(b *testing.B) {
	m := leafFPABIBenchmarkModule(b)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"general", false}, {"leaf-fp", true}} {
		b.Run(tc.name, func(b *testing.B) {
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"abi-leaf-fp": tc.on, "inline": false}})
			if err != nil {
				b.Fatal(err)
			}
			eng, err := coreruntime.NewEngine()
			if err != nil {
				b.Fatal(err)
			}
			defer eng.Close()
			jm, err := coreruntime.NewJobMemory(65536)
			if err != nil {
				b.Fatal(err)
			}
			defer jm.Close()
			arena, err := coreruntime.NewArena(4096)
			if err != nil {
				b.Fatal(err)
			}
			defer arena.Close()
			code, entry, err := coreruntime.MapCode(cm.Code)
			if err != nil {
				b.Fatal(err)
			}
			defer coreruntime.Unmap(code)
			args, results, trap := arena.Alloc(32), arena.Alloc(8), arena.Alloc(8)
			for i := range 4 {
				binary.LittleEndian.PutUint64(args[i*8:], math.Float64bits(float64(i+1)))
			}
			nativeBytes := 0
			for _, fnStats := range stats.Funcs {
				nativeBytes += fnStats.CodeBytes
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.ReportMetric(float64(nativeBytes), "native-bytes")
			for i := 0; i < b.N; i++ {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCompileLeafFPABI(b *testing.B) {
	m := leafFPABIBenchmarkModule(b)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"general", false}, {"leaf-fp", true}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"abi-leaf-fp": tc.on, "inline": false}}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func memoryLeafScalarABIModule(t testing.TB, calls int) *wasm.Module {
	t.Helper()
	i32 := []wasm.ValType{wasm.I32}
	caller := []byte{0x20, 0x00, 0x41, 0x02, 0x6a, 0x21, 0x00}
	for range calls {
		caller = append(caller, 0x20, 0x00, 0x10, 0x01, 0x1a)
	}
	caller = append(caller, 0x20, 0x00, 0x0b)
	callee := []byte{0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(i32, i32))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(caller), wasmtest.Code(callee))),
	)
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func leafFPABIBenchmarkModule(t testing.TB) *wasm.Module {
	t.Helper()
	f64x4 := []wasm.ValType{wasm.F64, wasm.F64, wasm.F64, wasm.F64}
	f64 := []wasm.ValType{wasm.F64}
	caller := []byte{0x00}
	for range 128 {
		caller = append(caller, 0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x20, 0x03, 0x10, 0x01, 0x1a)
	}
	caller = append(caller, 0x20, 0x00, 0x20, 0x01, 0xa0, 0x20, 0x02, 0xa0, 0x20, 0x03, 0xa0, 0x0b)
	callee := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0xa0, 0x20, 0x02, 0xa0, 0x20, 0x03, 0xa0, 0x0b}
	return modFuncs(t,
		funcDef{params: f64x4, results: f64, body: caller},
		funcDef{params: f64x4, results: f64, body: callee},
	)
}
