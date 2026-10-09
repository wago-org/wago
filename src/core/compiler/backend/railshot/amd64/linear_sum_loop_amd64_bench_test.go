//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"fmt"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func BenchmarkLinearSumNoWrapAMD64(b *testing.B) {
	m := linearSumLoopModuleAMD64(b)
	cm, err := CompileModule(m)
	if err != nil {
		b.Fatal(err)
	}
	defer cm.CodeImage.Close()
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
	for i := 0; i < 8192; i++ {
		binary.LittleEndian.PutUint64(jm.CurrentBytes()[i*8:], uint64(i+1))
	}
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
	args, results, trap := arena.Alloc(8), arena.Alloc(8), arena.Alloc(coreruntime.TrapBufferBytes)
	for _, count := range []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 15, 16, 17, 511, 512, 513, 8192} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			binary.LittleEndian.PutUint32(args, count)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if got, want := binary.LittleEndian.Uint64(results), uint64(count)*uint64(count+1)/2; got != want {
				b.Fatalf("sum = %d, want %d", got, want)
			}
		})
	}
}

func BenchmarkCompileLinearSumAMD64(b *testing.B) {
	m := linearSumLoopModuleAMD64(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cm, err := CompileModule(m)
		if err != nil {
			b.Fatal(err)
		}
		cm.CodeImage.Close()
	}
}
