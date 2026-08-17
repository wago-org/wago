//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime"
)

func simdI16x8BitmaskNonZeroBodyAMD64(v [16]byte, compare byte) []byte {
	body := []byte{0x00}
	body = append(body, v128ConstBytes(v)...)
	body = append(body, simdOp(132)...)
	return append(body, 0x41, compare, 0x47, 0x0b) // i32.const; i32.ne; end
}

func simdI16x8BitmaskZeroBranchBodyAMD64(v [16]byte, brIf bool) []byte {
	body := []byte{0x00}
	if brIf {
		body = append(body, 0x02, 0x7f, 0x41, 0x01) // block (result i32); branch value 1
	}
	body = append(body, v128ConstBytes(v)...)
	body = append(body, simdOp(132)...)
	body = append(body, 0x45) // i32.eqz
	if brIf {
		return append(body, 0x0d, 0x00, 0x1a, 0x41, 0x00, 0x0b, 0x0b) // br_if 0; drop; 0; end; end
	}
	return append(body, 0x04, 0x7f, 0x41, 0x01, 0x05, 0x41, 0x00, 0x0b, 0x0b) // if 1 else 0; end
}

func TestSIMDI16x8BitmaskZeroBranchAMD64(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    [16]byte
		want uint64
	}{
		{"zero", i16x8Bytes(0, 1, 2, 32767), 1},
		{"low", i16x8Bytes(-1), 0},
		{"high", i16x8Bytes(0, 0, 0, 0, 0, 0, 0, -32768), 0},
	} {
		for _, brIf := range []bool{false, true} {
			branch := "if"
			if brIf {
				branch = "br_if"
			}
			t.Run(tc.name+"/"+branch, func(t *testing.T) {
				m := mod1(t, nil, []wasm.ValType{wasm.I32}, simdI16x8BitmaskZeroBranchBodyAMD64(tc.v, brIf))
				compile := func(enabled bool) (*encoderamd64.CompiledModule, *CodegenStats) {
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{
						Stats:         &stats,
						Optimizations: map[string]bool{"simd-wide-bitmask-consumer": enabled},
					})
					if err != nil {
						t.Fatal(err)
					}
					return cm, stats.Funcs[0]
				}
				onModule, on := compile(true)
				offModule, off := compile(false)
				if onModule.CodeImage != nil {
					t.Cleanup(func() { _ = onModule.CodeImage.Close() })
				}
				if offModule.CodeImage != nil {
					t.Cleanup(func() { _ = offModule.CodeImage.Close() })
				}
				if got := on.Peephole["simd-bitmask-zero-branch"]; got != 1 {
					t.Fatalf("simd-bitmask-zero-branch hits = %d, want 1 (all: %v)", got, on.Peephole)
				}
				if got := off.Peephole["simd-bitmask-zero-branch"]; got != 0 {
					t.Fatalf("disabled simd-bitmask-zero-branch hits = %d, want 0", got)
				}
				if on.CodeBytes >= off.CodeBytes {
					t.Fatalf("fused/unfused code = %d/%d bytes, want reduction", on.CodeBytes, off.CodeBytes)
				}
				if got := runCompiledAmd64u(t, onModule); got != tc.want {
					t.Fatalf("fused result = %d, want %d", got, tc.want)
				}
				if got := runCompiledAmd64u(t, offModule); got != tc.want {
					t.Fatalf("unfused result = %d, want %d", got, tc.want)
				}
			})
		}
	}
}

func TestSIMDI16x8BitmaskZeroBranchNearMissAMD64(t *testing.T) {
	body := []byte{0x00}
	body = append(body, v128ConstBytes(i16x8Bytes(-1))...)
	body = append(body, simdOp(132)...)
	body = append(body, 0x45, 0x41, 0x01, 0x6a, 0x0b) // eqz; i32.const 1; add; end
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if got := stats.Funcs[0].Peephole["simd-bitmask-zero-branch-candidate"]; got != 0 {
		t.Fatalf("non-branch candidate hits = %d, want 0", got)
	}
}

