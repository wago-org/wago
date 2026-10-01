//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestLinearSumSignalsRangeAndRemainders(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved := linearSumSignalsEnabled
	defer func() { linearSumSignalsEnabled = saved }()
	for _, signals := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			linearSumSignalsEnabled = enabled
			m := linearSumWrappingModuleAMD64(t)
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: signals, Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			if got := stats.Funcs[0].Peephole["linear-sum-signals"] != 0; got != (signals && enabled) {
				t.Fatal("incorrect signal admission", signals, enabled, stats.Funcs[0].Peephole)
			}
			if signals && enabled && stats.Funcs[0].Peephole["linear-sum-unroll4"] == 0 {
				t.Fatal("signal fixture did not unroll")
			}
			eng, err := coreruntime.NewEngine()
			if err != nil {
				t.Fatal(err)
			}
			jm, err := coreruntime.NewJobMemory(65536)
			if err != nil {
				t.Fatal(err)
			}
			mem := jm.CurrentBytes()
			for i := range mem {
				mem[i] = byte(i*17 + 23)
			}
			arena, err := coreruntime.NewArena(4096)
			if err != nil {
				t.Fatal(err)
			}
			code, entry, err := coreruntime.MapCode(cm.Code)
			if err != nil {
				t.Fatal(err)
			}
			args, results, trap := arena.Alloc(16), arena.Alloc(8), arena.Alloc(coreruntime.TrapBufferBytes)
			for _, start := range []uint32{0, 1, 65528, 65529, 0xfffffff8, 0xfffffff9} {
				for _, n := range []uint32{0, 1, 2, 3, 4, 5, 7, 8, 9, 31, 8191, 8192, 8193, 0xffffffff} {
					binary.LittleEndian.PutUint32(args, start)
					binary.LittleEndian.PutUint32(args[8:], n)
					err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results)
					if n != 0 && uint64(start)+uint64(n)*8 > uint64(len(mem)) {
						if err == nil {
							t.Fatal("missing bounds trap", signals, enabled, start, n)
						}
						continue
					}
					if err != nil {
						t.Fatal(signals, enabled, start, n, err)
					}
					var want uint64
					for i := uint32(0); i < n; i++ {
						want += binary.LittleEndian.Uint64(mem[start+i*8:])
					}
					if got := binary.LittleEndian.Uint64(results); got != want {
						t.Fatalf("signals=%v enabled=%v start=%d n=%d: %#x want %#x", signals, enabled, start, n, got, want)
					}
				}
			}
			coreruntime.Unmap(code)
			arena.Close()
			jm.Close()
			eng.Close()
			cm.CodeImage.Close()
		}
	}
}

func TestLinearSumSignalsRejectsSharedAndInterruptible(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved := linearSumSignalsEnabled
	defer func() { linearSumSignalsEnabled = saved }()
	linearSumSignalsEnabled = true
	for _, shared := range []bool{false, true} {
		m := linearSumLoopModuleAMD64(t)
		m.Memories[0].Shared = shared
		m.Memories[0].Limits.HasMax, m.Memories[0].Limits.Max = true, 1
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: true, Interruptible: !shared, Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		if stats.Funcs[0].Peephole["linear-sum-signals"] != 0 || stats.Funcs[0].Peephole["linear-sum-unroll4"] != 0 {
			t.Fatal("unsafe signal admission", shared, stats.Funcs[0].Peephole)
		}
	}
}

func TestLinearSumSignalsMemory32WrappingGroups(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved := linearSumSignalsEnabled
	defer func() { linearSumSignalsEnabled = saved }()
	linearSumSignalsEnabled = true
	m := linearSumWrappingModuleAMD64(t)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: true, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	if stats.Funcs[0].Peephole["linear-sum-signals"] != 1 || stats.Funcs[0].Peephole["linear-sum-unroll4"] != 1 {
		t.Fatal("wrapping fixture did not use signal unroll", stats.Funcs[0].Peephole)
	}
	eng, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := coreruntime.NewJobMemory(1 << 32)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	mem := jm.CurrentBytes()
	for offset, value := range map[uint64]uint64{0xfffffff0: 3, 0xfffffff8: 11, 0: 7, 8: 13, 16: 17} {
		binary.LittleEndian.PutUint64(mem[offset:], value)
	}
	arena, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	code, entry, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.Unmap(code)
	args, results, trap := arena.Alloc(16), arena.Alloc(8), arena.Alloc(coreruntime.TrapBufferBytes)
	for _, c := range []struct {
		start, count uint32
		want         uint64
		fail         bool
	}{
		{0xfffffff0, 5, 51, false}, {0xfffffff8, 4, 48, false},
		{0xfffffff9, 1, 0, true}, {0xfffffff9, 0, 0, false},
	} {
		binary.LittleEndian.PutUint32(args, c.start)
		binary.LittleEndian.PutUint32(args[8:], c.count)
		err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results)
		if c.fail {
			if err == nil {
				t.Fatal("missing wrapping load trap", c)
			}
			continue
		}
		if err != nil {
			t.Fatal(c, err)
		}
		if got := binary.LittleEndian.Uint64(results); got != c.want {
			t.Fatal(c, got)
		}
	}
}
