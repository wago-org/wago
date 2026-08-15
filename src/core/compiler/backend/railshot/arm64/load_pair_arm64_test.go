//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func loadPairModuleARM64(t testing.TB, secondOffset byte) *wasm.Module {
	t.Helper()
	return modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x00,
		0x20, 0x00, 0x28, 0x02, 0x00,
		0x20, 0x00, 0x28, 0x02, secondOffset,
		0x6a,
		0x0b,
	})
}

func loadPairI64ModuleARM64(t testing.TB) *wasm.Module {
	t.Helper()
	return modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, []byte{
		0x00,
		0x20, 0x00, 0x29, 0x03, 0x00,
		0x20, 0x00, 0x29, 0x03, 0x08,
		0x7c,
		0x0b,
	})
}

func loadPairBenchModuleARM64(b testing.TB) *wasm.Module {
	body := []byte{0x01, 0x01, 0x7f} // one i32 accumulator local
	for i := byte(0); i < 16; i++ {
		off := i * 8
		body = append(body,
			0x20, 0x01,
			0x20, 0x00, 0x28, 0x02, off,
			0x20, 0x00, 0x28, 0x02, off+4,
			0x6a, 0x6a, 0x21, 0x01,
		)
	}
	body = append(body, 0x20, 0x01, 0x0b)
	return modMem(b, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
}

func TestLoadPairExecArm64(t *testing.T) {
	m := loadPairModuleARM64(t, 4)
	got, err := runArm64WrapperMem(t, m, 16, func(mem []byte) {
		binary.LittleEndian.PutUint32(mem[16:], 123)
		binary.LittleEndian.PutUint32(mem[20:], 456)
	})
	if err != nil || got != 579 {
		t.Fatalf("paired load result = %d, %v; want 579", got, err)
	}
	got, err = runArm64WrapperMem(t, loadPairI64ModuleARM64(t), 16, func(mem []byte) {
		binary.LittleEndian.PutUint64(mem[16:], 123)
		binary.LittleEndian.PutUint64(mem[24:], 456)
	})
	if err != nil || got != 579 {
		t.Fatalf("paired i64 load result = %d, %v; want 579", got, err)
	}
	if _, err := runArm64WrapperMem(t, m, 65532, nil); err == nil {
		t.Fatal("paired load with an out-of-bounds second lane did not trap")
	}
}

func BenchmarkLoadPairArm64(b *testing.B) {
	for _, enabled := range []bool{true, false} {
		name := "enabled"
		if !enabled {
			name = "disabled"
		}
		b.Run(name, func(b *testing.B) {
			cm, err := CompileModuleWith(loadPairBenchModuleARM64(b), CompileOptions{Optimizations: map[string]bool{"load-pair": enabled}})
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
			for off := 0; off < 256; off += 4 {
				binary.LittleEndian.PutUint32(jm.CurrentBytes()[off:], 1)
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
			args, results, trap := arena.Alloc(8), arena.Alloc(8), arena.Alloc(8)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(len(cm.Code)), "code-B")
			if got := binary.LittleEndian.Uint32(results); got != 32 {
				b.Fatalf("result = %d, want 32", got)
			}
		})
	}
}

func BenchmarkCompileLoadPairArm64(b *testing.B) {
	m := loadPairBenchModuleARM64(b)
	for _, enabled := range []bool{true, false} {
		name := "enabled"
		if !enabled {
			name = "disabled"
		}
		b.Run(name, func(b *testing.B) {
			opts := CompileOptions{Workers: 1, Optimizations: map[string]bool{"load-pair": enabled}}
			b.ReportAllocs()
			for range b.N {
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					b.Fatal(err)
				}
				if cm.CodeImage != nil {
					_ = cm.CodeImage.Close()
				}
			}
		})
	}
}

func TestLoadPairFiresAndNearMissesArm64(t *testing.T) {
	var stats ModuleStats
	paired, err := CompileModuleWith(loadPairModuleARM64(t, 4), CompileOptions{Stats: &stats, Optimizations: map[string]bool{"load-pair": true}})
	if err != nil {
		t.Fatal(err)
	}
	if got := stats.Funcs[0].Peephole["load-pair"]; got != 1 {
		t.Fatalf("load-pair = %d, want 1 (all: %v)", got, stats.Funcs[0].Peephole)
	}
	unpaired, err := CompileModuleWith(loadPairModuleARM64(t, 4), CompileOptions{Optimizations: map[string]bool{"load-pair": false}})
	if err != nil {
		t.Fatal(err)
	}
	if len(paired.Code) >= len(unpaired.Code) {
		t.Fatalf("paired code = %d bytes, want less than unpaired %d", len(paired.Code), len(unpaired.Code))
	}
	var i64Stats ModuleStats
	if _, err := CompileModuleWith(loadPairI64ModuleARM64(t), CompileOptions{Stats: &i64Stats, Optimizations: map[string]bool{"load-pair": true}}); err != nil {
		t.Fatal(err)
	}
	if got := i64Stats.Funcs[0].Peephole["load-pair"]; got != 1 {
		t.Fatalf("i64 load-pair = %d, want 1", got)
	}

	for _, tc := range []struct {
		name  string
		off   byte
		guard bool
	}{
		{name: "nonadjacent", off: 8},
		{name: "guard", off: 4, guard: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stats ModuleStats
			if _, err := CompileModuleWith(loadPairModuleARM64(t, tc.off), CompileOptions{Stats: &stats, ElideBoundsChecks: tc.guard, Optimizations: map[string]bool{"load-pair": true}}); err != nil {
				t.Fatal(err)
			}
			if got := stats.Funcs[0].Peephole["load-pair"]; got != 0 {
				t.Fatalf("load-pair near miss = %d, want 0", got)
			}
		})
	}

	shared := loadPairModuleARM64(t, 4)
	shared.Memories[0].Shared = true
	shared.Memories[0].Limits.HasMax = true
	shared.Memories[0].Limits.Max = 1
	var sharedStats ModuleStats
	if _, err := CompileModuleWith(shared, CompileOptions{Stats: &sharedStats, Optimizations: map[string]bool{"load-pair": true}}); err != nil {
		t.Fatal(err)
	}
	if got := sharedStats.Funcs[0].Peephole["load-pair"]; got != 0 {
		t.Fatalf("shared-memory load-pair = %d, want 0", got)
	}

	differentBase := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x00,
		0x20, 0x00, 0x28, 0x02, 0x00,
		0x20, 0x01, 0x28, 0x02, 0x04,
		0x6a, 0x0b,
	})
	var differentStats ModuleStats
	if _, err := CompileModuleWith(differentBase, CompileOptions{Stats: &differentStats, Optimizations: map[string]bool{"load-pair": true}}); err != nil {
		t.Fatal(err)
	}
	if got := differentStats.Funcs[0].Peephole["load-pair"]; got != 0 {
		t.Fatalf("different-base load-pair = %d, want 0", got)
	}
}
