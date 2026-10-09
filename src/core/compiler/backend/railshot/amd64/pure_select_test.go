//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func pureSelectModule(t testing.TB, wide, reverse bool, condition []byte, prefix int) *wasm.Module {
	t.Helper()
	typ, constant, xor, mul, add := wasm.I32, byte(0x41), byte(0x73), byte(0x6c), byte(0x6a)
	if wide {
		typ, constant, xor, mul, add = wasm.I64, 0x42, 0x85, 0x7e, 0x7c
	}
	body := []byte{0}
	for range prefix {
		body = append(body, constant, 1)
	}
	if reverse {
		body = append(body, 0x20, 0)
	}
	body = append(body, 0x20, 0, 0x20, 1, xor, constant, 17, mul, constant, 3, xor, constant, 19, mul)
	if !reverse {
		body = append(body, 0x20, 0)
	}
	body = append(body, condition...)
	body = append(body, 0x1b)
	for range prefix {
		body = append(body, add)
	}
	body = append(body, 0x0b)
	return mod1(t, []wasm.ValType{typ, typ, wasm.I32, wasm.I32}, []wasm.ValType{typ}, body)
}

func TestPureSelectCapturedArithmetic(t *testing.T) {
	saved := pureSelectEnabled
	defer func() { pureSelectEnabled = saved }()
	for _, enabled := range []bool{false, true} {
		pureSelectEnabled = enabled
		for _, wide := range []bool{false, true} {
			for _, reverse := range []bool{false, true} {
				for _, prefix := range []int{0, 20} {
					t.Run(fmt.Sprintf("enabled=%v/wide=%v/reverse=%v/prefix=%d", enabled, wide, reverse, prefix), func(t *testing.T) {
						m := pureSelectModule(t, wide, reverse, []byte{0x20, 2}, prefix)
						var stats *ModuleStats
						if diagnosticsEnabled {
							stats = &ModuleStats{}
						}
						cm, err := CompileModuleWith(m, CompileOptions{Stats: stats})
						if err != nil {
							t.Fatal(err)
						}
						if cm.CodeImage != nil {
							defer cm.CodeImage.Close()
						}
						if stats != nil && !enabled && stats.Funcs[0].Peephole["pure-select-branch"] != 0 {
							t.Fatal("disabled helper fired")
						}
						if stats != nil && enabled && prefix == 0 {
							if stats.Funcs[0].Peephole["pure-select-branch"] != 1 {
								t.Fatal("captured execution test did not use pure select")
							}
						}
						for _, xy := range [][2]uint64{{0, 0}, {7, 11}, {0xffffffff, 0x80000000}, {0xffffffffffffffff, 0x8000000000000000}} {
							for _, condition := range []uint64{0, 1, 0x80000000, 0xffffffff} {
								x, y := xy[0], xy[1]
								costly := ((x^y)*17 ^ 3) * 19
								want := x
								if (condition != 0) != reverse {
									want = costly
								}
								want += uint64(prefix)
								if !wide {
									want = uint64(uint32(want))
								}
								if got := runCompiledAmd64u(t, cm, x, y, condition, 1); got != want {
									t.Fatalf("args=%#v cond=%#x: got %#x, want %#x", xy, condition, got, want)
								}
							}
						}
					})
				}
			}
		}
	}
}

func TestPureSelectLocalAssignmentIsEager(t *testing.T) {
	saved := pureSelectEnabled
	pureSelectEnabled = true
	defer func() { pureSelectEnabled = saved }()
	// The first local.tee commits even when its arm is unused. The condition
	// overwrites x after both select values captured its old value.
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0,
		0x20, 0, 0x41, 1, 0x6a, 0x22, 0,
		0x41, 17, 0x6c, 0x41, 3, 0x73, 0x41, 19, 0x6c,
		0x20, 0,
		0x20, 1, 0x22, 0,
		0x1b, 0x20, 0, 0x6a, 0x0b,
	})
	for _, condition := range []uint64{0, 1, 37} {
		want := uint64(8)
		if condition != 0 {
			want = (8*17 ^ 3) * 19
		}
		want += condition
		if got := runAmd64u(t, m, 7, condition); got != want {
			t.Fatalf("condition %d: got %d, want %d", condition, got, want)
		}
	}
}

func TestPureSelectFixedRegisterCondition(t *testing.T) {
	saved := pureSelectEnabled
	pureSelectEnabled = true
	defer func() { pureSelectEnabled = saved }()
	for _, op := range []byte{0x6e, 0x70, 0x74, 0x76, 0x77} { // div, rem, variable shift/rotate
		m := pureSelectModule(t, false, false, []byte{0x20, 2, 0x20, 3, op}, 20)
		for _, condition := range []uint64{0, 8} {
			// With right=1, division, shift and rotate preserve zero/nonzero;
			// remainder is zero for both values.
			want := uint64(7)
			if condition != 0 && op != 0x70 {
				want = ((7^11)*17 ^ 3) * 19
			}
			want += 20
			if got := runAmd64u(t, m, 7, 11, condition, 1); got != want {
				t.Fatalf("op=%#x condition=%d: got %d, want %d", op, condition, got, want)
			}
		}
	}
}

