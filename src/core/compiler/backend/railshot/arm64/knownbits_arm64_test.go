//go:build (linux || darwin) && arm64

package arm64

import (
	"math/bits"
	"runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
	"github.com/wago-org/wago/testutil/wasmtest"
)

var knownBitsBenchmarkSink uintptr

func knownBitsShiftBodyArm64() []byte {
	b := []byte{0x00, 0x20, 0x00, 0x41, 0x08, 0x76, 0x41}
	b = append(b, wasmtest.SLEB32(0x00ffffff)...)
	return append(b, 0x71, 0x0b)
}

func swarMaskEqzBodyArm64() []byte {
	b := []byte{0x00, 0x20, 0x00, 0x42}
	b = append(b, wasmtest.SLEB64(int64(-9187201950435737472))...)
	return append(b, 0x83, 0x50, 0x0b)
}

func swarMaskBranchBodyArm64() []byte {
	b := swarMaskEqzBodyArm64()
	b = b[:len(b)-1]
	return append(b, 0x04, 0x7f, 0x41, 0x01, 0x05, 0x41, 0x00, 0x0b, 0x0b)
}

func swarWiden4BodyArm64() []byte {
	b := []byte{0x01, 0x01, 0x7e, 0x20, 0x00, 0x42}
	b = append(b, wasmtest.SLEB64(0xffffffff)...)
	b = append(b, 0x83, 0x22, 0x01, 0x20, 0x01, 0x42, 0x10, 0x86, 0x84, 0x42)
	b = append(b, wasmtest.SLEB64(0x0000ffff0000ffff)...)
	b = append(b, 0x83, 0x22, 0x01, 0x20, 0x01, 0x42, 0x08, 0x86, 0x84, 0x42)
	b = append(b, wasmtest.SLEB64(0x00ff00ff00ff00ff)...)
	return append(b, 0x83, 0x0b)
}

func swarPack4BodyArm64() []byte {
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

func mulHighU64BodyArm64() []byte {
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

func TestKnownBitsMaskElisionArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := mod1(t, i32, i32, knownBitsShiftBodyArm64())
	s := compileWithStats(t, m, false).Funcs[0]
	if got := s.Peephole["known-bits"]; got != 1 {
		t.Fatalf("known-bits = %d, want 1 (all: %v)", got, s.Peephole)
	}
	for _, x := range []uint32{0, 0xff, 0x12345678, 0xffffffff} {
		got := uint32(runArm64Internal2(t, m, uintptr(x), 0))
		if got != x>>8 {
			t.Fatalf("x=%#x: got %#x, want %#x", x, got, x>>8)
		}
	}
}

func TestKnownBitsNarrowLoadMaskElisionArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	body := []byte{0x00, 0x20, 0x00, 0x2d, 0x00, 0x00, 0x41, 0xff, 0x01, 0x71, 0x0b}
	m := modMem(t, 1, i32, i32, body)
	if got := compileWithStats(t, m, false).Funcs[0].Peephole["known-bits"]; got != 1 {
		t.Fatalf("known-bits = %d, want 1 for load8_u mask", got)
	}
}

func TestSWARMaskTestFusionArm64(t *testing.T) {
	i64, i32 := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}
	m := mod1(t, i64, i32, swarMaskEqzBodyArm64())
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
		got := uint32(runArm64Internal2(t, m, uintptr(x), 0))
		if got != want {
			t.Fatalf("x=%#x: got %d, want %d", x, got, want)
		}
	}
}

func TestKnownBitsKillSwitchEquivalentArm64(t *testing.T) {
	saved := knownBitsEnabled
	defer func() { knownBitsEnabled = saved }()
	i64, i32 := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}
	for _, x := range []uint64{0, 0x80, 0x8080, 0x7f7f7f7f7f7f7f7f} {
		knownBitsEnabled = true
		on := uint32(runArm64Internal2(t, mod1(t, i64, i32, swarMaskEqzBodyArm64()), uintptr(x), 0))
		knownBitsEnabled = false
		off := uint32(runArm64Internal2(t, mod1(t, i64, i32, swarMaskEqzBodyArm64()), uintptr(x), 0))
		if on != off {
			t.Fatalf("x=%#x: on=%d off=%d", x, on, off)
		}
	}
}

