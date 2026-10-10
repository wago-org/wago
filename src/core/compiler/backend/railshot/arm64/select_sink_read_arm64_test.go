//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// A saved old local value remains below a select that writes that same local.
// Read-only sources must survive both destination aliasing and local.tee.
func TestSelectSinkReadAliasing(t *testing.T) {
	old := selectSourceReadEnabled
	defer func() { selectSourceReadEnabled = old }()
	for _, wide := range []bool{false, true} {
		typ, constant, lt, add := wasm.I32, byte(0x41), byte(0x48), byte(0x6a)
		if wide {
			typ, constant, lt, add = wasm.I64, 0x42, 0x53, 0x7c
		}
		for _, tee := range []bool{false, true} {
			body := []byte{0, 0x3f, 0, 0x1a, 0x20, 0, 0x20, 0, 0x20, 1, 0x20, 0, constant, 0, lt, 0x1b}
			if tee {
				body = append(body, 0x22, 0)
			} else {
				body = append(body, 0x21, 0, 0x20, 0)
			}
			body = append(body, add, 0x0b)
			m := modMem(t, 1, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
			for _, enabled := range []bool{false, true} {
				selectSourceReadEnabled = enabled
				for _, x := range []uint64{0, 1, 0x80000000, 0xffffffff, 0x8000000000000000, ^uint64(0)} {
					for _, y := range []uint64{7, ^uint64(0)} {
						chosen := y
						if wide && int64(x) < 0 || !wide && int32(x) < 0 {
							chosen = x
						}
						want := x + chosen
						got := runArm64u(t, m, x, y)
						if !wide {
							got, want = uint64(uint32(got)), uint64(uint32(want))
						}
						if got != want {
							t.Fatalf("wide=%v tee=%v enabled=%v x=%x y=%x got=%x want=%x", wide, tee, enabled, x, y, got, want)
						}
					}
				}
			}
		}
	}
}

func selectLargeClampModule(t *testing.T, wide bool) *wasm.Module {
	typ, constant, gt, lt, add := wasm.I32, byte(0x41), byte(0x4a), byte(0x48), byte(0x6a)
	if wide {
		typ, constant, gt, lt, add = wasm.I64, 0x42, 0x55, 0x53, 0x7c
	}
	body := []byte{0, 0x3f, 0, 0x1a, 0x20, 0, 0x20, 0, constant, 0x80, 0x80, 0x7e, 0x20, 0, constant, 0x80, 0x80, 0x7e, gt, 0x1b, 0x22, 0, constant, 0xff, 0xff, 0x01, 0x20, 0, constant, 0xff, 0xff, 0x01, lt, 0x1b, 0x21, 0, 0x20, 0, add, 0x0b}
	return modMem(t, 1, []wasm.ValType{typ}, []wasm.ValType{typ}, body)
}

func TestSelectSinkLargeConstantsAndOldValues(t *testing.T) {
	old := selectSourceReadEnabled
	defer func() { selectSourceReadEnabled = old }()
	for _, wide := range []bool{false, true} {
		m := selectLargeClampModule(t, wide)
		for _, enabled := range []bool{false, true} {
			selectSourceReadEnabled = enabled
			for _, x := range []uint64{0, 1, 32767, 32768, 65535, 0xffffffff, 0xffff8000, 0x80000000, 0x8000000000000000, ^uint64(0)} {
				signed := int64(x)
				if !wide {
					signed = int64(int32(x))
				}
				clamped := signed
				if clamped < -32768 {
					clamped = -32768
				}
				if clamped > 32767 {
					clamped = 32767
				}
				want := x + uint64(clamped)
				got := runArm64u(t, m, x)
				if !wide {
					got, want = uint64(uint32(got)), uint64(uint32(want))
				}
				if got != want {
					t.Fatalf("wide=%v enabled=%v x=%x got=%x want=%x", wide, enabled, x, got, want)
				}
			}
		}
	}
}

func TestSelectSourceReadAdmissionAndPolicy(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := selectSourceReadEnabled
	defer func() { selectSourceReadEnabled = old }()
	m := selectLargeClampModule(t, false)
	for _, enabled := range []bool{false, true} {
		selectSourceReadEnabled = enabled
		stats := compileWithStats(t, m, false).Funcs[0]
		got := stats.Peephole["select-const-reuse"]
		if enabled && got == 0 || !enabled && got != 0 {
			t.Fatalf("enabled=%v reuse=%d", enabled, got)
		}
	}
	selectSourceReadEnabled = true
	stats := &ModuleStats{}
	cm, err := CompileModuleWith(m, CompileOptions{Stats: stats, Optimizations: map[string]bool{"select-source-read": false}})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		cm.CodeImage.Close()
	}
	if stats.Funcs[0].Peephole["select-source-read"] != 0 || stats.Funcs[0].Peephole["select-const-reuse"] != 0 {
		t.Fatal("policy rollback ignored")
	}
}
