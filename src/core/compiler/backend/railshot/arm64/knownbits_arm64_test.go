//go:build (linux || darwin) && arm64

package arm64

import (
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
