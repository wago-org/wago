//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"math/bits"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	wagoruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func knownBitsShiftBody() []byte {
	b := []byte{0x00, 0x20, 0x00, 0x41, 0x08, 0x76, 0x41} // x >>u 8; i32.const 0x00ffffff
	b = append(b, wasmtest.SLEB32(0x00ffffff)...)
	return append(b, 0x71, 0x0b) // and; end
}

func swarMaskEqzBody() []byte {
	b := []byte{0x00, 0x20, 0x00, 0x42}                            // local.get 0; i64.const lane-high-bit mask
	b = append(b, wasmtest.SLEB64(int64(-9187201950435737472))...) // 0x8080808080808080
	return append(b, 0x83, 0x50, 0x0b)                             // i64.and; i64.eqz; end
}

func swarMaskBranchBody() []byte {
	b := swarMaskEqzBody()
	b = b[:len(b)-1]
	return append(b, 0x04, 0x7f, 0x41, 0x01, 0x05, 0x41, 0x00, 0x0b, 0x0b)
}

func swarWiden4Body() []byte {
	b := []byte{0x01, 0x01, 0x7e, 0x20, 0x00, 0x42}
	b = append(b, wasmtest.SLEB64(0xffffffff)...)
	b = append(b, 0x83, 0x22, 0x01, 0x20, 0x01, 0x42, 0x10, 0x86, 0x84, 0x42)
	b = append(b, wasmtest.SLEB64(0x0000ffff0000ffff)...)
	b = append(b, 0x83, 0x22, 0x01, 0x20, 0x01, 0x42, 0x08, 0x86, 0x84, 0x42)
	b = append(b, wasmtest.SLEB64(0x00ff00ff00ff00ff)...)
	return append(b, 0x83, 0x0b)
}

func swarPack4Body() []byte {
	b := []byte{0x00}
	term := func(shift, mask int64) {
		b = append(b, 0x20, 0x00)
		if shift != 0 {
			b = append(b, 0x42)
			b = append(b, wasmtest.SLEB64(shift)...)
			b = append(b, 0x88)
		}
		b = append(b, 0x42)
		b = append(b, wasmtest.SLEB64(mask)...)
		b = append(b, 0x83)
	}
	term(24, 0xff000000)
	term(16, 0x00ff0000)
	term(0, 0x000000ff)
	term(8, 0x0000ff00)
	return append(b, 0x84, 0x84, 0x84, 0xa7, 0x0b)
}

func mulHighU64Body() []byte {
	b := []byte{0x01, 0x02, 0x7e, 0x20, 0x00, 0x42, 0x20, 0x88, 0x22, 0x02, 0x20, 0x01, 0x42}
	b = append(b, wasmtest.SLEB64(0xffffffff)...)
	b = append(b, 0x83, 0x22, 0x03, 0x7e, 0x20, 0x00, 0x42)
	b = append(b, wasmtest.SLEB64(0xffffffff)...)
	b = append(b, 0x83, 0x22, 0x00, 0x20, 0x03, 0x7e, 0x42, 0x20, 0x88, 0x7c, 0x21, 0x03,
		0x20, 0x01, 0x42, 0x20, 0x88, 0x22, 0x01, 0x20, 0x02, 0x7e,
		0x20, 0x03, 0x42, 0x20, 0x88, 0x7c, 0x20, 0x00, 0x20, 0x01, 0x7e, 0x20, 0x03, 0x42)
	b = append(b, wasmtest.SLEB64(0xffffffff)...)
	return append(b, 0x83, 0x7c, 0x42, 0x20, 0x88, 0x7c, 0x0b)
}

func TestKnownBitsMaskElision(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := mod1(t, i32, i32, knownBitsShiftBody())
	s := compileWithStats(t, m, false).Funcs[0]
	if got := s.Peephole["known-bits"]; got != 1 {
		t.Fatalf("known-bits = %d, want 1 (all: %v)", got, s.Peephole)
	}
	for _, x := range []uint32{0, 0xff, 0x12345678, 0xffffffff} {
		if got := uint32(runAmd64u(t, m, uint64(x))); got != x>>8 {
			t.Fatalf("x=%#x: got %#x, want %#x", x, got, x>>8)
		}
	}
}

