//go:build (linux || darwin) && arm64

package arm64

import (
	"fmt"
	"syscall"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
	"github.com/wago-org/wago/tests/wasmtest"
)

func simdAndAnyTrueBodyArm64(a, b [16]byte) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(a)...)
	body = append(body, simdConst(b)...)
	body = append(body, simdOp(78)...)
	body = append(body, simdOp(83)...)
	return append(body, 0x0b)
}

func TestSIMDAndAnyTrueSuperoptArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	for _, tc := range []struct {
		name string
		a, b [16]byte
		want uint32
	}{
		{"zero", i8x16Bytes(1, 2, 4, 8), i8x16Bytes(16, 32, 64, -128), 0},
		{"low-bit", i8x16Bytes(3), i8x16Bytes(1), 1},
		{"high-lane", i8x16Bytes(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -128), i8x16Bytes(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -1), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := simdAndAnyTrueBodyArm64(tc.a, tc.b)
			m := mod1(t, nil, i32, body)
			on := compileWithStats(t, m, false).Funcs[0]
			if got := on.Peephole["simd-and-anytrue"]; got != 1 {
				t.Fatalf("simd-and-anytrue = %d, want 1 (all: %v)", got, on.Peephole)
			}
			if got := runArm64I32(t, body); got != tc.want {
				t.Fatalf("and-anytrue = %d, want %d", got, tc.want)
			}
			func() {
				saved := simdSuperoptEnabled
				defer func() { simdSuperoptEnabled = saved }()
				simdSuperoptEnabled = false
				if got := runArm64I32(t, body); got != tc.want {
					t.Fatalf("scalar and-anytrue = %d, want %d", got, tc.want)
				}
			}()
			t.Logf("and-anytrue code: %d bytes", on.CodeBytes)
		})
	}
}

func TestSIMDAndAnyTrueSuperoptRejectsNonAdjacentArm64(t *testing.T) {
	body := simdAndAnyTrueBodyArm64(i8x16Bytes(1), i8x16Bytes(1))
	andEnd := len(body) - len(simdOp(83)) - 1
	body = append(body[:andEnd], append(simdOp(77), body[andEnd:]...)...)
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
	if got := compileWithStats(t, m, false).Funcs[0].Peephole["simd-and-anytrue"]; got != 0 {
		t.Fatalf("simd-and-anytrue = %d, want 0 for non-adjacent sequence", got)
	}
	if got := runArm64I32(t, body); got != 1 {
		t.Fatalf("not(and) any_true = %d, want 1", got)
	}
}

func simdBitmaskNonZeroBodyArm64(v [16]byte, compare int32) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(v)...)
	body = append(body, simdOp(100)...)
	body = append(body, 0x41, byte(compare), 0x47, 0x0b) // i32.const compare; i32.ne; end
	return body
}

func TestSIMDBitmaskNonZeroSuperoptArm64(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    [16]byte
		want uint32
	}{
		{"none", i8x16Bytes(0, 1, 2, 127), 0},
		{"low-lane", i8x16Bytes(-1), 1},
		{"high-lane", i8x16Bytes(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -128), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := simdBitmaskNonZeroBodyArm64(tc.v, 0)
			m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
			on := compileWithStats(t, m, false).Funcs[0]
			if got := on.Peephole["simd-bitmask-nonzero"]; got != 1 {
				t.Fatalf("simd-bitmask-nonzero = %d, want 1 (all: %v)", got, on.Peephole)
			}
			if got := runArm64I32(t, body); got != tc.want {
				t.Fatalf("bitmask != 0 = %d, want %d", got, tc.want)
			}

			var off *CodegenStats
			func() {
				saved := simdSuperoptEnabled
				defer func() { simdSuperoptEnabled = saved }()
				simdSuperoptEnabled = false
				off = compileWithStats(t, m, false).Funcs[0]
				if got := runArm64I32(t, body); got != tc.want {
					t.Fatalf("unfused bitmask != 0 = %d, want %d", got, tc.want)
				}
			}()
			if on.CodeBytes >= off.CodeBytes {
				t.Fatalf("fused code = %d bytes, unfused = %d; want smaller", on.CodeBytes, off.CodeBytes)
			}
		})
	}
}

