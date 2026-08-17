//go:build (linux || darwin) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func BenchmarkForwardMergeNextUse(b *testing.B) {
	i32 := []wasm.ValType{wasm.I32}
	body := []byte{0x00}
	for range 32 {
		body = append(body,
			0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00,
			0x10, 0x01,
			0x41, 0x00, 0x04, 0x40, 0x20, 0x00, 0x1a, 0x0b,
			0x41, 0x09, 0x21, 0x00,
		)
	}
	body = append(body, 0x41, 0x07, 0x0b)
	m := modFuncs(b,
		funcDef{params: i32, results: i32, body: body},
		funcDef{body: []byte{0x00, 0x0b}},
	)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"reload", false}, {"lazy", true}} {
		b.Run(tc.name, func(b *testing.B) {
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"inline": false, "merge-next-use": tc.on}})
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
