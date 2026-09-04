//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderarm64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestDirectZeroBranchEncodingArm64(t *testing.T) {
	for _, test := range []struct {
		name   string
		wide   bool
		onZero bool
	}{
		{"cbz32", false, true},
		{"cbnz32", false, false},
		{"cbz64", true, true},
		{"cbnz64", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			stats := &CodegenStats{}
			f := fn{a: &encoderarm64.Asm{}, stats: stats}
			f.zeroBranch(X0, test.wide, test.onZero)
			if got := len(f.a.B); got != 4 {
				t.Fatalf("direct zero branch = %d bytes, want 4", got)
			}
			if got := stats.Peephole["direct-zero-branch"]; got != 1 {
				t.Fatalf("direct-zero-branch hits = %d, want 1", got)
			}
		})
	}
}

func zeroBranchOptions(enabled, compact bool, stats *ModuleStats) CompileOptions {
	return CompileOptions{
		CompactNative: compact,
		Stats:         stats,
		Optimizations: map[string]bool{"zero-branch": enabled},
	}
}

func compileZeroBranchStats(t *testing.T, m *wasm.Module, enabled, compact bool) *CodegenStats {
	t.Helper()
	stats := &ModuleStats{}
	cm, err := CompileModuleWith(m, zeroBranchOptions(enabled, compact, stats))
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		t.Cleanup(func() { _ = cm.CodeImage.Close() })
	}
	return stats.Funcs[0]
}

func TestZeroBranchIfArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	// local.get 0; if (result i32) 11 else 22 end
	body := []byte{0x00, 0x20, 0x00, 0x04, 0x7f, 0x41, 0x0b, 0x05, 0x41, 0x16, 0x0b, 0x0b}
	m := mod1(t, i32, i32, body)

	for _, enabled := range []bool{false, true} {
		for arg, want := range map[uint32]uint32{0: 22, 1: 11, ^uint32(0): 11} {
			got, err := runArm64WrapperWithOptions(t, m, zeroBranchOptions(enabled, true, nil), uint64(arg))
			if err != nil || uint32(got) != want {
				t.Fatalf("if(%d), enabled=%t = %d, err=%v, want %d", arg, enabled, got, err, want)
			}
		}
	}

	long := compileZeroBranchStats(t, m, false, true)
	short := compileZeroBranchStats(t, m, true, true)
	if got := long.CodeBytes - short.CodeBytes; got != 4 {
		t.Fatalf("CMP+B.cond delta = %d bytes, want 4", got)
	}
	if got := short.Peephole["zero-branch"]; got != 1 {
		t.Fatalf("zero-branch hits = %d, want 1", got)
	}
}

func TestZeroBranchBrIfArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	// block; local.get 0; br_if 0; return 11; end; return 22
	body := []byte{0x00, 0x02, 0x40, 0x20, 0x00, 0x0d, 0x00, 0x41, 0x0b, 0x0f, 0x0b, 0x41, 0x16, 0x0b}
	m := mod1(t, i32, i32, body)

	for _, enabled := range []bool{false, true} {
		for arg, want := range map[uint32]uint32{0: 11, 1: 22, ^uint32(0): 22} {
			got, err := runArm64WrapperWithOptions(t, m, zeroBranchOptions(enabled, true, nil), uint64(arg))
			if err != nil || uint32(got) != want {
				t.Fatalf("br_if(%d), enabled=%t = %d, err=%v, want %d", arg, enabled, got, err, want)
			}
		}
	}

	long := compileZeroBranchStats(t, m, false, true)
	short := compileZeroBranchStats(t, m, true, true)
	if got := long.CodeBytes - short.CodeBytes; got != 4 {
		t.Fatalf("CMP+B.cond delta = %d bytes, want 4", got)
	}
	if got := short.Peephole["zero-branch"]; got != 1 {
		t.Fatalf("zero-branch hits = %d, want 1", got)
	}
}

func TestZeroBranchEqzIfArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	// local.get 0; i32.eqz; if (result i32) 11 else 22 end
	body := []byte{0x00, 0x20, 0x00, 0x45, 0x04, 0x7f, 0x41, 0x0b, 0x05, 0x41, 0x16, 0x0b, 0x0b}
	m := mod1(t, i32, i32, body)
	for _, enabled := range []bool{false, true} {
		for arg, want := range map[uint32]uint32{0: 11, 1: 22, ^uint32(0): 22} {
			got, err := runArm64WrapperWithOptions(t, m, zeroBranchOptions(enabled, false, nil), uint64(arg))
			if err != nil || uint32(got) != want {
				t.Fatalf("eqz if(%d), enabled=%t = %d, err=%v, want %d", arg, enabled, got, err, want)
			}
		}
	}

	long := compileZeroBranchStats(t, m, false, false)
	short := compileZeroBranchStats(t, m, true, false)
	if got := long.CodeBytes - short.CodeBytes; got != 4 {
		t.Fatalf("CMP+B.cond delta = %d bytes, want 4", got)
	}
	if got := short.Peephole["zero-branch"]; got != 1 {
		t.Fatalf("zero-branch hits = %d, want 1", got)
	}
}
