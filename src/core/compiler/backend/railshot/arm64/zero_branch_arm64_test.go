//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderarm64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestDirectZeroBranchEncodingArm64(t *testing.T) {
	before := directZeroBranchEnabled
	t.Cleanup(func() { directZeroBranchEnabled = before })
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
			emit := func(enabled bool) (int, int) {
				directZeroBranchEnabled = enabled
				stats := &CodegenStats{}
				f := fn{a: &encoderarm64.Asm{}, stats: stats}
				f.zeroBranch(X0, test.wide, test.onZero)
				return len(f.a.B), stats.Peephole["direct-zero-branch"]
			}
			long, longHits := emit(false)
			short, shortHits := emit(true)
			if long-short != 4 {
				t.Fatalf("CMP+B.cond delta = %d bytes, want 4", long-short)
			}
			if longHits != 0 || shortHits != 1 {
				t.Fatalf("direct-zero-branch hits = %d/%d, want 0/1", longHits, shortHits)
			}
		})
	}
}

func TestZeroBranchIfArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	// local.get 0; if (result i32) 11 else 22 end
	body := []byte{0x00, 0x20, 0x00, 0x04, 0x7f, 0x41, 0x0b, 0x05, 0x41, 0x16, 0x0b, 0x0b}
	m := mod1(t, i32, i32, body)

	for arg, want := range map[uint32]uint32{0: 22, 1: 11, ^uint32(0): 11} {
		if got := uint32(runArm64Internal2(t, m, uintptr(arg), 0)); got != want {
			t.Fatalf("if(%d) = %d, want %d", arg, got, want)
		}
	}

	before := zeroBranchEnabled
	t.Cleanup(func() { zeroBranchEnabled = before })
	compileCompact := func(enabled bool) *CodegenStats {
		zeroBranchEnabled = enabled
		stats := &ModuleStats{}
		cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: stats})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			t.Cleanup(func() { cm.CodeImage.Close() })
		}
		return stats.Funcs[0]
	}
	zeroBranchEnabled = false
	long := compileCompact(false)
	short := compileCompact(true)
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

	for arg, want := range map[uint32]uint32{0: 11, 1: 22, ^uint32(0): 22} {
		if got := uint32(runArm64Internal2(t, m, uintptr(arg), 0)); got != want {
			t.Fatalf("br_if(%d) = %d, want %d", arg, got, want)
		}
	}

	before := zeroBranchEnabled
	t.Cleanup(func() { zeroBranchEnabled = before })
	compileCompact := func(enabled bool) *CodegenStats {
		zeroBranchEnabled = enabled
		stats := &ModuleStats{}
		cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: stats})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			t.Cleanup(func() { cm.CodeImage.Close() })
		}
		return stats.Funcs[0]
	}
	long := compileCompact(false)
	short := compileCompact(true)
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
	for arg, want := range map[uint32]uint32{0: 11, 1: 22, ^uint32(0): 22} {
		if got := uint32(runArm64Internal2(t, m, uintptr(arg), 0)); got != want {
			t.Fatalf("eqz if(%d) = %d, want %d", arg, got, want)
		}
	}

	before := zeroBranchEnabled
	t.Cleanup(func() { zeroBranchEnabled = before })
	zeroBranchEnabled = false
	long := compileWithStats(t, m, false).Funcs[0]
	zeroBranchEnabled = true
	short := compileWithStats(t, m, false).Funcs[0]
	if got := long.CodeBytes - short.CodeBytes; got != 4 {
		t.Fatalf("CMP+B.cond delta = %d bytes, want 4", got)
	}
	if got := short.Peephole["zero-branch"]; got != 1 {
		t.Fatalf("zero-branch hits = %d, want 1", got)
	}
}
