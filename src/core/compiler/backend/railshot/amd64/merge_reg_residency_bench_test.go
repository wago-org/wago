//go:build (linux || darwin) && amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

var mergeRegResidencyBenchmarkSink uint64

func mergeRegResidencyBenchmarkModule(tb testing.TB) *wasm.Module {
	tb.Helper()
	body := []byte{0x00}
	for i := range 8 {
		if i&1 == 0 {
			body = append(body,
				0x20, 0x00, 0x41, 0x01, 0x71, // x & 1
				0x04, 0x40, // if
				0x20, 0x00, 0x41, 0x02, 0x6a, 0x21, 0x00, // x += 2
				0x05,                                     // else
				0x20, 0x00, 0x41, 0x03, 0x6a, 0x21, 0x00, // x += 3
				0x0b,
			)
		} else {
			body = append(body,
				0x02, 0x40, // block
				0x20, 0x00, 0x41, 0x01, 0x71, 0x0d, 0x00, // br_if block (x & 1)
				0x20, 0x00, 0x41, 0x02, 0x6a, 0x21, 0x00, // x += 2
				0x0b,
			)
		}
	}
	body = append(body,
		0x20, 0x00, // result remains below a never-taken call block
		0x41, 0x00, 0x04, 0x40,
		0x20, 0x00, 0x10, 0x01, 0x1a,
		0x0b, 0x0b,
	)
	return modFuncs(tb,
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x20, 0x00, 0x0b}},
	)
}

func BenchmarkMergeRegisterResidency(b *testing.B) {
	m := mergeRegResidencyBenchmarkModule(b)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"canonical-merges", false}, {"resident-merges", true}} {
		b.Run(tc.name, func(b *testing.B) {
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{
				Optimizations: map[string]bool{"merge-reg-residency": tc.on, "inline": false},
				Stats:         &stats,
			})
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
			binary.LittleEndian.PutUint64(args, 7)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(stats.Funcs[0].CodeBytes), "native-bytes")
			mergeRegResidencyBenchmarkSink = binary.LittleEndian.Uint64(results)
		})
	}
}

func BenchmarkCompileMergeRegisterResidency(b *testing.B) {
	m := mergeRegResidencyBenchmarkModule(b)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"canonical-merges", false}, {"resident-merges", true}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"merge-reg-residency": tc.on, "inline": false}})
				if err != nil {
					b.Fatal(err)
				}
				benchCompiledSink = cm
			}
		})
	}
}
