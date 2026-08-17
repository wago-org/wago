//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/wasmtest"
)

func BenchmarkMemoryLeafScalarABI(b *testing.B) {
	i32 := []wasm.ValType{wasm.I32}
	caller := []byte{0x20, 0x00, 0x41, 0x02, 0x6a, 0x21, 0x00}
	for range 16 {
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
		b.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		on   bool
	}{{"off", false}, {"on", true}} {
		b.Run(tc.name, func(b *testing.B) {
			cm, compileErr := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"abi-classes": tc.on, "inline": false}})
			if compileErr != nil {
				b.Fatal(compileErr)
			}
			eng, engineErr := coreruntime.NewEngine()
			if engineErr != nil {
				b.Fatal(engineErr)
			}
			defer eng.Close()
			jm, memoryErr := coreruntime.NewJobMemory(65536)
			if memoryErr != nil {
				b.Fatal(memoryErr)
			}
			defer jm.Close()
			arena, arenaErr := coreruntime.NewArena(4096)
			if arenaErr != nil {
				b.Fatal(arenaErr)
			}
			defer arena.Close()
			code, entry, mapErr := coreruntime.MapCode(cm.Code)
			if mapErr != nil {
				b.Fatal(mapErr)
			}
			defer coreruntime.Unmap(code)
			args, results, trap := arena.Alloc(8), arena.Alloc(8), arena.Alloc(8)
			args[0] = 7
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if callErr := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); callErr != nil {
					b.Fatal(callErr)
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

func leafFPABIBenchmarkModule(t testing.TB) *wasm.Module {
	t.Helper()
	f64x4 := []wasm.ValType{wasm.F64, wasm.F64, wasm.F64, wasm.F64}
	f64 := []wasm.ValType{wasm.F64}
	caller := []byte{0x00}
	for range 64 {
		caller = append(caller, 0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x20, 0x03, 0x10, 0x01, 0x1a)
	}
	caller = append(caller, 0x20, 0x00, 0x20, 0x01, 0xa0, 0x20, 0x02, 0xa0, 0x20, 0x03, 0xa0, 0x0b)
	callee := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0xa0, 0x20, 0x02, 0xa0, 0x20, 0x03, 0xa0, 0x0b}
	return modFuncs(t,
		funcDef{params: f64x4, results: f64, body: caller},
		funcDef{params: f64x4, results: f64, body: callee},
	)
}