func TestSIMDBitmaskNonZeroSuperoptRejectsOtherConstantArm64(t *testing.T) {
	body := simdBitmaskNonZeroBodyArm64(i8x16Bytes(-1), 1)
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
	if got := compileWithStats(t, m, false).Funcs[0].Peephole["simd-bitmask-nonzero"]; got != 0 {
		t.Fatalf("simd-bitmask-nonzero = %d, want 0 for comparison with one", got)
	}
	if got := runArm64I32(t, body); got != 0 { // bitmask is exactly one.
		t.Fatalf("bitmask != 1 = %d, want 0", got)
	}
}

func simdBitmaskPopcntBodyArm64(v [16]byte) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(v)...)
	body = append(body, simdOp(100)...)
	return append(body, 0x69, 0x0b) // i32.popcnt; end
}

func TestSIMDBitmaskPopcntSuperoptArm64(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    [16]byte
		want uint32
	}{
		{"none", i8x16Bytes(0, 1, 2, 127), 0},
		{"ends", i8x16Bytes(-1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -128), 2},
		{"all", i8x16Bytes(-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1), 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := simdBitmaskPopcntBodyArm64(tc.v)
			m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
			on := compileWithStats(t, m, false).Funcs[0]
			if got := on.Peephole["simd-bitmask-popcnt"]; got != 1 {
				t.Fatalf("simd-bitmask-popcnt = %d, want 1 (all: %v)", got, on.Peephole)
			}
			if got := runArm64I32(t, body); got != tc.want {
				t.Fatalf("popcnt(bitmask) = %d, want %d", got, tc.want)
			}

			var off *CodegenStats
			func() {
				saved := simdSuperoptEnabled
				defer func() { simdSuperoptEnabled = saved }()
				simdSuperoptEnabled = false
				off = compileWithStats(t, m, false).Funcs[0]
				if got := runArm64I32(t, body); got != tc.want {
					t.Fatalf("unfused popcnt(bitmask) = %d, want %d", got, tc.want)
				}
			}()
			if on.CodeBytes >= off.CodeBytes {
				t.Fatalf("fused code = %d bytes, unfused = %d; want smaller", on.CodeBytes, off.CodeBytes)
			}
		})
	}
}

func simdWideBitmaskConsumerBodyArm64(v [16]byte, bitmaskOp uint32, popcnt bool, compare int32) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(v)...)
	body = append(body, simdOp(bitmaskOp)...)
	if popcnt {
		return append(body, 0x69, 0x0b) // i32.popcnt; end
	}
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(compare)...)
	return append(body, 0x47, 0x0b) // i32.ne; end
}

func TestSIMDWideBitmaskConsumersArm64(t *testing.T) {
	cases := []struct {
		name      string
		v         [16]byte
		bitmaskOp uint32
		count     uint64
		nonzero   bool
	}{
		{"i16x8", i16x8Bytes(-1, 0, -32768, 7, 8, -2, 10, 11), 132, 3, true},
		{"i32x4", i32x4Bytes(-1, 0, -2147483648, 7), 164, 2, true},
		{"i64x2", i64x2Bytes(-1, 0), 196, 1, false},
	}
	for _, tc := range cases {
		for _, popcnt := range []bool{false, true} {
			if !popcnt && !tc.nonzero {
				continue
			}
			consumer := "nonzero"
			want := uint64(1)
			peep := "simd-bitmask-nonzero"
			if popcnt {
				consumer = "popcnt"
				want = tc.count
				peep = "simd-bitmask-popcnt"
			}
			t.Run(tc.name+"/"+consumer, func(t *testing.T) {
				body := simdWideBitmaskConsumerBodyArm64(tc.v, tc.bitmaskOp, popcnt, 0)
				m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
				compile := func(enabled bool) *CodegenStats {
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{
						Stats:         &stats,
						Optimizations: map[string]bool{"simd-wide-bitmask-consumer": enabled},
					})
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						t.Cleanup(func() { cm.CodeImage.Close() })
					}
					return stats.Funcs[0]
				}
				on, off := compile(true), compile(false)
				if got := on.Peephole[peep]; got != 1 {
					t.Fatalf("%s hits = %d, want 1 (all: %v)", peep, got, on.Peephole)
				}
				if got := off.Peephole[peep]; got != 0 {
					t.Fatalf("disabled %s hits = %d, want 0", peep, got)
				}
				if on.CodeBytes > off.CodeBytes || (on.CodeBytes == off.CodeBytes && tc.name != "i64x2") {
					t.Fatalf("fused/unfused code = %d/%d bytes, want no growth and a reduction before i64 alignment", on.CodeBytes, off.CodeBytes)
				}
				t.Logf("fused/unfused code = %d/%d bytes", on.CodeBytes, off.CodeBytes)
				got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"simd-wide-bitmask-consumer": true}})
				if err != nil || got != want {
					t.Fatalf("result = %d, %v; want %d", got, err, want)
				}
			})
		}
	}
}