func TestKnownBitsNarrowLoadMaskElision(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	body := []byte{0x00, 0x20, 0x00, 0x2d, 0x00, 0x00, 0x41, 0xff, 0x01, 0x71, 0x0b}
	m := modMem(t, 1, i32, i32, body) // load8_u(address) & 0xff
	if got := compileWithStats(t, m, false).Funcs[0].Peephole["known-bits"]; got != 1 {
		t.Fatalf("known-bits = %d, want 1 for load8_u mask", got)
	}
}

func TestSWARMaskTestFusion(t *testing.T) {
	i64, i32 := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}
	m := mod1(t, i64, i32, swarMaskEqzBody())
	s := compileWithStats(t, m, false).Funcs[0]
	if got := s.Peephole["swar-mask-test"]; got != 1 {
		t.Fatalf("swar-mask-test = %d, want 1 (all: %v)", got, s.Peephole)
	}
	var off *CodegenStats
	func() {
		saved := knownBitsEnabled
		defer func() { knownBitsEnabled = saved }()
		knownBitsEnabled = false
		off = compileWithStats(t, m, false).Funcs[0]
	}()
	if s.CodeBytes >= off.CodeBytes {
		t.Fatalf("fused code = %d bytes, unfused = %d; want smaller", s.CodeBytes, off.CodeBytes)
	}
	t.Logf("packed mask fusion: %d -> %d code bytes", off.CodeBytes, s.CodeBytes)
	for x, want := range map[uint64]uint32{0: 1, 0x7f7f7f7f7f7f7f7f: 1, 0x80: 0, 0x8000000000000000: 0} {
		if got := uint32(runAmd64u(t, m, x)); got != want {
			t.Fatalf("x=%#x: got %d, want %d", x, got, want)
		}
	}
}

func TestKnownBitsKillSwitchEquivalent(t *testing.T) {
	saved := knownBitsEnabled
	defer func() { knownBitsEnabled = saved }()
	i64, i32 := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}
	for _, x := range []uint64{0, 0x80, 0x8080, 0x7f7f7f7f7f7f7f7f} {
		knownBitsEnabled = true
		on := uint32(runAmd64u(t, mod1(t, i64, i32, swarMaskEqzBody()), x))
		knownBitsEnabled = false
		off := uint32(runAmd64u(t, mod1(t, i64, i32, swarMaskEqzBody()), x))
		if on != off {
			t.Fatalf("x=%#x: on=%d off=%d", x, on, off)
		}
	}
}

func TestSWARMaskBranchFusion(t *testing.T) {
	i64, i32 := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}
	m := mod1(t, i64, i32, swarMaskBranchBody())
	s := compileWithStats(t, m, false).Funcs[0]
	if s.Peephole["swar-mask-test"] != 1 || s.Peephole["cmp-branch-fuse"] != 1 || s.Peephole["compare-setcc"] != 0 {
		t.Fatalf("unexpected branch-fusion counters: %v", s.Peephole)
	}
	for x, want := range map[uint64]uint32{0: 1, 0x7f7f: 1, 0x80: 0} {
		if got := uint32(runAmd64u(t, m, x)); got != want {
			t.Fatalf("x=%#x: got %d, want %d", x, got, want)
		}
	}
}

func TestSWARWiden4Fusion(t *testing.T) {
	i64 := []wasm.ValType{wasm.I64}
	m := mod1(t, i64, i64, swarWiden4Body())
	on := compileWithStats(t, m, false).Funcs[0]
	if got := on.Peephole["swar-widen4"]; got != 1 {
		t.Fatalf("swar-widen4 = %d, want 1 (all: %v)", got, on.Peephole)
	}
	var off *CodegenStats
	func() {
		saved := swarIdiomsEnabled
		defer func() { swarIdiomsEnabled = saved }()
		swarIdiomsEnabled = false
		off = compileWithStats(t, m, false).Funcs[0]
	}()
	if on.CodeBytes >= off.CodeBytes {
		t.Fatalf("fused code = %d bytes, unfused = %d; want smaller", on.CodeBytes, off.CodeBytes)
	}
	for _, tc := range []struct{ in, want uint64 }{
		{0, 0},
		{0x44332211, 0x0044003300220011},
		{0xffffffffffffffff, 0x00ff00ff00ff00ff},
	} {
		if got := runAmd64u(t, m, tc.in); got != tc.want {
			t.Fatalf("widen(%#x) = %#x, want %#x", tc.in, got, tc.want)
		}
	}
}