func TestSWARMaskBranchFusionArm64(t *testing.T) {
	i64, i32 := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}
	m := mod1(t, i64, i32, swarMaskBranchBodyArm64())
	s := compileWithStats(t, m, false).Funcs[0]
	if s.Peephole["swar-mask-test"] != 1 || s.Peephole["cmp-branch-fuse"] != 1 || s.Peephole["compare-setcc"] != 0 {
		t.Fatalf("unexpected branch-fusion counters: %v", s.Peephole)
	}
	for x, want := range map[uint64]uint32{0: 1, 0x7f7f: 1, 0x80: 0} {
		got := uint32(runArm64Internal2(t, m, uintptr(x), 0))
		if got != want {
			t.Fatalf("x=%#x: got %d, want %d", x, got, want)
		}
	}
}

func TestSWARWiden4FusionArm64(t *testing.T) {
	i64 := []wasm.ValType{wasm.I64}
	m := mod1(t, i64, i64, swarWiden4BodyArm64())
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
		if got := uint64(runArm64Internal2(t, m, uintptr(tc.in), 0)); got != tc.want {
			t.Fatalf("widen(%#x) = %#x, want %#x", tc.in, got, tc.want)
		}
	}
}

func TestSWARWiden4PreservesLiveTemporaryArm64(t *testing.T) {
	i64 := []wasm.ValType{wasm.I64}
	body := swarWiden4BodyArm64()
	body = append(body[:len(body)-1], 0x20, 0x01, 0x1a, 0x0b) // local.get 1; drop; end
	m := mod1(t, i64, i64, body)
	if got := compileWithStats(t, m, false).Funcs[0].Peephole["swar-widen4"]; got != 0 {
		t.Fatalf("swar-widen4 = %d, want 0 while temporary remains live", got)
	}
}

func TestSWARPack4FusionArm64(t *testing.T) {
	i64, i32 := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}
	m := mod1(t, i64, i32, swarPack4BodyArm64())
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
		if got := uint32(runArm64Internal2(t, m, uintptr(tc.in), 0)); got != tc.want {
			t.Fatalf("pack(%#x) = %#x, want %#x", tc.in, got, tc.want)
		}
	}
}

func TestSWARPack4RejectsNearMatchArm64(t *testing.T) {
	i64, i32 := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}
	body := swarPack4BodyArm64()
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

func TestSWARPack4FusionWithoutWrapArm64(t *testing.T) {
	i64 := []wasm.ValType{wasm.I64}
	body := swarPack4BodyArm64()
	body = append(body[:len(body)-2], 0x0b)
	m := mod1(t, i64, i64, body)
	if got := compileWithStats(t, m, false).Funcs[0].Peephole["swar-pack4"]; got != 1 {
		t.Fatalf("swar-pack4 = %d, want 1 without wrap", got)
	}
	for _, x := range []uint64{0, 0x0044004300420041, 0xffffffffffffffff, 0x123456789abcdef0} {
		want := (x & 0xff) | (x>>8)&0xff00 | (x>>16)&0xff0000 | (x>>24)&0xff000000
		if got := uint64(runArm64Internal2(t, m, uintptr(x), 0)); got != want {
			t.Fatalf("pack64(%#x) = %#x, want %#x", x, got, want)
		}
	}
}

func TestMulHighU64FusionArm64(t *testing.T) {
	i64 := []wasm.ValType{wasm.I64}
	m := mod1(t, []wasm.ValType{wasm.I64, wasm.I64}, i64, mulHighU64BodyArm64())
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
		if got := uint64(runArm64Internal2(t, m, uintptr(tc[0]), uintptr(tc[1]))); got != want {
			t.Fatalf("mulhi(%#x,%#x) = %#x, want %#x", tc[0], tc[1], got, want)
		}
	}
}