func TestSIMDWideBitmaskNonZeroRejectsOtherConstantArm64(t *testing.T) {
	body := simdWideBitmaskConsumerBodyArm64(i16x8Bytes(-1), 132, false, 1)
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats}); err != nil {
		t.Fatal(err)
	}
	if got := stats.Funcs[0].Peephole["simd-bitmask-nonzero"]; got != 0 {
		t.Fatalf("simd-bitmask-nonzero hits = %d, want 0", got)
	}
	if got := runArm64I32(t, body); got != 0 {
		t.Fatalf("bitmask != 1 = %d, want 0", got)
	}
}

func simdBitmaskZeroBranchBodyArm64(v [16]byte, bitmaskOp uint32, brIf bool) []byte {
	body := []byte{0x00}
	if brIf {
		body = append(body, 0x02, 0x7f, 0x41, 0x01) // block (result i32); branch value 1
	}
	body = append(body, simdConst(v)...)
	body = append(body, simdOp(bitmaskOp)...)
	body = append(body, 0x45) // i32.eqz
	if brIf {
		return append(body, 0x0d, 0x00, 0x1a, 0x41, 0x00, 0x0b, 0x0b) // br_if 0; drop; 0; end; end
	}
	return append(body, 0x04, 0x7f, 0x41, 0x01, 0x05, 0x41, 0x00, 0x0b, 0x0b) // if 1 else 0; end
}

func TestSIMDBitmaskZeroBranchFusionArm64(t *testing.T) {
	for _, tc := range []struct {
		name      string
		v         [16]byte
		bitmaskOp uint32
		want      uint64
	}{
		{"i8x16-zero", i8x16Bytes(0, 1, 2, 127), 100, 1},
		{"i8x16-nonzero", i8x16Bytes(-1), 100, 0},
		{"i16x8-zero", i16x8Bytes(0, 1, 2, 32767), 132, 1},
		{"i16x8-nonzero", i16x8Bytes(-1), 132, 0},
		{"i32x4-zero", i32x4Bytes(0, 1, 2, 2147483647), 164, 1},
		{"i32x4-nonzero", i32x4Bytes(-1), 164, 0},
	} {
		for _, brIf := range []bool{false, true} {
			branch := "if"
			if brIf {
				branch = "br_if"
			}
			t.Run(tc.name+"/"+branch, func(t *testing.T) {
				body := simdBitmaskZeroBranchBodyArm64(tc.v, tc.bitmaskOp, brIf)
				m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
				var onStats, offStats ModuleStats
				on, err := CompileModuleWith(m, CompileOptions{Stats: &onStats, Optimizations: map[string]bool{"simd-wide-bitmask-consumer": true}})
				if err != nil {
					t.Fatal(err)
				}
				off, err := CompileModuleWith(m, CompileOptions{Stats: &offStats, Optimizations: map[string]bool{"simd-wide-bitmask-consumer": false}})
				if err != nil {
					t.Fatal(err)
				}
				if got := onStats.Funcs[0].Peephole["simd-bitmask-zero-branch"]; got != 1 {
					t.Fatalf("fused hits = %d, want 1 (all: %v)", got, onStats.Funcs[0].Peephole)
				}
				if got := offStats.Funcs[0].Peephole["simd-bitmask-zero-branch"]; got != 0 {
					t.Fatalf("disabled hits = %d, want 0", got)
				}
				if len(on.Code) >= len(off.Code) {
					t.Fatalf("fused/unfused code = %d/%d bytes, want reduction", len(on.Code), len(off.Code))
				}
				got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"simd-wide-bitmask-consumer": true}})
				if err != nil || got != tc.want {
					t.Fatalf("result = %d, %v; want %d", got, err, tc.want)
				}
			})
		}
	}
}