func TestSWARWiden4PreservesLiveTemporary(t *testing.T) {
	i64 := []wasm.ValType{wasm.I64}
	body := swarWiden4Body()
	body = append(body[:len(body)-1], 0x20, 0x01, 0x1a, 0x0b) // local.get 1; drop; end
	m := mod1(t, i64, i64, body)
	if got := compileWithStats(t, m, false).Funcs[0].Peephole["swar-widen4"]; got != 0 {
		t.Fatalf("swar-widen4 = %d, want 0 while temporary remains live", got)
	}
}

func TestSWARPack4Fusion(t *testing.T) {
	i64, i32 := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}
	m := mod1(t, i64, i32, swarPack4Body())
	on := compileWithStats(t, m, false).Funcs[0]
	if got := on.Peephole["swar-pack4"]; got != 1 {
		t.Fatalf("swar-pack4 = %d, want 1 (all: %v)", got, on.Peephole)
	}
	var off *CodegenStats
	func() {
		saved := swarIdiomsEnabled
		defer func() { swarIdiomsEnabled = saved }()
		swarIdiomsEnabled = false
		off = compileWithStats(t, m, false).Funcs[0]
	}()
	if on.CodeBytes >= off.CodeBytes {
		t.Fatalf("fused code = %d bytes, unfused = %d; want smaller", on.CodeBytes, off.CodeBytes)
	}
	for _, tc := range []struct {
		in   uint64
		want uint32
	}{{0, 0}, {0x0044004300420041, 0x44434241}, {0xffffffffffffffff, 0xffffffff}, {0x123456789abcdef0, 0x3478bcf0}} {
		if got := uint32(runAmd64u(t, m, tc.in)); got != tc.want {
			t.Fatalf("pack(%#x) = %#x, want %#x", tc.in, got, tc.want)
		}
	}
}

func TestSWARPack4RejectsNearMatch(t *testing.T) {
	i64, i32 := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}
	body := swarPack4Body()
	// Replace the low-byte mask with 0xfe. This remains valid Wasm but is not
	// the byte-gather identity.
	for i := 0; i+2 < len(body); i++ {
		if body[i] == 0x42 && body[i+1] == 0xff && body[i+2] == 0x01 {
			body[i+1] = 0xfe
			break
		}
	}
	m := mod1(t, i64, i32, body)
	if got := compileWithStats(t, m, false).Funcs[0].Peephole["swar-pack4"]; got != 0 {
		t.Fatalf("swar-pack4 = %d, want 0 for near-match", got)
	}
}

func TestSWARPack4FusionWithoutWrap(t *testing.T) {
	i64 := []wasm.ValType{wasm.I64}
	body := swarPack4Body()
	body = append(body[:len(body)-2], 0x0b) // keep the packed value as i64
	m := mod1(t, i64, i64, body)
	if got := compileWithStats(t, m, false).Funcs[0].Peephole["swar-pack4"]; got != 1 {
		t.Fatalf("swar-pack4 = %d, want 1 without wrap", got)
	}
	for _, x := range []uint64{0, 0x0044004300420041, 0xffffffffffffffff, 0x123456789abcdef0} {
		want := (x & 0xff) | (x>>8)&0xff00 | (x>>16)&0xff0000 | (x>>24)&0xff000000
		if got := runAmd64u(t, m, x); got != want {
			t.Fatalf("pack64(%#x) = %#x, want %#x", x, got, want)
		}
	}
}

