//go:build arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestEntryZeroPairsARM64(t *testing.T) {
	for _, tc := range []struct {
		name      string
		locals    byte
		wantPairs int
	}{
		{name: "within-offset-range", locals: 8, wantPairs: 1},
		{name: "offset-cap-fallback", locals: 80, wantPairs: 32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0x01, tc.locals, 0x7e, 0x41, 0x07, 0x0b}
			m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
			var offStats, onStats ModuleStats
			off, err := CompileModuleWith(m, CompileOptions{Stats: &offStats, Optimizations: map[string]bool{
				"entry-init-elide": false,
				"entry-zero-pairs": false,
			}})
			if err != nil {
				t.Fatal(err)
			}
			on, err := CompileModuleWith(m, CompileOptions{Stats: &onStats, Optimizations: map[string]bool{
				"entry-init-elide": false,
				"entry-zero-pairs": true,
			}})
			if err != nil {
				t.Fatal(err)
			}
			if got := onStats.Funcs[0].Peephole["entry-zero-pair"]; got != tc.wantPairs {
				t.Fatalf("entry-zero-pair = %d, want %d", got, tc.wantPairs)
			}
			offStores := offStats.Funcs[0].LocalTraffic.DeclaredLocalZeroStores
			if got, want := onStats.Funcs[0].LocalTraffic.DeclaredLocalZeroStores, offStores-tc.wantPairs; got != want {
				t.Fatalf("enabled zero stores = %d, want %d", got, want)
			}
			if tc.name == "offset-cap-fallback" && onStats.Funcs[0].LocalTraffic.DeclaredLocalZeroStores <= tc.wantPairs {
				t.Fatalf("offset cap did not retain single-store fallback: %+v", onStats.Funcs[0].LocalTraffic)
			}
			if got, want := len(off.Code)-len(on.Code), tc.wantPairs*4; got != want {
				t.Fatalf("native reduction = %d, want %d", got, want)
			}
			got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{
				"entry-init-elide": false,
				"entry-zero-pairs": true,
			}})
			if err != nil || got != 7 {
				t.Fatalf("result = %d, %v; want 7", got, err)
			}
		})
	}
}

func TestEntryZeroPairsV128ARM64(t *testing.T) {
	body := []byte{0x01, 0x02, 0x7b, 0x20, 0x00}
	body = append(body, simdOp(29)...)
	body = append(body, 0x01, 0x0b) // i64x2.extract_lane 1; end
	m := mod1(t, nil, []wasm.ValType{wasm.I64}, body)
	opts := CompileOptions{Stats: &ModuleStats{}, Optimizations: map[string]bool{
		"entry-init-elide": false,
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
	m := mod1(b, nil, []wasm.ValType{wasm.I32}, []byte{0x01, 0x40, 0x7e, 0x41, 0x07, 0x0b})
	for _, tc := range []struct {
		name string
		on   bool
	}{{"single", false}, {"paired", true}} {
		b.Run(tc.name, func(b *testing.B) {
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{
				"entry-init-elide": false,
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
			args, results, trap := arena.Alloc(8), arena.Alloc(8), arena.Alloc(8)
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
	m := mod1(b, nil, []wasm.ValType{wasm.I32}, []byte{0x01, 0x40, 0x7e, 0x41, 0x07, 0x0b})
	for _, tc := range []struct {
		name string
		on   bool
	}{{"single", false}, {"paired", true}} {
		b.Run(tc.name, func(b *testing.B) {
			opts := CompileOptions{Optimizations: map[string]bool{
				"entry-init-elide": false,
				"entry-zero-pairs": tc.on,
			}}
			b.ReportAllocs()
			for range b.N {
				if _, err := CompileModuleWith(m, opts); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
