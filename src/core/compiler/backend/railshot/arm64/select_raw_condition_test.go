//go:build arm64

package arm64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func rawConditionSelectModule(t *testing.T, load, workOnTrue, trappingWork bool) *wasm.Module {
	body := []byte{1, 1, 0x7f, 0x3f, 0, 0x1a}
	if load {
		body = append(body, 0x41, 0, 0x20, 2, 0x36, 2, 0)
	}
	body = append(body, 0x20, 0, 0x21, 3, 0x20, 3)
	work := []byte{0x20, 3, 0x20, 2, 0x73, 0x41, 19, 0x6c}
	if trappingWork {
		work = []byte{0x41, 1, 0x20, 2, 0x6d, 0x20, 3, 0x73, 0x41, 19, 0x6c}
	}
	keep := []byte{0x20, 3}
	if workOnTrue {
		body = append(body, work...)
		body = append(body, keep...)
	} else {
		body = append(body, keep...)
		body = append(body, work...)
	}
	body = append(body, 0x20, 1)
	if load {
		body = append(body, 0x28, 2, 0)
	}
	body = append(body, 0x1b, 0x22, 3, 0x6a, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
}

func TestPureSelectRawCondition(t *testing.T) {
	old := selectSinkPureGuardEnabled
	defer func() { selectSinkPureGuardEnabled = old }()
	for _, load := range []bool{false, true} {
		for _, onTrue := range []bool{false, true} {
			m := rawConditionSelectModule(t, load, onTrue, false)
			for _, enabled := range []bool{false, true} {
				selectSinkPureGuardEnabled = enabled
				for _, flag := range []uint64{0, 1, 128, 0x80000000, 0xffffffff, 1 << 32} {
					for _, x := range []uint64{0, 7, 0xffffffff, 0x123456789abcdef0} {
						arg, y := flag, uint64(0x80000001)
						if load {
							arg, y = 0, flag
						}
						value := uint32(x)
						if (uint32(flag) != 0) == onTrue {
							value = (value ^ uint32(y)) * 19
						}
						got := uint32(runArm64u(t, m, x, arg, y))
						if want := uint32(x) + value; got != want {
							t.Fatalf("load=%v true=%v enabled=%v x=%x flag=%x got=%x want=%x", load, onTrue, enabled, x, flag, got, want)
						}
					}
				}
			}
		}
	}
}

func TestPureSelectRawConditionTraps(t *testing.T) {
	for _, onTrue := range []bool{false, true} {
		_, err := runArm64Wrapper(t, rawConditionSelectModule(t, true, onTrue, false), 7, 65533, 0)
		var trap *rt.TrapError
		if !errors.As(err, &trap) || trap.Code != rt.TrapLinMemOutOfBounds {
			t.Fatalf("condition load trap: %v", err)
		}
		_, err = runArm64Wrapper(t, rawConditionSelectModule(t, true, onTrue, true), 7, 65533, 0)
		if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
			t.Fatalf("division must trap before condition load: %v", err)
		}
		flag := uint64(0)
		if !onTrue {
			flag = 1
		}
		_, err = runArm64Wrapper(t, rawConditionSelectModule(t, false, onTrue, true), 7, flag, 0)
		if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
			t.Fatalf("discarded work trap: %v", err)
		}
	}
}

func TestPureSelectRawConditionAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, load := range []bool{false, true} {
		for _, onTrue := range []bool{false, true} {
			for _, trap := range []bool{false, true} {
				stats := compileWithStats(t, rawConditionSelectModule(t, load, onTrue, trap), false).Funcs[0]
				if got := stats.Peephole["select-sink-pure-guard"]; !trap && got == 0 {
					t.Fatalf("load=%v true=%v trap=%v admission=%d", load, onTrue, trap, got)
				}
			}
		}
	}
}

func TestPureSelectRawConditionI64(t *testing.T) {
	old := selectSinkPureGuardEnabled
	defer func() { selectSinkPureGuardEnabled = old }()
	for _, onTrue := range []bool{false, true} {
		body := []byte{1, 1, 0x7e, 0x3f, 0, 0x1a, 0x20, 0, 0x21, 3, 0x20, 3}
		keep := []byte{0x20, 3}
		work := []byte{0x20, 3, 0x20, 2, 0x85, 0x42, 19, 0x7e}
		if onTrue {
			body = append(body, work...)
			body = append(body, keep...)
		} else {
			body = append(body, keep...)
			body = append(body, work...)
		}
		body = append(body, 0x20, 1, 0x1b, 0x22, 3, 0x7c, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I64, wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}, body)
		for _, enabled := range []bool{false, true} {
			selectSinkPureGuardEnabled = enabled
			for _, flag := range []uint64{0, 1, 0x80000000, 1 << 32} {
				for _, x := range []uint64{0, 7, 0x8000000000000000, ^uint64(0)} {
					y := uint64(0x123456789abcdef0)
					value := x
					if (uint32(flag) != 0) == onTrue {
						value = (x ^ y) * 19
					}
					if got := runArm64u(t, m, x, flag, y); got != x+value {
						t.Fatalf("true=%v enabled=%v x=%x flag=%x got=%x want=%x", onTrue, enabled, x, flag, got, x+value)
					}
				}
			}
		}
	}
}

func TestPureSelectStackResult(t *testing.T) {
	old := pureSelectEnabled
	defer func() { pureSelectEnabled = old }()
	for _, wide := range []bool{false, true} {
		typ := wasm.I32
		xor, mul, cnst, add := byte(0x73), byte(0x6c), byte(0x41), byte(0x6a)
		if wide {
			typ = wasm.I64
			xor, mul, cnst, add = 0x85, 0x7e, 0x42, 0x7c
		}
		for _, workOnTrue := range []bool{false, true} {
			body := []byte{0}
			work := []byte{0x20, 0, 0x20, 1, xor, cnst, 19, mul}
			cheap := []byte{0x20, 2}
			if workOnTrue {
				body = append(body, work...)
				body = append(body, cheap...)
			} else {
				body = append(body, cheap...)
				body = append(body, work...)
			}
			body = append(body, 0x20, 3, 0x1b, cnst, 7, add, 0x0b)
			m := modMem(t, 1, []wasm.ValType{typ, typ, typ, wasm.I32}, []wasm.ValType{typ}, body)
			for _, enabled := range []bool{false, true} {
				pureSelectEnabled = enabled
				for _, flag := range []uint64{0, 0x80000000, 1 << 32} {
					x, y, fallback := uint64(0xe71ac9307318cd56), uint64(0x10131e8fa43d69e4), uint64(0xfdb97531)
					want := fallback
					if (uint32(flag) != 0) == workOnTrue {
						want = (x ^ y) * 19
					}
					want += 7
					got := runArm64u(t, m, x, y, fallback, flag)
					if !wide {
						want, got = uint64(uint32(want)), uint64(uint32(got))
					}
					if got != want {
						t.Fatalf("wide=%v workOnTrue=%v enabled=%v flag=%x: got %x, want %x", wide, workOnTrue, enabled, flag, got, want)
					}
				}
				if diagnosticsEnabled {
					hits := compileWithStats(t, m, false).Funcs[0].Peephole["pure-select-branch"]
					if (hits != 0) != enabled {
						t.Fatalf("wide=%v workOnTrue=%v enabled=%v: pure select hits=%d", wide, workOnTrue, enabled, hits)
					}
				}
			}
		}
	}
}
