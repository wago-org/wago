//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func BenchmarkMixedFourResultRegisterABI(b *testing.B) {
	results := []wasm.ValType{wasm.I32, wasm.F64, wasm.I64, wasm.F32}
	caller := []byte{0x00}
	for range 16 {
		caller = append(caller, 0x10, 0x01, 0x1a, 0x1a, 0x1a, 0x1a)
	}
	caller = append(caller, 0x41, 0x0a, 0x0b)
	callee := []byte{0x00, 0x41, 0x01, 0x44, 0, 0, 0, 0, 0, 0, 0, 0x40, 0x42, 0x03, 0x43, 0, 0, 0x80, 0x40, 0x0b}
	m := modFuncs(b, funcDef{results: []wasm.ValType{wasm.I32}, body: caller}, funcDef{results: results, body: callee})
	for _, tc := range []struct {
		name string
		on   bool
	}{{"wrapper", false}, {"register", true}} {
		b.Run(tc.name, func(b *testing.B) {
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"reg-abi": tc.on, "inline": false}})
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
			args, out, trap := arena.Alloc(8), arena.Alloc(8), arena.Alloc(8)
			codeBytes := 0
			for _, fn := range stats.Funcs {
				codeBytes += fn.CodeBytes
			}
			if codeBytes == 0 {
				b.Fatal("missing native-size stats")
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.ReportMetric(float64(codeBytes), "native-bytes")
			for i := 0; i < b.N; i++ {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, out); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCompileMixedFourResultRegisterABI(b *testing.B) {
	results := []wasm.ValType{wasm.I32, wasm.F64, wasm.I64, wasm.F32}
	caller := []byte{0x00, 0x10, 0x01, 0x1a, 0x1a, 0x1a, 0x1a, 0x41, 0x0a, 0x0b}
	callee := []byte{0x00, 0x41, 0x01, 0x44, 0, 0, 0, 0, 0, 0, 0, 0x40, 0x42, 0x03, 0x43, 0, 0, 0x80, 0x40, 0x0b}
	m := modFuncs(b, funcDef{results: []wasm.ValType{wasm.I32}, body: caller}, funcDef{results: results, body: callee})
	for _, tc := range []struct {
		name string
		on   bool
	}{{"wrapper", false}, {"register", true}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"reg-abi": tc.on, "inline": false}}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
