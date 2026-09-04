//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

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

func singleBitMaskBranchBodyArm64() []byte {
	return []byte{0x00, 0x20, 0x00, 0x42, 0x08, 0x83, 0x50, 0x04, 0x7f, 0x41, 0x01, 0x05, 0x41, 0x00, 0x0b, 0x0b}
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
		saved := swarMaskTestEnabled
		defer func() { swarMaskTestEnabled = saved }()
		swarMaskTestEnabled = false
		off = compileWithStats(t, m, false).Funcs[0]
	}()
	if s.CodeBytes >= off.CodeBytes {
		t.Fatalf("fused code = %d bytes, unfused = %d; want smaller", s.CodeBytes, off.CodeBytes)
	}
	for x, want := range map[uint64]uint32{0: 1, 0x7f7f7f7f7f7f7f7f: 1, 0x80: 0, 0x8000000000000000: 0} {
		got := uint32(runArm64Internal2(t, m, uintptr(x), 0))
		if got != want {
			t.Fatalf("x=%#x: got %d, want %d", x, got, want)
		}
	}
}

func TestSWARMaskTestKillSwitchEquivalentArm64(t *testing.T) {
	saved := swarMaskTestEnabled
	defer func() { swarMaskTestEnabled = saved }()
	i64, i32 := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}
	for _, x := range []uint64{0, 0x80, 0x8080, 0x7f7f7f7f7f7f7f7f} {
		swarMaskTestEnabled = true
		on := uint32(runArm64Internal2(t, mod1(t, i64, i32, swarMaskEqzBodyArm64()), uintptr(x), 0))
		swarMaskTestEnabled = false
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

func TestSingleBitBranchUsesBranchFoldPolicyArm64(t *testing.T) {
	m := mod1(t, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}, singleBitMaskBranchBodyArm64())
	for _, enabled := range []bool{false, true} {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: &stats, Optimizations: map[string]bool{"branch-fold": enabled}})
		if err != nil {
			t.Fatalf("branch-fold=%t: %v", enabled, err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		want := 0
		if enabled {
			want = 1
		}
		if got := stats.Funcs[0].Peephole["single-bit-test-branch"]; got != want {
			t.Fatalf("branch-fold=%t: single-bit-test-branch = %d, want %d (all: %v)", enabled, got, want, stats.Funcs[0].Peephole)
		}
	}
}
