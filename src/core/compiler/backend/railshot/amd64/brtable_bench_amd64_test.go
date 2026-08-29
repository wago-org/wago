//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func BenchmarkExecBrTableCompactTargetIDsAMD64(b *testing.B) {
	m := brTableLabelsInRAX(b, []uint32{0, 0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 3}, 4)
	for _, tc := range []struct {
		name    string
		compact bool
	}{
		{"ordinary", false},
		{"compact", true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			cm, err := CompileModuleWith(m, CompileOptions{CompactNative: tc.compact})
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

			args, results, trap := arena.Alloc(16), arena.Alloc(8), arena.Alloc(coreruntime.TrapBufferBytes)
			binary.LittleEndian.PutUint64(args, 7)
			binary.LittleEndian.PutUint64(args[8:], 1)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if got := binary.LittleEndian.Uint64(results); got != 1002 {
				b.Fatalf("result = %d, want 1002", got)
			}
		})
	}
}
