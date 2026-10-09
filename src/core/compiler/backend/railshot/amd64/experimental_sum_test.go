//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestExperimentalSumMatrix(t *testing.T) {
	saved := shared.SumExperiment
	defer func() { shared.SumExperiment = saved }()
	for _, variant := range []string{"A", "B", "C", "D", "E", "F", "G"} {
		t.Run(variant, func(t *testing.T) {
			shared.SumExperiment = variant
			m := linearSumWrappingModuleAMD64(t)
			for _, start := range []uint64{0, 1, 8, 127, 65528} {
				for _, count := range []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 15, 16, 17, 511, 512, 513} {
					init := func(mem []byte) {
						for i := range mem {
							mem[i] = byte(i*19 + 7)
						}
					}
					want := uint64(0)
					raw := make([]byte, 65536)
					init(raw)
					trap := start+count*8 > 65536
					if !trap {
						for i := uint64(0); i < count; i++ {
							want += binary.LittleEndian.Uint64(raw[start+8*i:])
						}
					}
					got, _, err := runMemAmd64(t, m, init, start, count)
					if (err != nil) != trap || !trap && got != want {
						t.Fatalf("start=%d count=%d got=%x want=%x err=%v", start, count, got, want, err)
					}
				}
			}
		})
	}
}

func TestExperimentalSumGroupedFullMemoryWrap(t *testing.T) {
	saved := shared.SumExperiment
	defer func() { shared.SumExperiment = saved }()
	jm, err := coreruntime.NewJobMemory(1 << 32)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	eng, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	arena, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	args, out, trap := arena.Alloc(16), arena.Alloc(8), arena.Alloc(coreruntime.TrapBufferBytes)
	for _, variant := range []string{"A", "B", "C", "D", "E", "F", "G"} {
		shared.SumExperiment = variant
		cm, err := CompileModule(linearSumWrappingModuleAMD64(t))
		if err != nil {
			t.Fatal(err)
		}
		code, entry, err := coreruntime.MapCode(cm.Code)
		if err != nil {
			t.Fatal(err)
		}
		mem := jm.CurrentBytes()
		for i := uint32(0); i < 16; i++ {
			at := uint32(0xfffffff0 + i*8)
			binary.LittleEndian.PutUint64(mem[uint64(at):], uint64(i+1))
		}
		for _, count := range []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 15, 16} {
			binary.LittleEndian.PutUint32(args, 0xfffffff0)
			binary.LittleEndian.PutUint32(args[8:], count)
			if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, out); err != nil {
				t.Fatal(variant, count, err)
			}
			if got, want := binary.LittleEndian.Uint64(out), uint64(count)*uint64(count+1)/2; got != want {
				t.Fatal(variant, count, got, want)
			}
		}
		binary.LittleEndian.PutUint32(args, 0xfffffff9)
		binary.LittleEndian.PutUint32(args[8:], 5)
		if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, out); err == nil {
			t.Fatal("boundary access did not trap", variant)
		}
		coreruntime.Unmap(code)
		cm.CodeImage.Close()
	}
}

func TestExperimentalSumSizes(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved := shared.SumExperiment
	defer func() { shared.SumExperiment = saved }()
	for _, variant := range []string{"", "A", "B", "C", "D", "E", "F", "G"} {
		shared.SumExperiment = variant
		var stats ModuleStats
		cm, err := CompileModuleWith(linearSumLoopModuleAMD64(t), CompileOptions{Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		s := stats.Funcs[0]
		t.Logf("variant=%q module=%d function=%d spills=%d reloads=%d frame=%d labels=%v", variant, len(cm.Code), s.CodeBytes, s.Spills, s.Reloads, s.FrameBytes, s.Peephole)
		cm.CodeImage.Close()
	}
}
