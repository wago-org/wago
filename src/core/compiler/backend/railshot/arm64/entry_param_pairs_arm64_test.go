//go:build arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderarm64 "github.com/wago-org/wago/src/core/encoder/arm64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func parameterSumBody(n int) []byte {
	body := []byte{0x00}
	for i := range n {
		body = append(body, 0x20, byte(i))
		if i != 0 {
			body = append(body, 0x7c) // i64.add
		}
	}
	return append(body, 0x0b)
}

func parameterTypes(n int) []wasm.ValType {
	params := make([]wasm.ValType, n)
	for i := range params {
		params[i] = wasm.I64
	}
	return params
}

func TestEntryParamPairsWrapperARM64(t *testing.T) {
	const n = 32
	m := mod1(t, parameterTypes(n), []wasm.ValType{wasm.I64}, parameterSumBody(n))
	compile := func(on bool) (int, ModuleStats) {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{
			"entry-param-pairs": on,
			"reg-abi":           false,
		}})
		if err != nil {
			t.Fatal(err)
		}
		codeLen := len(cm.Code)
		if cm.CodeImage != nil {
			_ = cm.CodeImage.Close()
		}
		return codeLen, stats
	}
	off, _ := compile(false)
	on, onStats := compile(true)
	pairs := onStats.Funcs[0].Peephole["entry-param-pair-wrapper"]
	if pairs != 8 {
		t.Fatalf("wrapper parameter pairs = %d, want 8", pairs)
	}
	if got, want := off-on, pairs*8; got != want {
		t.Fatalf("native reduction = %d, want %d", got, want)
	}
	args := make([]uint64, n)
	for i := range args {
		args[i] = uint64(i + 1)
	}
	got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{
		"entry-param-pairs": true,
		"reg-abi":           false,
	}}, args...)
	if err != nil || got != 528 {
		t.Fatalf("sum = %d, %v; want 528", got, err)
	}
}

func TestEntryParamPairsOffsetCapARM64(t *testing.T) {
	f := fn{a: &encoderarm64.Asm{}, stats: &CodegenStats{}}
	p := f.queueWrapperParamHome(pendingWrapperParamHome{}, 504, 504)
	p = f.queueWrapperParamHome(p, 512, 512)
	if p.valid || f.stats.Peephole["entry-param-pair-wrapper"] != 1 {
		t.Fatalf("in-range pair = %+v, stats=%v", p, f.stats.Peephole)
	}
	p = f.queueWrapperParamHome(pendingWrapperParamHome{}, 512, 512)
	p = f.queueWrapperParamHome(p, 520, 520)
	f.flushWrapperParamHome(p)
	if got := f.stats.Peephole["entry-param-pair-wrapper"]; got != 1 {
		t.Fatalf("out-of-range pair count = %d, want 1", got)
	}
}

func BenchmarkEntryParamPairsARM64(b *testing.B) {
	const n = 32
	m := mod1(b, parameterTypes(n), []wasm.ValType{wasm.I64}, parameterSumBody(n))
	for _, tc := range []struct {
		name string
		on   bool
	}{{"single", false}, {"paired", true}} {
		b.Run(tc.name, func(b *testing.B) {
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{
				"entry-param-pairs": tc.on,
				"reg-abi":           false,
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
			args, results, trap := arena.Alloc(n*8), arena.Alloc(8), arena.Alloc(8)
			for i := range n {
				binary.LittleEndian.PutUint64(args[i*8:], uint64(i+1))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			if got := binary.LittleEndian.Uint64(results); got != 528 {
				b.Fatalf("sum = %d, want 528", got)
			}
			b.ReportMetric(float64(stats.Funcs[0].CodeBytes), "native-bytes")
		})
	}
}

func BenchmarkCompileEntryParamPairsARM64(b *testing.B) {
	const n = 32
	m := mod1(b, parameterTypes(n), []wasm.ValType{wasm.I64}, parameterSumBody(n))
	for _, tc := range []struct {
		name string
		on   bool
	}{{"single", false}, {"paired", true}} {
		b.Run(tc.name, func(b *testing.B) {
			opts := CompileOptions{Optimizations: map[string]bool{
				"entry-param-pairs": tc.on,
				"reg-abi":           false,
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