func TestSIMDI16x8BitmaskNonZeroConsumerAMD64(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    [16]byte
		want uint64
	}{
		{"none", i16x8Bytes(0, 1, 2, 32767), 0},
		{"low", i16x8Bytes(-1), 1},
		{"high", i16x8Bytes(0, 0, 0, 0, 0, 0, 0, -32768), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, nil, []wasm.ValType{wasm.I32}, simdI16x8BitmaskNonZeroBodyAMD64(tc.v, 0))
			compile := func(enabled bool) (*encoderamd64.CompiledModule, *CodegenStats) {
				var stats ModuleStats
				cm, err := CompileModuleWith(m, CompileOptions{
					Stats:         &stats,
					Optimizations: map[string]bool{"simd-wide-bitmask-consumer": enabled},
				})
				if err != nil {
					t.Fatal(err)
				}
				return cm, stats.Funcs[0]
			}
			onModule, on := compile(true)
			offModule, off := compile(false)
			if onModule.CodeImage != nil {
				t.Cleanup(func() { _ = onModule.CodeImage.Close() })
			}
			if offModule.CodeImage != nil {
				t.Cleanup(func() { _ = offModule.CodeImage.Close() })
			}
			if got := on.Peephole["simd-bitmask-nonzero"]; got != 1 {
				t.Fatalf("simd-bitmask-nonzero hits = %d, want 1 (all: %v)", got, on.Peephole)
			}
			if got := off.Peephole["simd-bitmask-nonzero"]; got != 0 {
				t.Fatalf("disabled simd-bitmask-nonzero hits = %d, want 0", got)
			}
			if on.CodeBytes >= off.CodeBytes {
				t.Fatalf("fused/unfused code = %d/%d bytes, want reduction", on.CodeBytes, off.CodeBytes)
			}
			t.Logf("fused/unfused code = %d/%d bytes", on.CodeBytes, off.CodeBytes)
			if got := runCompiledAmd64u(t, onModule); got != tc.want {
				t.Fatalf("fused result = %d, want %d", got, tc.want)
			}
			if got := runCompiledAmd64u(t, offModule); got != tc.want {
				t.Fatalf("unfused result = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSIMDI16x8BitmaskNonZeroRejectsOtherConstantAMD64(t *testing.T) {
	body := simdI16x8BitmaskNonZeroBodyAMD64(i16x8Bytes(-1), 1)
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if got := stats.Funcs[0].Peephole["simd-bitmask-nonzero"]; got != 0 {
		t.Fatalf("simd-bitmask-nonzero hits = %d, want 0", got)
	}
	if got := runAmd64u(t, m); got != 0 {
		t.Fatalf("bitmask != 1 = %d, want 0", got)
	}
}

func TestSIMDI16x8BitmaskNonZeroRejectsPopcntAMD64(t *testing.T) {
	body := []byte{0x00}
	body = append(body, v128ConstBytes(i16x8Bytes(-1, 0, -32768, 7, 8, -2, 10, 11))...)
	body = append(body, simdOp(132)...)
	body = append(body, 0x69, 0x0b) // i32.popcnt; end
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if got := stats.Funcs[0].Peephole["simd-bitmask-nonzero"]; got != 0 {
		t.Fatalf("simd-bitmask-nonzero hits = %d, want 0", got)
	}
	if got := runAmd64u(t, m); got != 3 {
		t.Fatalf("popcnt(bitmask) = %d, want 3", got)
	}
}

func repeatedI16x8BitmaskNonZeroBodyAMD64(n int) []byte {
	body := []byte{0x00}
	for i := 0; i < n; i++ {
		body = append(body, v128ConstBytes(i16x8Bytes(-1, 0, -32768, 7, 8, -2, 10, 11))...)
		body = append(body, simdOp(132)...)
		body = append(body, 0x41, 0x00, 0x47) // i32.const 0; i32.ne
		if i != 0 {
			body = append(body, 0x6a) // i32.add
		}
	}
	return append(body, 0x0b)
}

func repeatedI16x8BitmaskZeroBranchBodyAMD64(n int) []byte {
	body := []byte{0x01, 0x01, 0x7f} // one i32 local accumulator
	for range n {
		body = append(body, v128ConstBytes(i16x8Bytes(0, 1, 2, 3, 4, 5, 6, 7))...)
		body = append(body, simdOp(132)...)
		body = append(body,
			0x45,       // i32.eqz
			0x04, 0x40, // if
			0x20, 0x00, // local.get 0
			0x41, 0x01, // i32.const 1
			0x6a,       // i32.add
			0x21, 0x00, // local.set 0
			0x0b, // end if
		)
	}
	return append(body, 0x20, 0x00, 0x0b) // local.get 0; end
}

var benchmarkSIMDBitmaskResultAMD64 uint64

func benchmarkSIMDBitmaskExecAMD64(b *testing.B, enabled bool) {
	b.Helper()
	m := mod1(b, nil, []wasm.ValType{wasm.I32}, repeatedI16x8BitmaskNonZeroBodyAMD64(64))
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{
		Stats:         &stats,
		Optimizations: map[string]bool{"simd-wide-bitmask-consumer": enabled},
	})
	if err != nil {
		b.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		b.Fatal(err)
	}
	defer jm.Close()
	ar, err := runtime.NewArena(4096)
	if err != nil {
		b.Fatal(err)
	}
	defer ar.Close()
	code, entry, err := runtime.MapCode(cm.Code)
	if err != nil {
		b.Fatal(err)
	}
	defer runtime.Unmap(code)
	args := ar.Alloc(8)
	results := ar.Alloc(8)
	trap := ar.Alloc(8)
	callEntry := entry + uintptr(cm.Entry[0])
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := eng.Call(callEntry, args, jm.LinearMemory(), trap, results); err != nil {
			b.Fatal(err)
		}
	}
	benchmarkSIMDBitmaskResultAMD64 = binary.LittleEndian.Uint64(results)
	b.ReportMetric(float64(stats.Funcs[0].CodeBytes), "code-B")
}

