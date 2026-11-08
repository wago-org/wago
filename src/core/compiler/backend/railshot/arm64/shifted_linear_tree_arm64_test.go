//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func shiftedLinearModule(t testing.TB, shift int32, capture bool) *wasm.Module {
	return shiftedLinearModuleParent(t, shift, capture, false)
}

func shiftedLinearModuleParent(t testing.TB, shift int32, capture, parent bool) *wasm.Module {
	body := []byte{0, 0x03, 0x7f}
	if parent {
		body = append(body, 0x20, 3)
	}
	for i, c := range []int32{1073741776, 1073741764, 1073741797} {
		body = append(body, 0x20, byte(i), 0x41)
		body = append(body, wasmtest.SLEB32(c)...)
		body = append(body, 0x6c)
		if capture && i == 0 {
			body = append(body, 0x20, 0, 0x41, 1, 0x6a, 0x21, 0)
		}
		if i != 0 {
			body = append(body, 0x6a)
		}
	}
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(shift)...)
	body = append(body, 0x74)
	if parent {
		body = append(body, 0x6a)
	}
	body = append(body, 0x0b, 0x0b)
	params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}
	if parent {
		params = append(params, wasm.I32)
	}
	return mod1(t, params, []wasm.ValType{wasm.I32}, body)
}

func TestShiftedLinearTreeWrapAndMaskedCount(t *testing.T) {
	for _, shift := range []int32{2, 34, 66} {
		m := shiftedLinearModule(t, shift, false)
		for _, cache := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				for _, x := range []uint64{0, 1, 0x80000000, 0xdeadbeefffffffff} {
					for _, y := range []uint64{0, 7, 0xcafebabe80000001} {
						z := uint64(0x87654321)
						var stats ModuleStats
						opts := CompileOptions{Optimizations: map[string]bool{"linear-integer-tree": enabled, "loop-int-const": cache, "scoped-loop-int-const": cache}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, x, y, z)
						want := uint64((uint32(x)*1073741776 + uint32(y)*1073741764 + uint32(z)*1073741797) << uint32(shift&31))
						if err != nil || got != want {
							t.Fatalf("shift=%d cache=%v on=%v x=%x y=%x got=%x want=%x err=%v", shift, cache, enabled, x, y, got, want, err)
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["shifted-linear-tree"] != 0) != (enabled && !cache) {
							t.Fatalf("unexpected selection shift=%d cache=%v on=%v: %v", shift, cache, enabled, stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}

func TestShiftedLinearTreePreservesCapturedLocal(t *testing.T) {
	m := shiftedLinearModule(t, 2, true)
	for _, x := range []uint64{0, 1, 0x80000000, 0xffffffff} {
		opts := CompileOptions{Optimizations: map[string]bool{"loop-int-const": false, "scoped-loop-int-const": false}}
		var stats ModuleStats
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		got, err := runArm64WrapperWithOptions(t, m, opts, x, 11, 23)
		y, z := uint32(11), uint32(23)
		want := uint64((uint32(x)*1073741776 + y*1073741764 + z*1073741797) << 2)
		if err != nil || got != want {
			t.Fatalf("captured x=%x: got=%x want=%x err=%v", x, got, want, err)
		}
		if diagnosticsEnabled && stats.Funcs[0].Peephole["shifted-linear-tree"] != 0 {
			t.Fatal("captured local was admitted")
		}
	}
}

func TestShiftedLinearTreeInsideAdd(t *testing.T) {
	m := shiftedLinearModuleParent(t, 2, false, true)
	for _, enabled := range []bool{false, true} {
		for _, x := range []uint64{0, 1, 0x80000000, 0xdeadbeefffffffff} {
			var stats ModuleStats
			opts := CompileOptions{Optimizations: map[string]bool{"linear-integer-tree": enabled, "loop-int-const": false, "scoped-loop-int-const": false}}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			y, z, prefix := uint32(11), uint32(23), uint32(0x87654321)
			got, err := runArm64WrapperWithOptions(t, m, opts, x, uint64(y), uint64(z), uint64(prefix))
			want := uint64(((uint32(x)*1073741776 + y*1073741764 + z*1073741797) << 2) + prefix)
			if err != nil || got != want {
				t.Fatalf("on=%v x=%x: got=%x want=%x err=%v", enabled, x, got, want, err)
			}
			if diagnosticsEnabled && (stats.Funcs[0].Peephole["shifted-linear-tree"] != 0) != enabled {
				t.Fatalf("parent selection on=%v: %v", enabled, stats.Funcs[0].Peephole)
			}
		}
	}
}