func TestMulHighU64MatcherIsFunctionTailOnlyArm64(t *testing.T) {
	i64 := []wasm.ValType{wasm.I64}
	body := mulHighU64BodyArm64()
	body = append(body[:len(body)-1], 0x42, 0x00, 0x7c, 0x0b) // result + 0; end
	m := mod1(t, []wasm.ValType{wasm.I64, wasm.I64}, i64, body)
	if got := compileWithStats(t, m, false).Funcs[0].Peephole["mul-high-u64"]; got != 0 {
		t.Fatalf("mul-high-u64 = %d, want 0 away from function tail", got)
	}
}

func BenchmarkKnownBitsCompileArm64(b *testing.B) {
	i64, i32 := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}
	m := mod1(b, i64, i32, swarMaskEqzBodyArm64())
	b.ReportAllocs()
	b.ResetTimer()
	var cmLen uintptr
	for i := 0; i < b.N; i++ {
		cm, err := CompileModule(m)
		if err != nil {
			b.Fatal(err)
		}
		cmLen ^= uintptr(len(cm.Code))
	}
	knownBitsBenchmarkSink = cmLen
}

func BenchmarkSWARMaskExecArm64(b *testing.B) {
	i64, i32 := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}
	cm, err := CompileModule(mod1(b, i64, i32, swarMaskEqzBodyArm64()))
	if err != nil {
		b.Fatal(err)
	}
	code, err := arm64spike.MapExec(cm.Code)
	if err != nil {
		b.Fatal(err)
	}
	entry := uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]]))
	b.ReportAllocs()
	b.ResetTimer()
	var result uintptr
	for i := 0; i < b.N; i++ {
		result ^= arm64spike.Call2(entry, uintptr(i), 0)
	}
	knownBitsBenchmarkSink = result
	b.ReportMetric(float64(len(cm.Code)), "code-B")
	runtime.KeepAlive(code)
}

func BenchmarkSWARWiden4ExecArm64(b *testing.B) {
	i64 := []wasm.ValType{wasm.I64}
	cm, err := CompileModule(mod1(b, i64, i64, swarWiden4BodyArm64()))
	if err != nil {
		b.Fatal(err)
	}
	code, err := arm64spike.MapExec(cm.Code)
	if err != nil {
		b.Fatal(err)
	}
	entry := uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]]))
	b.ReportAllocs()
	b.ResetTimer()
	var result uintptr
	for i := 0; i < b.N; i++ {
		result ^= arm64spike.Call2(entry, uintptr(i), 0)
	}
	knownBitsBenchmarkSink = result
	b.ReportMetric(float64(len(cm.Code)), "code-B")
	runtime.KeepAlive(code)
}

func BenchmarkMulHighU64ExecArm64(b *testing.B) {
	i64 := []wasm.ValType{wasm.I64}
	cm, err := CompileModule(mod1(b, []wasm.ValType{wasm.I64, wasm.I64}, i64, mulHighU64BodyArm64()))
	if err != nil {
		b.Fatal(err)
	}
	code, err := arm64spike.MapExec(cm.Code)
	if err != nil {
		b.Fatal(err)
	}
	entry := uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]]))
	b.ReportAllocs()
	b.ResetTimer()
	var result uintptr
	for i := 0; i < b.N; i++ {
		result ^= arm64spike.Call2(entry, uintptr(i)*0x9e3779b9, uintptr(i)^0xd6e8feb8)
	}
	knownBitsBenchmarkSink = result
	b.ReportMetric(float64(len(cm.Code)), "code-B")
	runtime.KeepAlive(code)
}

func BenchmarkSWARPack4CompileArm64(b *testing.B) {
	m := mod1(b, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}, swarPack4BodyArm64())
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

func BenchmarkSWARPack4ExecArm64(b *testing.B) {
	m := mod1(b, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}, swarPack4BodyArm64())
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
			code, err := arm64spike.MapExec(cm.Code)
			if err != nil {
				b.Fatal(err)
			}
			entry := uintptr(unsafe.Pointer(&code[cm.InternalEntry[0]]))
			b.ReportAllocs()
			b.ReportMetric(float64(len(cm.Code)), "code-B")
			b.ResetTimer()
			var result uintptr
			for i := 0; i < b.N; i++ {
				result ^= arm64spike.Call2(entry, uintptr(i)*0x9e3779b97f4a7c15, 0)
			}
			knownBitsBenchmarkSink = result
			runtime.KeepAlive(code)
		})
	}
}