func TestSIMDBitmaskZeroBranchFusionNearMissesArm64(t *testing.T) {
	body := []byte{0x00}
	body = append(body, simdConst(i16x8Bytes(-1))...)
	body = append(body, simdOp(132)...)
	body = append(body, 0x45, 0x41, 0x01, 0x6a, 0x0b) // eqz; i32.const 1; add; end
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats}); err != nil {
		t.Fatal(err)
	}
	if got := stats.Funcs[0].Peephole["simd-bitmask-zero-branch-candidate"]; got != 0 {
		t.Fatalf("non-branch candidate hits = %d, want 0", got)
	}
}

func benchmarkSIMDWideBitmaskBodyARM64(v [16]byte, bitmaskOp uint32, popcnt bool) []byte {
	body := []byte{0x00}
	for i := 0; i < 64; i++ {
		body = append(body, simdConst(v)...)
		body = append(body, simdOp(bitmaskOp)...)
		if popcnt {
			body = append(body, 0x69) // i32.popcnt
		} else {
			body = append(body, 0x41, 0x00, 0x47) // i32.const 0; i32.ne
		}
		if i != 0 {
			body = append(body, 0x6a) // i32.add
		}
	}
	return append(body, 0x0b)
}

func BenchmarkSIMDWideBitmaskConsumersARM64(b *testing.B) {
	for _, tc := range []struct {
		name      string
		v         [16]byte
		bitmaskOp uint32
		nonzero   bool
	}{
		{"i16x8", i16x8Bytes(-1, 0, -32768, 7, 8, -2, 10, 11), 132, true},
		{"i32x4", i32x4Bytes(-1, 0, -2147483648, 7), 164, true},
		{"i64x2", i64x2Bytes(-1, 0), 196, false},
	} {
		for _, popcnt := range []bool{false, true} {
			if !popcnt && !tc.nonzero {
				continue
			}
			consumer := "nonzero"
			if popcnt {
				consumer = "popcnt"
			}
			body := benchmarkSIMDWideBitmaskBodyARM64(tc.v, tc.bitmaskOp, popcnt)
			m := mod1(b, nil, []wasm.ValType{wasm.I32}, body)
			for _, enabled := range []bool{false, true} {
				selection := "unfused"
				if enabled {
					selection = "fused"
				}
				b.Run(fmt.Sprintf("%s/%s/%s", tc.name, consumer, selection), func(b *testing.B) {
					cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{
						"simd-wide-bitmask-consumer": enabled,
						"stack-fence":                false,
					}})
					if err != nil {
						b.Fatal(err)
					}
					code, err := arm64spike.MapExec(cm.Code)
					if err != nil {
						b.Fatal(err)
					}
					b.Cleanup(func() { _ = syscall.Munmap(code) })
					entry := uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]]))
					b.ReportAllocs()
					b.ReportMetric(float64(len(cm.Code)), "code-B")
					b.ResetTimer()
					for range b.N {
						arm64spike.Call2(entry, 0, 0)
					}
				})
			}
		}
	}
}

func BenchmarkSIMDWideBitmaskConsumersCompileARM64(b *testing.B) {
	body := benchmarkSIMDWideBitmaskBodyARM64(i16x8Bytes(-1, 0, -32768, 7, 8, -2, 10, 11), 132, false)
	m := mod1(b, nil, []wasm.ValType{wasm.I32}, body)
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

func benchmarkSIMDBitmaskZeroBranchBodyARM64(v [16]byte, bitmaskOp uint32) []byte {
	body := []byte{0x01, 0x01, 0x7f} // one i32 local accumulator
	for range 64 {
		body = append(body, simdConst(v)...)
		body = append(body, simdOp(bitmaskOp)...)
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

func BenchmarkSIMDBitmaskZeroBranchARM64(b *testing.B) {
	body := benchmarkSIMDBitmaskZeroBranchBodyARM64(i16x8Bytes(0, 1, 2, 3, 4, 5, 6, 7), 132)
	m := mod1(b, nil, []wasm.ValType{wasm.I32}, body)
	for _, enabled := range []bool{false, true} {
		name := "unfused"
		if enabled {
			name = "fused"
		}
		b.Run(name, func(b *testing.B) {
			cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{
				"simd-wide-bitmask-consumer": enabled,
				"stack-fence":                false,
			}})
			if err != nil {
				b.Fatal(err)
			}
			code, err := arm64spike.MapExec(cm.Code)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = syscall.Munmap(code) })
			entry := uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]]))
			b.ReportAllocs()
			b.ReportMetric(float64(len(cm.Code)), "code-B")
			b.ResetTimer()
			for range b.N {
				arm64spike.Call2(entry, 0, 0)
			}
		})
	}
}

