//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"bytes"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestFloatCompareBranchSemantics(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, first := wasm.F32, byte(0x5b)
		values := []uint64{0, 0x80000000, 0x3f800000, 0xbf800000, 0x7f800000, 0xff800000, 0x7fc12345, 0x7f812345}
		if wide {
			typ, first = wasm.F64, 0x61
			values = []uint64{0, 0x8000000000000000, 0x3ff0000000000000, 0xbff0000000000000, 0x7ff0000000000000, 0xfff0000000000000, 0x7ff8000000001234, 0x7ff0000000001234}
		}
		for k := byte(0); k < 6; k++ {
			for _, branch := range []bool{false, true} {
				for _, invert := range []bool{false, true} {
					body := []byte{0}
					if branch {
						body = append(body, 0x02, 0x7f, 0x41, 17)
					}
					body = append(body, 0x20, 0, 0x20, 1, first+k)
					if invert {
						body = append(body, 0x45)
					}
					if branch {
						body = append(body, 0x0d, 0, 0x1a, 0x41, 29, 0x0b)
					} else {
						body = append(body, 0x04, 0x7f, 0x41, 17, 0x05, 0x41, 29, 0x0b)
					}
					body = append(body, 0x0b)
					m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{wasm.I32}, body)
					for _, a := range values {
						for _, b := range values {
							x, y := math.Float64frombits(a), math.Float64frombits(b)
							if !wide {
								x, y = float64(math.Float32frombits(uint32(a))), float64(math.Float32frombits(uint32(b)))
							}
							truth := false
							switch k {
							case 0:
								truth = x == y
							case 1:
								truth = x != y
							case 2:
								truth = x < y
							case 3:
								truth = x > y
							case 4:
								truth = x <= y
							case 5:
								truth = x >= y
							}
							if invert {
								truth = !truth
							}
							want := uint64(29)
							if truth {
								want = 17
							}
							for _, on := range []bool{false, true} {
								var stats ModuleStats
								opts := CompileOptions{Optimizations: map[string]bool{"st-flags": on}}
								if diagnosticsEnabled {
									opts.Stats = &stats
								}
								got, err := runArm64WrapperWithOptions(t, m, opts, a, b)
								if err != nil || got != want {
									t.Fatalf("wide=%v op=%d br=%v invert=%v on=%v a=%x b=%x got=%d want=%d err=%v", wide, k, branch, invert, on, a, b, got, want, err)
								}
								if diagnosticsEnabled && (stats.Funcs[0].Peephole["float-branch-fuse"] > 0) != on {
									t.Fatalf("fusion on=%v stats=%v", on, stats.Funcs[0].Peephole)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestFloatCompareBranchPendingLoads(t *testing.T) {
	for _, wide := range []bool{false, true} {
		ft, load, align, first := wasm.F32, byte(0x2a), byte(2), byte(0x5b)
		if wide {
			ft, load, align, first = wasm.F64, 0x2b, 3, 0x61
		}
		for k := byte(0); k < 6; k++ {
			body := []byte{1, 1, 0x7f}
			for i := byte(1); i <= 40; i++ {
				body = append(body, 0x20, 2, 0x41, i, 0x6c, load, align, 0)
			}
			body = append(body, 0x20, 0, 0x20, 1, first+k, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 0, 0x0b, 0x21, 3)
			for range 40 {
				body = append(body, 0x1a)
			}
			body = append(body, 0x20, 3, 0x0b)
			m := mod1(t, []wasm.ValType{ft, ft, wasm.I32}, []wasm.ValType{wasm.I32}, body)
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			a, b := uint64(0x7f812345), uint64(0x3f800000)
			if wide {
				a, b = 0x7ff0000000001234, 0x3ff0000000000000
			}
			got, err := runArm64Wrapper(t, m, a, b, 8)
			want := uint64(0)
			if k == 1 {
				want = 1
			}
			if err != nil || got != want {
				t.Fatalf("wide=%v op=%d got=%d want=%d err=%v", wide, k, got, want, err)
			}
		}
	}
}

func TestFloatCompareBranchLoadTrap(t *testing.T) {
	for _, wide := range []bool{false, true} {
		load, align, first := byte(0x2a), byte(2), byte(0x5b)
		if wide {
			load, align, first = 0x2b, 3, 0x61
		}
		for k := byte(0); k < 6; k++ {
			// Even comparisons whose result is false must evaluate the load.
			body := []byte{0, 0x20, 0, load, align, 0, 0x41, 0, load, align, 0, first + k, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 0, 0x0b, 0x0b}
			m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			got, err := runArm64WrapperMem(t, m, 0, nil)
			want := uint32(1)
			if k == 1 || k == 2 || k == 3 {
				want = 0
			}
			if err != nil || got != want {
				t.Fatalf("wide=%v op=%d got=%d want=%d err=%v", wide, k, got, want, err)
			}
			if _, err := runArm64WrapperMem(t, m, 65536, nil); err == nil {
				t.Fatalf("wide=%v op=%d: load trap eliminated", wide, k)
			}
		}
	}
}

func TestFloatSelfComparePressureAndBits(t *testing.T) {
	for _, wide := range []bool{false, true} {
		ft, first, reinterpret := wasm.F32, byte(0x5b), byte(0xbc)
		values := []uint64{0, 0x80000000, 0x7f800000, 0xff800000, 0x7fc12345, 0x7f812345, 0x3f800000}
		if wide {
			ft, first, reinterpret = wasm.F64, 0x61, 0xbd
			values = []uint64{0, 0x8000000000000000, 0x7ff0000000000000, 0xfff0000000000000, 0x7ff8000000001234, 0x7ff0000000001234, 0x3ff0000000000000}
		}
		for k := byte(0); k < 6; k++ {
			body := []byte{1, 24, 0x7f}
			for i := 0; i < 24; i++ {
				body = append(body, 0x20, 1, 0x41, byte(i+1), 0x6a, 0x22, byte(i+2))
			}
			body = append(body, 0x20, 0, 0x20, 0, first+k)
			for range 24 {
				body = append(body, 0x73)
			}
			body = append(body, 0xad, 0x20, 0, reinterpret)
			if !wide {
				body = append(body, 0xad)
			}
			body = append(body, 0x85, 0x0b)
			m := mod1(t, []wasm.ValType{ft, wasm.I32}, []wasm.ValType{wasm.I64}, body)
			for _, bits := range values {
				x := math.Float64frombits(bits)
				if !wide {
					x = float64(math.Float32frombits(uint32(bits)))
				}
				truth := !math.IsNaN(x)
				if k == 1 {
					truth = !truth
				}
				if k == 2 || k == 3 {
					truth = false
				}
				want := uint64(0)
				if truth {
					want = 1
				}
				for i := 0; i < 24; i++ {
					want ^= 7 + uint64(i+1)
				}
				want ^= bits
				got, err := runArm64Wrapper(t, m, bits, 7)
				if err != nil || got != want {
					t.Fatalf("wide=%v op=%d bits=%x got=%x want=%x err=%v", wide, k, bits, got, want, err)
				}
			}
		}
	}
}

// A retained local reference can force the deferred predicate to materialize.
func TestFloatCompareBranchMaterializationOwnership(t *testing.T) {
	for _, op := range []wOp{opFEq, opFNe, opFLt, opFGt, opFLe, opFGe} {
		f := fn{a: &a64.Asm{}, s: newStack(), reserved: maskOf(9)}
		f.pushFReg(0, mtF64)
		leftID := f.s.head.prev
		f.pushFReg(1, mtF64)
		rightID := f.s.head.prev
		id, node := f.s.alloc()
		node.setElemKind(ekDeferred)
		node.setDeferredOp(op)
		node.st.typ = mtI32
		node.setChildren(leftID, rightID)
		f.s.push(id, node)
		if got := f.condenseFloatCompare(node, 8); got != 8 {
			t.Fatalf("op=%v result=%v", op, got)
		}
		if node.st.kind != stReg || node.st.typ != mtI32 || f.regUser[8] != node || f.fregUser[0] != nil || f.fregUser[1] != nil || f.reserved != maskOf(9) {
			t.Fatalf("op=%v: materialization ownership or reservations changed", op)
		}
		var want a64.Asm
		xa, xb, cc := Reg(0), Reg(1), condE
		switch op {
		case opFNe:
			cc = condNE
		case opFLt:
			xa, xb, cc = 1, 0, condG
		case opFGt:
			cc = condG
		case opFLe:
			xa, xb, cc = 1, 0, condGE
		case opFGe:
			cc = condGE
		}
		want.Fcmp(xa, xb, true)
		want.Cset32(8, cc)
		if !bytes.Equal(f.a.B, want.B) {
			t.Fatalf("op=%v got=%x want=%x", op, f.a.B, want.B)
		}
	}
}

func TestFloatSelfCompareZeroInstruction(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, op := range []wOp{opLtS, opGtS} {
			f := fn{a: &a64.Asm{}}
			f.emitFCmpCset(op, 8, 8, wide, 9)
			var want a64.Asm
			want.MovImm32(9, 0)
			if !bytes.Equal(f.a.B, want.B) {
				t.Fatalf("wide=%v op=%v got=%x want=%x", wide, op, f.a.B, want.B)
			}
		}
	}
}

func TestFloatCompareSelectSemantics(t *testing.T) {
	for _, wide := range []bool{false, true} {
		ft, first := wasm.F32, byte(0x5b)
		values := []uint64{0, 0x80000000, 0x3f800000, 0xbf800000, 0x7f800000, 0x7fc12345, 0x7f812345}
		if wide {
			ft, first = wasm.F64, 0x61
			values = []uint64{0, 0x8000000000000000, 0x3ff0000000000000, 0xbff0000000000000, 0x7ff0000000000000, 0x7ff8000000001234, 0x7ff0000000001234}
		}
		for k := byte(0); k < 6; k++ {
			for _, invert := range []bool{false, true} {
				for _, out := range []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64} {
					// Integer selections fuse to CSEL; float selections exercise the materialized fallback.
					body := []byte{0, 0x20, 2, 0x20, 3, 0x20, 0, 0x20, 1, first + k}
					if invert {
						body = append(body, 0x45)
					}
					body = append(body, 0x1b, 0x0b)
					m := mod1(t, []wasm.ValType{ft, ft, out, out}, []wasm.ValType{out}, body)
					for _, a := range values {
						for _, b := range values {
							x, y := math.Float64frombits(a), math.Float64frombits(b)
							if !wide {
								x, y = float64(math.Float32frombits(uint32(a))), float64(math.Float32frombits(uint32(b)))
							}
							truth := false
							switch k {
							case 0:
								truth = x == y
							case 1:
								truth = x != y
							case 2:
								truth = x < y
							case 3:
								truth = x > y
							case 4:
								truth = x <= y
							case 5:
								truth = x >= y
							}
							if invert {
								truth = !truth
							}
							left, right := uint64(17), uint64(29)
							if out == wasm.F32 {
								left, right = 0x7f812345, 0x80000000
							}
							if out == wasm.F64 {
								left, right = 0x7ff0000000001234, 0x8000000000000000
							}
							if out == wasm.I64 {
								left, right = 0x123456789abcdef0, 0xfedcba9876543210
							}
							want := right
							if truth {
								want = left
							}
							got, err := runArm64Wrapper(t, m, a, b, left, right)
							if err != nil || got != want {
								t.Fatalf("wide=%v op=%d invert=%v out=%v a=%x b=%x got=%x want=%x err=%v", wide, k, invert, out, a, b, got, want, err)
							}
						}
					}
				}
			}
		}
	}
}

func TestFloatCompareTypedSelect(t *testing.T) {
	for _, out := range []wasm.ValType{wasm.I64, wasm.F64} {
		typ := byte(0x7e)
		if out == wasm.F64 {
			typ = 0x7c
		}
		for k := byte(0); k < 6; k++ {
			body := []byte{0, 0x20, 2, 0x20, 3, 0x20, 0, 0x20, 1, 0x61 + k, 0x1c, 1, typ, 0x0b}
			m := mod1(t, []wasm.ValType{wasm.F64, wasm.F64, out, out}, []wasm.ValType{out}, body)
			left, right := uint64(0x123456789abcdef0), uint64(0xfedcba9876543210)
			if out == wasm.F64 {
				left, right = 0x7ff0000000001234, 0x8000000000000000
			}
			for _, nan := range []bool{false, true} {
				a, b := uint64(0), uint64(0x8000000000000000)
				if nan {
					a = 0x7ff0000000001234
				}
				want := right
				if (nan && k == 1) || (!nan && (k == 0 || k == 4 || k == 5)) {
					want = left
				}
				got, err := runArm64Wrapper(t, m, a, b, left, right)
				if err != nil || got != want {
					t.Fatalf("out=%v op=%d nan=%v got=%x want=%x err=%v", out, k, nan, got, want, err)
				}
			}
		}
	}
}
