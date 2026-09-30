//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func BenchmarkExecMixedCallPressureARM64(b *testing.B) {
	cases := []struct {
		name           string
		nargs          int
		computed, wide bool
		variant        string
	}{
		{name: "borrowed4", nargs: 4},
		{name: "borrowed8", nargs: 8},
		{name: "computed", nargs: 8, computed: true},
		{name: "wide", nargs: 5, computed: true, wide: true},
		{name: "canonical", nargs: 8, variant: "canonical"},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			m, want := mixedCallVariantModule(b, true, tc.nargs, tc.computed, tc.wide, tc.variant)
			cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"inline": false}})
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
			results, trap := arena.Alloc(8), arena.Alloc(coreruntime.TrapBufferBytes)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), nil, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if got := binary.LittleEndian.Uint64(results); got != want {
				b.Fatalf("result = %#x, want %#x", got, want)
			}
		})
	}
}