func newPureSelectFn() *fn {
	return &fn{s: newStackWithCap(64), a: &encoder.Asm{}, stats: new(CodegenStats)}
}

func buildPureSelectStack(f *fn, leaf storage, op wOp, reverse bool) {
	if reverse {
		f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: R12})
	}
	f.pushValue(leaf)
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 17})
	f.pushBinOp(op, mtI32)
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 3})
	f.pushBinOp(opXor, mtI32)
	if !reverse {
		f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: R12})
	}
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
}

func TestPureSelectProofAndPressureFallback(t *testing.T) {
	saved := pureSelectEnabled
	pureSelectEnabled = true
	defer func() { pureSelectEnabled = saved }()
	for _, kind := range []storageKind{stConst, stReg, stSlot, stLocalReg, stLocalRef, stGlobalRef, stGlobReg, stMemRef, stFuncRef} {
		leaf := &elem{st: storage{kind: kind, typ: mtI32}}
		want := kind == stConst || kind == stReg || kind == stSlot || kind == stLocalReg
		if got := pureSelectLeaf(leaf, mtI32); got != want {
			t.Fatalf("leaf kind=%v: got %v, want %v", kind, got, want)
		}
		leaf.st.setGCRoot(true)
		if pureSelectLeaf(leaf, mtI32) {
			t.Fatal("accepted GC leaf")
		}
	}
	for _, reverse := range []bool{false, true} {
		f := newPureSelectFn()
		buildPureSelectStack(f, storage{kind: stLocalReg, typ: mtI32, reg: R13}, opMul, reverse)
		if !f.tryPureExpensiveSelect() || f.depth() != 1 || f.s.back().st.kind != stReg {
			t.Fatal("pure captured tree was not lowered")
		}
	}
	for _, op := range []wOp{opDivU, opDivS, opRemU, opRemS, opShl, opEq} {
		f := newPureSelectFn()
		buildPureSelectStack(f, storage{kind: stLocalReg, typ: mtI32, reg: R13}, op, false)
		before := f.a.Len()
		if f.tryPureExpensiveSelect() || f.a.Len() != before || f.depth() != 3 {
			t.Fatalf("accepted or changed rejected op %v", op)
		}
	}
	// One free register is enough for the condition, but not for the two
	// private registers. Reject before materializing the condition or spilling.
	f := newPureSelectFn()
	buildPureSelectStack(f, storage{kind: stLocalReg, typ: mtI32, reg: R13}, opMul, false)
	for _, r := range gpAlloc {
		if r != RDI {
			f.reserved = f.reserved.add(r)
		}
	}
	if f.tryPureExpensiveSelect() || f.depth() != 3 || f.maxSpill != 0 || f.a.Len() != 0 {
		t.Fatalf("pressure fallback changed state: depth=%d spills=%d code=%x", f.depth(), f.maxSpill, f.a.B)
	}
}

func TestPureSelectPressureKeepsCompareFlags(t *testing.T) {
	saved := pureSelectEnabled
	defer func() { pureSelectEnabled = saved }()
	for _, wide := range []bool{false, true} {
		typ, constant, xor, mul, add := wasm.I32, byte(0x41), byte(0x73), byte(0x6c), byte(0x6a)
		if wide {
			typ, constant, xor, mul, add = wasm.I64, 0x42, 0x85, 0x7e, 0x7c
		}
		body := []byte{0}
		// Earlier selects leave distinct owned results in general registers.
		// They fill the register file and remain live across the final select.
		const pressure = 24
		for range pressure {
			body = append(body, 0x20, 0, 0x20, 1, 0x20, 2, 0x1b)
		}
		body = append(body,
			0x20, 0, 0x20, 1, xor, constant, 17, mul, constant, 3, xor, constant, 19, mul,
			0x20, 0, 0x20, 2, 0x20, 3, 0x47, // i32.ne compare condition
			0x1b,
		)
		for range pressure {
			body = append(body, add)
		}
		body = append(body, 0x0b)
		m := mod1(t, []wasm.ValType{typ, typ, wasm.I32, wasm.I32}, []wasm.ValType{typ}, body)
		var baseline []byte
		for _, enabled := range []bool{false, true} {
			pureSelectEnabled = enabled
			t.Run(fmt.Sprintf("wide=%v/enabled=%v", wide, enabled), func(t *testing.T) {
				var stats *ModuleStats
				if diagnosticsEnabled {
					stats = new(ModuleStats)
				}
				cm, err := CompileModuleWith(m, CompileOptions{Stats: stats})
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				if !enabled {
					baseline = bytes.Clone(cm.Code)
				} else if !bytes.Equal(cm.Code, baseline) {
					if stats != nil {
						t.Fatalf("pressure rejection changed the fallback native code: %v", stats.Funcs[0].Peephole)
					}
					t.Fatal("pressure rejection changed the fallback native code")
				}
				if stats != nil {
					peep := stats.Funcs[0].Peephole
					if peep["pure-select-branch"] != 0 || peep["select-flags"] != 1 {
						t.Fatalf("pressure fallback lost compare fusion: %v", peep)
					}
					if stats.Funcs[0].Spills == 0 {
						t.Fatal("pressure fixture did not spill live operands")
					}
				}
				for _, xy := range [][2]uint64{{7, 3}, {0xffffffff, 7}, {0x80000000, 3}, {0xffffffffffffffff, 3}} {
					for _, condition := range []uint64{0, 1, 0x80000000} {
						x, y := xy[0], xy[1]
						want := x
						if condition != 0 {
							want = ((x^y)*17 ^ 3) * 19
						}
						prefixValue := y
						if condition != 0 {
							prefixValue = x
						}
						want += prefixValue * pressure
						if !wide {
							want = uint64(uint32(want))
						}
						if got := runCompiledAmd64u(t, cm, x, y, condition, 0); got != want {
							t.Fatalf("args=%#v condition=%#x: got %#x, want %#x", xy, condition, got, want)
						}
					}
				}
			})
		}
	}
}

