//go:build arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func entryZeroPairsModuleARM64(t testing.TB, locals, localType byte) *wasm.Module {
	t.Helper()
	body := []byte{0x01, locals, localType, 0x02, 0x40} // locals; block
	for i := byte(0); i < locals; i++ {
		body = append(body, 0x20, i, 0x1a) // local.get i; drop
	}
	body = append(body, 0x0b, 0x41, 0x07, 0x0b) // end block; i32.const 7; end
	return mod1(t, nil, []wasm.ValType{wasm.I32}, body)
}

func TestEntryZeroPairsARM64(t *testing.T) {
	for _, tc := range []struct {
		name      string
		locals    byte
		localType byte
		wantPairs int
	}{
		{name: "offset-cap-fallback", locals: 80, localType: 0x7e, wantPairs: 32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := entryZeroPairsModuleARM64(t, tc.locals, tc.localType)
			var offStats, onStats ModuleStats
			off, err := CompileModuleWith(m, CompileOptions{Stats: &offStats, Optimizations: map[string]bool{
				"entry-zero-pairs": false,
			}})
			if err != nil {
				t.Fatal(err)
			}
			on, err := CompileModuleWith(m, CompileOptions{Stats: &onStats, Optimizations: map[string]bool{
				"entry-zero-pairs": true,
			}})
			if err != nil {
				t.Fatal(err)
			}
			if got := onStats.Funcs[0].Peephole["entry-zero-pair"]; got != tc.wantPairs {
				t.Fatalf("entry-zero-pair = %d, want %d", got, tc.wantPairs)
			}
			if got, want := len(off.Code)-len(on.Code), tc.wantPairs*4; got != want {
				t.Fatalf("native reduction = %d, want %d", got, want)
			}
			got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{
				"entry-zero-pairs": true,
			}})
			if err != nil || got != 7 {
				t.Fatalf("result = %d, %v; want 7", got, err)
			}
		})
	}
}

func TestEntryZeroPairsV128ARM64(t *testing.T) {
	body := []byte{0x01, 0x02, 0x7b, 0x20, 0x01, 0x1a, 0x20, 0x00}
	body = append(body, simdOp(29)...)
	body = append(body, 0x01, 0x0b) // i64x2.extract_lane 1; end
	m := mod1(t, nil, []wasm.ValType{wasm.I64}, body)
	opts := CompileOptions{Stats: &ModuleStats{}, Optimizations: map[string]bool{
		"entry-zero-pairs": true,
		"v128-pins":        false,
	}}
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := opts.Stats.Funcs[0].Peephole["entry-zero-pair"]; got != 2 {
		t.Fatalf("entry-zero-pair = %d, want 2", got)
	}
	if len(cm.Code) == 0 {
		t.Fatal("empty native code")
	}
	if got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: opts.Optimizations}); err != nil || got != 0 {
		t.Fatalf("zero v128 lane = %d, %v; want 0", got, err)
	}
}

func BenchmarkEntryZeroPairsARM64(b *testing.B) {
	m := entryZeroPairsModuleARM64(b, 64, 0x70)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"single", false}, {"paired", true}} {
		b.Run(tc.name, func(b *testing.B) {
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{
				"entry-zero-pairs": tc.on,
			}})
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
			args, results, trap := arena.Alloc(8), arena.Alloc(8), arena.Alloc(coreruntime.TrapBufferBytes)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			if got := binary.LittleEndian.Uint32(results); got != 7 {
				b.Fatalf("result = %d, want 7", got)
			}
			b.ReportMetric(float64(stats.Funcs[0].CodeBytes), "native-bytes")
		})
	}
}

func BenchmarkCompileEntryZeroPairsARM64(b *testing.B) {
	m := entryZeroPairsModuleARM64(b, 64, 0x70)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"single", false}, {"paired", true}} {
		b.Run(tc.name, func(b *testing.B) {
			opts := CompileOptions{Optimizations: map[string]bool{
				"entry-zero-pairs": tc.on,
			}}
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
