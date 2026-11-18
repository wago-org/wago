//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestNormalZeroBranchRemainderLoop(t *testing.T) {
	old := normalZeroBranchEnabled
	t.Cleanup(func() { normalZeroBranchEnabled = old })
	body := []byte{0, 0x3f, 0, 0x1a, 0x02, 0x40, 0x20, 1, 0x45, 0x0d, 0, 0x03, 0x40, 0x20, 0, 0x20, 1, 0x22, 0, 0x70, 0x22, 1, 0x0d, 0, 0x0b, 0x0b, 0x20, 0, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	hits := 0
	for _, enabled := range []bool{false, true} {
		normalZeroBranchEnabled = enabled
		for _, compact := range []bool{false, true} {
			for _, fold := range []bool{false, true} {
				for _, args := range [][2]uint64{{0, 0}, {0, 19}, {19, 0}, {48, 18}, {65535, 65534}, {0xfffffffffffa, 0xfffffffffff9}, {0x100000001, 0x100000000}} {
					x, y := uint32(args[0]), uint32(args[1])
					for y != 0 {
						x, y = y, x%y
					}
					var stats ModuleStats
					opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"branch-fold": fold}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, args[:]...)
					if err != nil || uint32(got) != x {
						t.Fatalf("enabled%v compact%v fold%v args%x got%x want%x err%v", enabled, compact, fold, args, got, x, err)
					}
					if diagnosticsEnabled {
						hits += stats.Funcs[0].Peephole["normal-zero-branch"]
					}
				}
			}
		}
	}
	if diagnosticsEnabled && hits == 0 {
		t.Fatal("normal direct branch never selected")
	}
}

func TestNormalZeroBranchUsesI32Condition(t *testing.T) {
	old := normalZeroBranchEnabled
	t.Cleanup(func() { normalZeroBranchEnabled = old })
	body := []byte{0, 0x02, 0x40, 0x20, 0, 0x0d, 0, 0x41, 11, 0x0f, 0x0b, 0x41, 22, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, enabled := range []bool{false, true} {
		normalZeroBranchEnabled = enabled
		for _, arg := range []uint64{0, 1, 1 << 32, 1<<32 | 1, ^uint64(0)} {
			want := uint32(11)
			if uint32(arg) != 0 {
				want = 22
			}
			var stats ModuleStats
			opts := CompileOptions{}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, err := runArm64WrapperWithOptions(t, m, opts, arg)
			if diagnosticsEnabled && stats.Funcs[0].Peephole["normal-zero-branch"] != 0 {
				t.Fatal("forward branch used loop-only cover")
			}
			if err != nil || uint32(got) != want {
				t.Fatalf("enabled%v input%x got%x want%x err%v", enabled, arg, got, want, err)
			}
		}
	}
}
