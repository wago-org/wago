//go:build arm64

package arm64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func pureSelectSinkModule(t *testing.T, workOnTrue, trap, wide bool) *wasm.Module {
	typ, valType, constant, xor, mul, div, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x73), byte(0x6c), byte(0x6d), byte(0x6a)
	if wide {
		typ, valType, constant, xor, mul, div, add = wasm.I64, 0x7e, 0x42, 0x85, 0x7e, 0x7f, 0x7c
	}
	body := []byte{1, 1, valType, 0x3f, 0, 0x1a, 0x20, 0, 0x21, 3, 0x20, 3}
	keep := []byte{0x20, 3}
	work := []byte{0x20, 3, 0x20, 2, xor, constant, 19, mul}
	if trap {
		work = []byte{constant, 1, 0x20, 2, div, 0x20, 3, xor, constant, 19, mul}
	}
	if workOnTrue {
		body = append(body, work...)
		body = append(body, keep...)
	} else {
		body = append(body, keep...)
		body = append(body, work...)
	}
	body = append(body, 0x20, 1, 0x41, 0x80, 0x01, 0x71, 0x1b, 0x22, 3, add, 0x0b)
	return modMem(t, 1, []wasm.ValType{typ, wasm.I32, typ}, []wasm.ValType{typ}, body)
}

func TestPureSelectSinkGuardOldValuesAndDirections(t *testing.T) {
	old := selectSinkPureGuardEnabled
	defer func() { selectSinkPureGuardEnabled = old }()
	for _, onTrue := range []bool{false, true} {
		m := pureSelectSinkModule(t, onTrue, false, false)
		for _, enabled := range []bool{false, true} {
			selectSinkPureGuardEnabled = enabled
			for _, x := range []uint64{0, 1, 0xffffffff, 0x123456789abcdef0} {
				for _, y := range []uint64{7, 0x80000000, ^uint64(0)} {
					for _, flag := range []uint64{0, 1, 128, 255, 0x80000000} {
						value := uint32(x)
						if (flag&128 != 0) == onTrue {
							value = (uint32(x) ^ uint32(y)) * 19
						}
						want := uint32(x) + value
						got := uint32(runArm64u(t, m, x, flag, y))
						if got != want {
							t.Fatalf("true=%v enabled=%v x=%x flag=%x y=%x got=%x want=%x", onTrue, enabled, x, flag, y, got, want)
						}
					}
				}
			}
		}
	}
}

func TestPureSelectSinkGuardPreservesTraps(t *testing.T) {
	old := selectSinkPureGuardEnabled
	defer func() { selectSinkPureGuardEnabled = old }()
	for _, onTrue := range []bool{false, true} {
		m := pureSelectSinkModule(t, onTrue, true, false)
		for _, enabled := range []bool{false, true} {
			selectSinkPureGuardEnabled = enabled
			flag := uint64(128)
			if onTrue {
				flag = 0
			}
			_, err := runArm64Wrapper(t, m, 7, flag, 0)
			var trapErr *rt.TrapError
			if !errors.As(err, &trapErr) || trapErr.Code != rt.TrapDivZero {
				t.Fatalf("true=%v enabled=%v discarded division trap=%v", onTrue, enabled, err)
			}
		}
	}
}

func TestPureSelectSinkGuardAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := selectSinkPureGuardEnabled
	defer func() { selectSinkPureGuardEnabled = old }()
	for _, onTrue := range []bool{false, true} {
		for _, trap := range []bool{false, true} {
			selectSinkPureGuardEnabled = true
			stats := compileWithStats(t, pureSelectSinkModule(t, onTrue, trap, false), false).Funcs[0]
			got := stats.Peephole["select-sink-pure-guard"]
			if !trap && got == 0 || trap && got != 0 {
				t.Fatalf("true=%v trap=%v admissions=%d", onTrue, trap, got)
			}
		}
	}
}

func TestPureSelectSinkGuardI64(t *testing.T) {
	old := selectSinkPureGuardEnabled
	defer func() { selectSinkPureGuardEnabled = old }()
	for _, onTrue := range []bool{false, true} {
		m := pureSelectSinkModule(t, onTrue, false, true)
		for _, enabled := range []bool{false, true} {
			selectSinkPureGuardEnabled = enabled
			for _, x := range []uint64{0, 1, 0xffffffff, 0x8000000000000000, ^uint64(0)} {
				for _, flag := range []uint64{0, 128} {
					y := uint64(0x123456789abcdef0)
					value := x
					if (flag&128 != 0) == onTrue {
						value = (x ^ y) * 19
					}
					got := runArm64u(t, m, x, flag, y)
					if want := x + value; got != want {
						t.Fatalf("true=%v enabled=%v x=%x flag=%x got=%x want=%x", onTrue, enabled, x, flag, got, want)
					}
				}
			}
		}
		selectSinkPureGuardEnabled = true
		flag := uint64(128)
		if onTrue {
			flag = 0
		}
		_, err := runArm64Wrapper(t, pureSelectSinkModule(t, onTrue, true, true), 7, flag, 0)
		var trapErr *rt.TrapError
		if !errors.As(err, &trapErr) || trapErr.Code != rt.TrapDivZero {
			t.Fatalf("discarded i64 division trap=%v", err)
		}
	}
}

func TestPureSelectSinkGuardPolicyRollback(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := selectSinkPureGuardEnabled
	defer func() { selectSinkPureGuardEnabled = old }()
	selectSinkPureGuardEnabled = true
	stats := &ModuleStats{}
	cm, err := CompileModuleWith(pureSelectSinkModule(t, true, false, false), CompileOptions{Stats: stats, Optimizations: map[string]bool{"select-sink-pure-guard": false}})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		cm.CodeImage.Close()
	}
	if stats.Funcs[0].Peephole["select-sink-pure-guard"] != 0 {
		t.Fatal("policy rollback ignored")
	}
}
