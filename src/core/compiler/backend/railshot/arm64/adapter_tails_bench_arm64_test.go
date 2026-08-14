//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func BenchmarkExecSharedAdapterTailArm64(b *testing.B) {
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(b,
		funcDef{nil, i32, []byte{0x00, 0x41, 0x01, 0x0b}},
		funcDef{nil, i32, []byte{0x00, 0x41, 0x02, 0x0b}},
	)
	m.Exports = append(m.Exports, wasm.Export{Name: "g", Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 1}})
	for _, tc := range []struct {
		name      string
		objective OptimizationObjective
	}{
		{"balanced", OptimizeBalanced},
		{"size", OptimizeSize},
	} {
		b.Run(tc.name, func(b *testing.B) {
			cm, err := CompileModuleWith(m, CompileOptions{Objective: &tc.objective})
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
			results, trap := arena.Alloc(8), arena.Alloc(8)
			b.ReportMetric(float64(len(cm.Code)), "code-B")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), nil, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if got := binary.LittleEndian.Uint64(results); got != 1 {
				b.Fatalf("result = %d, want 1", got)
			}
		})
	}
}