func BenchmarkSIMDI16x8BitmaskNonZeroConsumerAMD64(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "unfused"
		if enabled {
			name = "fused"
		}
		b.Run(name, func(b *testing.B) { benchmarkSIMDBitmaskExecAMD64(b, enabled) })
	}
}

func BenchmarkSIMDI16x8BitmaskZeroBranchAMD64(b *testing.B) {
	body := repeatedI16x8BitmaskZeroBranchBodyAMD64(64)
	for _, enabled := range []bool{false, true} {
		name := "unfused"
		if enabled {
			name = "fused"
		}
		b.Run(name, func(b *testing.B) {
			m := mod1(b, nil, []wasm.ValType{wasm.I32}, body)
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{
				Stats:         &stats,
				Optimizations: map[string]bool{"simd-wide-bitmask-consumer": enabled},
			})
			if err != nil {
				b.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			eng, err := runtime.NewEngine()
			if err != nil {
				b.Fatal(err)
			}
			defer eng.Close()
			jm, err := runtime.NewJobMemory(65536)
			if err != nil {
				b.Fatal(err)
			}
			defer jm.Close()
			ar, err := runtime.NewArena(4096)
			if err != nil {
				b.Fatal(err)
			}
			defer ar.Close()
			code, entry, err := runtime.MapCode(cm.Code)
			if err != nil {
				b.Fatal(err)
			}
			defer runtime.Unmap(code)
			args, results, trap := ar.Alloc(8), ar.Alloc(8), ar.Alloc(8)
			callEntry := entry + uintptr(cm.Entry[0])
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := eng.Call(callEntry, args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			benchmarkSIMDBitmaskResultAMD64 = binary.LittleEndian.Uint64(results)
			b.ReportMetric(float64(stats.Funcs[0].CodeBytes), "code-B")
		})
	}
}

func BenchmarkSIMDI16x8BitmaskNonZeroCompileAMD64(b *testing.B) {
	m := mod1(b, nil, []wasm.ValType{wasm.I32}, repeatedI16x8BitmaskNonZeroBodyAMD64(64))
	for _, enabled := range []bool{false, true} {
		name := "unfused"
		if enabled {
			name = "fused"
		}
		b.Run(name, func(b *testing.B) {
			opts := CompileOptions{Workers: 1, Optimizations: map[string]bool{"simd-wide-bitmask-consumer": enabled}}
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, Stats: &stats, Optimizations: opts.Optimizations})
			if err != nil {
				b.Fatal(err)
			}
			if cm.CodeImage != nil {
				_ = cm.CodeImage.Close()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					b.Fatal(err)
				}
				if cm.CodeImage != nil {
					_ = cm.CodeImage.Close()
				}
			}
			b.ReportMetric(float64(stats.Funcs[0].CodeBytes), "code-B")
		})
	}
}

func BenchmarkSIMDI16x8BitmaskZeroBranchCompileAMD64(b *testing.B) {
	m := mod1(b, nil, []wasm.ValType{wasm.I32}, repeatedI16x8BitmaskZeroBranchBodyAMD64(64))
	for _, enabled := range []bool{false, true} {
		name := "unfused"
		if enabled {
			name = "fused"
		}
		b.Run(name, func(b *testing.B) {
			opts := CompileOptions{Workers: 1, Optimizations: map[string]bool{"simd-wide-bitmask-consumer": enabled}}
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, Stats: &stats, Optimizations: opts.Optimizations})
			if err != nil {
				b.Fatal(err)
			}
			if cm.CodeImage != nil {
				_ = cm.CodeImage.Close()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					b.Fatal(err)
				}
				if cm.CodeImage != nil {
					_ = cm.CodeImage.Close()
				}
			}
			b.ReportMetric(float64(stats.Funcs[0].CodeBytes), "code-B")
		})
	}
}