func TestMulHighU64Fusion(t *testing.T) {
	i64 := []wasm.ValType{wasm.I64}
	m := mod1(t, []wasm.ValType{wasm.I64, wasm.I64}, i64, mulHighU64Body())
	s := compileWithStats(t, m, false).Funcs[0]
	if got := s.Peephole["mul-high-u64"]; got != 1 {
		t.Fatalf("mul-high-u64 = %d, want 1 (all: %v)", got, s.Peephole)
	}
	var off *CodegenStats
	func() {
		saved := swarIdiomsEnabled
		defer func() { swarIdiomsEnabled = saved }()
		swarIdiomsEnabled = false
		off = compileWithStats(t, m, false).Funcs[0]
	}()
	if s.CodeBytes >= off.CodeBytes {
		t.Fatalf("native mul-high code = %d bytes, expansion = %d; want smaller", s.CodeBytes, off.CodeBytes)
	}
	for _, tc := range [][2]uint64{{0, 0}, {1, 1}, {0xffffffffffffffff, 2}, {0x9e3779b97f4a7c15, 0xd6e8feb86659fd93}} {
		want, _ := bits.Mul64(tc[0], tc[1])
		if got := runAmd64u(t, m, tc[0], tc[1]); got != want {
			t.Fatalf("mulhi(%#x,%#x) = %#x, want %#x", tc[0], tc[1], got, want)
		}
	}
}

func TestMulHighU64MatcherIsFunctionTailOnly(t *testing.T) {
	i64 := []wasm.ValType{wasm.I64}
	body := mulHighU64Body()
	body = append(body[:len(body)-1], 0x42, 0x00, 0x7c, 0x0b) // result + 0; end
	m := mod1(t, []wasm.ValType{wasm.I64, wasm.I64}, i64, body)
	if got := compileWithStats(t, m, false).Funcs[0].Peephole["mul-high-u64"]; got != 0 {
		t.Fatalf("mul-high-u64 = %d, want 0 away from function tail", got)
	}
}

var swarPackBenchmarkSink uint64

func BenchmarkSWARPack4Compile(b *testing.B) {
	m := mod1(b, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}, swarPack4Body())
	saved := swarIdiomsEnabled
	defer func() { swarIdiomsEnabled = saved }()
	for _, tc := range []struct {
		name string
		on   bool
	}{{"fused", true}, {"scalar", false}} {
		b.Run(tc.name, func(b *testing.B) {
			swarIdiomsEnabled = tc.on
			b.ReportAllocs()
			var codeBytes int
			for i := 0; i < b.N; i++ {
				cm, err := CompileModule(m)
				if err != nil {
					b.Fatal(err)
				}
				codeBytes = len(cm.Code)
			}
			b.ReportMetric(float64(codeBytes), "code-B")
		})
	}
}

func BenchmarkSWARPack4Exec(b *testing.B) {
	m := mod1(b, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}, swarPack4Body())
	saved := swarIdiomsEnabled
	defer func() { swarIdiomsEnabled = saved }()
	for _, tc := range []struct {
		name string
		on   bool
	}{{"fused", true}, {"scalar", false}} {
		b.Run(tc.name, func(b *testing.B) {
			swarIdiomsEnabled = tc.on
			cm, err := CompileModule(m)
			if err != nil {
				b.Fatal(err)
			}
			eng, err := wagoruntime.NewEngine()
			if err != nil {
				b.Fatal(err)
			}
			defer eng.Close()
			jm, err := wagoruntime.NewJobMemory(65536)
			if err != nil {
				b.Fatal(err)
			}
			defer jm.Close()
			mem, entry, err := wagoruntime.MapCode(cm.Code)
			if err != nil {
				b.Fatal(err)
			}
			defer wagoruntime.Unmap(mem)
			args, results, trap := make([]byte, 8), make([]byte, 8), make([]byte, 8)
			b.ReportAllocs()
			b.ReportMetric(float64(len(cm.Code)), "code-B")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				binary.LittleEndian.PutUint64(args, uint64(i)*0x9e3779b97f4a7c15)
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
				swarPackBenchmarkSink ^= binary.LittleEndian.Uint64(results)
			}
		})
	}
}