func BenchmarkSIMDBitmaskZeroBranchCompileARM64(b *testing.B) {
	body := benchmarkSIMDBitmaskZeroBranchBodyARM64(i16x8Bytes(0, 1, 2, 3, 4, 5, 6, 7), 132)
	m := mod1(b, nil, []wasm.ValType{wasm.I32}, body)
	for _, enabled := range []bool{false, true} {
		name := "unfused"
		if enabled {
			name = "fused"
		}
		b.Run(name, func(b *testing.B) {
			var stats ModuleStats
			opts := CompileOptions{Workers: 1, Optimizations: map[string]bool{"simd-wide-bitmask-consumer": enabled}}
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

func simdExtShuffleBodyArm64(a, b [16]byte, offset byte) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(a)...)
	body = append(body, simdConst(b)...)
	body = append(body, simdOp(13)...)
	for lane := byte(0); lane < 16; lane++ {
		body = append(body, offset+lane)
	}
	return append(body, 0x0b)
}

func TestSIMDShuffleExtSelectorArm64(t *testing.T) {
	a := i8x16Bytes(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)
	b := i8x16Bytes(16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31)
	for offset := byte(1); offset < 16; offset++ {
		body := simdExtShuffleBodyArm64(a, b, offset)
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
		if got := compileWithStats(t, m, false).Funcs[0].Peephole["simd-shuffle-ext"]; got != 1 {
			t.Fatalf("offset %d: simd-shuffle-ext = %d, want 1", offset, got)
		}
		var want [16]byte
		for i := range want {
			want[i] = offset + byte(i)
		}
		if got := runArm64V128(t, m); got != want {
			t.Fatalf("offset %d: ext = %v, want %v", offset, got, want)
		}
	}
}

func TestSIMDShuffleExtSelectorRejectsNonContiguousArm64(t *testing.T) {
	body := simdExtShuffleBodyArm64(i8x16Bytes(), i8x16Bytes(), 7)
	body[len(body)-2] = 0 // Break the final lane of the shuffle immediate.
	m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
	if got := compileWithStats(t, m, false).Funcs[0].Peephole["simd-shuffle-ext"]; got != 0 {
		t.Fatalf("simd-shuffle-ext = %d, want 0 for non-contiguous shuffle", got)
	}
}

func simdShuffleBodyArm64(a, b [16]byte, lanes [16]byte) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(a)...)
	body = append(body, simdConst(b)...)
	body = append(body, simdOp(13)...)
	body = append(body, lanes[:]...)
	return append(body, 0x0b)
}

func TestSIMDShuffleHalfZipSelectorArm64(t *testing.T) {
	a := i8x16Bytes(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)
	b := i8x16Bytes(16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31)
	for _, lanes := range [][16]byte{i8x16Zip1H, i8x16Zip2H} {
		m := mod1(t, nil, []wasm.ValType{wasm.V128}, simdShuffleBodyArm64(a, b, lanes))
		compile := func(on bool) *CodegenStats {
			var stats ModuleStats
			if _, err := CompileModuleWith(m, CompileOptions{
				Stats:         &stats,
				Optimizations: map[string]bool{"shuffle-half-zip": on},
			}); err != nil {
				t.Fatal(err)
			}
			return stats.Funcs[0]
		}
		on, off := compile(true), compile(false)
		if got := on.Peephole["simd-shuffle-half-zip"]; got != 1 {
			t.Fatalf("simd-shuffle-half-zip = %d, want 1 (all: %v)", got, on.Peephole)
		}
		if got := off.Peephole["simd-shuffle-half-zip"]; got != 0 {
			t.Fatalf("disabled simd-shuffle-half-zip = %d, want 0", got)
		}
		if on.CodeBytes >= off.CodeBytes {
			t.Fatalf("halfword ZIP code = %d bytes, fallback = %d; want smaller", on.CodeBytes, off.CodeBytes)
		}
	}
}

