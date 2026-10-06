//go:build amd64 && (linux || darwin || windows)

package amd64

import (
	"encoding/binary"
	"fmt"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func BenchmarkBoundsFactsExecute(b *testing.B) {
	for _, sources := range []int{1, 2, 8, 9, 32} {
		for _, guard := range []bool{false, true} {
			b.Run(fmt.Sprintf("sources=%d/guard=%v", sources, guard), func(b *testing.B) {
				m := boundsResearchModule(b, sources, 32)
				cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, ElideBoundsChecks: guard})
				if err != nil {
					b.Fatal(err)
				}
				defer cm.CodeImage.Close()
				engine, err := coreruntime.NewEngine()
				if err != nil {
					b.Fatal(err)
				}
				defer engine.Close()
				memory, err := coreruntime.NewJobMemory(65536)
				if err != nil {
					b.Fatal(err)
				}
				defer memory.Close()
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
				args, results, trap := arena.Alloc(sources*8), arena.Alloc(8), arena.Alloc(coreruntime.TrapBufferBytes)
				for i := 0; i < sources; i++ {
					binary.LittleEndian.PutUint64(args[i*8:], uint64(i*4))
				}
				lin := memory.LinearMemory()
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := engine.Call(entry+uintptr(cm.Entry[0]), args, lin, trap, results); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(len(cm.Code)), "native-B")
			})
		}
	}
}