func TestPureSelectProofBound(t *testing.T) {
	f := newPureSelectFn()
	f.pushValue(storage{kind: stLocalReg, typ: mtI32, reg: R13})
	for range pureSelectMaxOps + 1 {
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: 17})
		f.pushBinOp(opMul, mtI32)
	}
	if _, ok := provePureSelectChain(f.s.back(), mtI32); ok {
		t.Fatal("accepted chain beyond scan bound")
	}
}

func TestPureSelectTrapsStayEager(t *testing.T) {
	saved := pureSelectEnabled
	pureSelectEnabled = true
	defer func() { pureSelectEnabled = saved }()
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0,
		0x20, 0, 0x20, 1, 0x6d, // first arm starts with signed division
		0x41, 17, 0x6c, 0x41, 3, 0x73, 0x41, 19, 0x6c,
		0x20, 0,
		0x20, 2, 0x20, 3, 0x6e, // the condition can also trap
		0x1b, 0x0b,
	})
	for _, tc := range []struct {
		args []uint64
		want runtime.TrapCode
	}{
		{[]uint64{1, 0, 0, 1}, runtime.TrapDivZero},
		{[]uint64{0x80000000, 0xffffffff, 0, 1}, runtime.TrapDivOverflow},
		{[]uint64{0x80000000, 0xffffffff, 1, 0}, runtime.TrapDivOverflow},
		{[]uint64{1, 1, 1, 0}, runtime.TrapDivZero},
	} {
		_, _, err := runMemAmd64(t, m, nil, tc.args...)
		var trap *runtime.TrapError
		if !errors.As(err, &trap) || trap.Code != tc.want {
			t.Fatalf("args=%v: trap=%v, want %v", tc.args, err, tc.want)
		}
	}
}

func TestPureSelectScratchHasNoAllocations(t *testing.T) {
	saved := pureSelectEnabled
	pureSelectEnabled = true
	defer func() { pureSelectEnabled = saved }()
	f := newPureSelectFn()
	f.stats = nil
	f.a.B = make([]byte, 0, 256)
	allocs := testing.AllocsPerRun(100, func() {
		f.s.reset()
		f.a.B = f.a.B[:0]
		clear(f.regUser[:])
		buildPureSelectStack(f, storage{kind: stLocalReg, typ: mtI32, reg: R13}, opMul, false)
		if !f.tryPureExpensiveSelect() {
			panic("pure select admission changed")
		}
	})
	if allocs != 0 {
		t.Fatalf("allocations per captured select = %g, want 0", allocs)
	}
}

func TestPureSelectWideRightConstants(t *testing.T) {
	saved := pureSelectEnabled
	pureSelectEnabled = true
	defer func() { pureSelectEnabled = saved }()
	const mask, factor = int64(0x123456789abcdef0), int64(-4294967273)
	body := []byte{0, 0x20, 0, 0x42}
	body = append(body, wasmtest.SLEB64(mask)...)
	body = append(body, 0x85, 0x42)
	body = append(body, wasmtest.SLEB64(factor)...)
	body = append(body, 0x7e, 0x20, 0, 0x20, 1, 0x1b, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I64, wasm.I32}, []wasm.ValType{wasm.I64}, body)
	for _, x := range []uint64{0, 1, 0xffffffffffffffff, 0x8000000000000000} {
		for _, condition := range []uint64{0, 1, 0xffffffff} {
			want := x
			if condition != 0 {
				v := int64(x) ^ mask
				want = uint64(v * factor)
			}
			if got := runAmd64u(t, m, x, condition); got != want {
				t.Fatalf("x=%#x cond=%#x: got %#x, want %#x", x, condition, got, want)
			}
		}
	}
}