func TestSIMDShuffleHalfZipSelectorRejectsNearMissArm64(t *testing.T) {
	lanes := i8x16Zip1H
	lanes[15]--
	m := mod1(t, nil, []wasm.ValType{wasm.V128}, simdShuffleBodyArm64(i8x16Bytes(), i8x16Bytes(), lanes))
	if got := compileWithStats(t, m, false).Funcs[0].Peephole["simd-shuffle-half-zip"]; got != 0 {
		t.Fatalf("simd-shuffle-half-zip = %d, want 0 for near miss", got)
	}
}

func TestSIMDShuffleExtSelectorSinksToPinnedLocalArm64(t *testing.T) {
	a := i8x16Bytes(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)
	b := i8x16Bytes(16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31)
	want := i8x16Bytes(13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28)
	for _, tc := range []struct {
		name   string
		target byte
	}{
		{name: "destination-aliases-first-source", target: 0},
		{name: "destination-aliases-second-source", target: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0x01, 0x02, 0x7b} // two v128 locals
			body = append(body, simdConst(a)...)
			body = append(body, 0x21, 0x00) // local.set 0
			body = append(body, simdConst(b)...)
			body = append(body, 0x21, 0x01) // local.set 1
			body = append(body, 0x20, 0x00, 0x20, 0x01)
			body = append(body, simdOp(13)...)
			for lane := byte(0); lane < 16; lane++ {
				body = append(body, 13+lane)
			}
			body = append(body, 0x21, tc.target, 0x20, tc.target, 0x0b)
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
			s := compileWithStats(t, m, false).Funcs[0]
			if got := s.Peephole["simd-shuffle-ext"]; got != 1 {
				t.Fatalf("simd-shuffle-ext = %d, want 1 (all: %v)", got, s.Peephole)
			}
			if got := s.Peephole["v128-shuffle-sink"]; got != 1 {
				t.Fatalf("v128-shuffle-sink = %d, want 1 (all: %v)", got, s.Peephole)
			}
			if got := runArm64V128(t, m); got != want {
				t.Fatalf("sunk EXT = %v, want %v", got, want)
			}
		})
	}
}

func simdNotAndBodyArm64(a, b [16]byte) []byte {
	body := []byte{0x00}
	body = append(body, simdConst(a)...)
	body = append(body, simdConst(b)...)
	body = append(body, simdOp(77)...)
	body = append(body, simdOp(78)...)
	return append(body, 0x0b)
}

func TestSIMDNotAndSuperoptArm64(t *testing.T) {
	a := i8x16Bytes(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)
	b := i8x16Bytes(-1, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14)
	body := simdNotAndBodyArm64(a, b)
	m := mod1(t, nil, []wasm.ValType{wasm.V128}, body)
	on := compileWithStats(t, m, false).Funcs[0]
	if got := on.Peephole["simd-not-and"]; got != 1 {
		t.Fatalf("simd-not-and = %d, want 1 (all: %v)", got, on.Peephole)
	}
	var off *CodegenStats
	func() {
		saved := simdSuperoptEnabled
		defer func() { simdSuperoptEnabled = saved }()
		simdSuperoptEnabled = false
		off = compileWithStats(t, m, false).Funcs[0]
	}()
	if on.CodeBytes >= off.CodeBytes {
		t.Fatalf("fused code = %d bytes, scalar = %d; want smaller", on.CodeBytes, off.CodeBytes)
	}
	t.Logf("not-and code: %d -> %d bytes", off.CodeBytes, on.CodeBytes)
	var want [16]byte
	for i := range want {
		want[i] = a[i] &^ b[i]
	}
	if got := runArm64V128(t, m); got != want {
		t.Fatalf("not-and = % x, want % x", got, want)
	}
}
