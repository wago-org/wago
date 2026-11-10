package wagobench

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

type IntOp uint8

const (
	IntNone IntOp = iota
	IntAdd
	IntSub
	IntAnd
	IntOr
	IntXor
	IntShl
	IntShrU
	IntShrS
	IntRotl
	IntRotr
	IntMul
	IntDivU
	IntDivS
	IntRemU
	IntRemS
	IntClz
	IntCtz
	IntPopcnt
	IntWrap
	IntSExt32
	IntZExt32
	IntSExt8
	IntSExt16
	IntEq
	IntNe
	IntLtS
	IntLtU
	IntGtS
	IntGtU
	IntLeS
	IntLeU
	IntGeS
	IntGeU
	IntEqz
)

type sharingAdmissionSummary struct {
	Eligible        bool
	MaxStack, Nodes int
}

const sharingProbeMaxNodes = 16384

// sharingProbeAdmission admits only numeric signatures and nontrapping integer bodies.
// Indexed block signatures, loops, arbitrary branches, calls, effects, refs and
// mixed register banks stay on the established function compiler.
func sharingProbeAdmission(code []byte, ft *wasm.CompType, localTypes []wasm.ValType) sharingAdmissionSummary {
	s := sharingAdmissionSummary{Nodes: len(localTypes) + 1}
	if len(code) == 0 || len(code) > 64<<10 || len(localTypes) > 256 || len(ft.Results) != 1 {
		return s
	}
	integer := func(t wasm.ValType) bool { return wasm.EqualValType(t, wasm.I32) || wasm.EqualValType(t, wasm.I64) }
	for _, t := range ft.Params {
		if !integer(t) {
			return s
		}
	}
	if !integer(ft.Results[0]) {
		return s
	}
	for _, t := range localTypes {
		if !integer(t) {
			return s
		}
	}
	type frame struct {
		base, result  int
		isIf, hasElse bool
	}
	var ctrl [33]frame
	ctrl[0].result = 1
	depth, stack := 1, 0
	r := wasm.ReaderFrom(code)
	for depth > 0 {
		op, e := r.Byte()
		if e != nil {
			return s
		}
		s.Nodes++
		if s.Nodes > sharingProbeMaxNodes {
			return s
		}
		switch op {
		case 0x01:
		case 0x41:
			if _, e = r.I32(); e != nil {
				return s
			}
			stack++
		case 0x42:
			if _, e = r.I64(); e != nil {
				return s
			}
			stack++
		case 0x20, 0x21, 0x22:
			x, e := r.U32()
			if e != nil || int(x) >= len(localTypes) {
				return s
			}
			if op == 0x20 {
				stack++
			} else if op == 0x21 {
				stack--
			}
		case 0x1a:
			stack--
		case 0x02, 0x04:
			s.Nodes += len(localTypes) + 2*s.MaxStack
			t, e := r.Byte()
			if e != nil || depth == len(ctrl) {
				return s
			}
			result := 0
			if t == 0x7f || t == 0x7e {
				result = 1
			} else if t != 0x40 {
				return s
			}
			if op == 0x04 {
				stack--
			}
			ctrl[depth] = frame{base: stack, result: result, isIf: op == 0x04}
			depth++
		case 0x05:
			s.Nodes += 2 * (len(localTypes) + s.MaxStack)
			f := &ctrl[depth-1]
			if !f.isIf || f.hasElse || stack != f.base+f.result {
				return s
			}
			f.hasElse = true
			stack = f.base
		case 0x0b:
			s.Nodes += len(localTypes) + s.MaxStack
			f := ctrl[depth-1]
			if stack != f.base+f.result || f.isIf && f.result != 0 && !f.hasElse {
				return s
			}
			depth--
		case 0x0f:
			// Tail return only: no unreachable polymorphic suffix or inline state.
			if depth != 1 || r.Offset() != len(code)-1 || code[r.Offset()] != 0x0b || stack != 1 {
				return s
			}
		default:
			if _, _, ok := sharingProbeOpcode(op); !ok {
				return s
			}
			stack--
		}
		if stack < 0 || stack > 512 {
			return s
		}
		if stack > s.MaxStack {
			s.MaxStack = stack
		}
	}
	if r.Offset() != len(code) {
		return s
	}
	if s.Nodes > sharingProbeMaxNodes {
		return sharingAdmissionSummary{}
	}
	s.Eligible = true
	return s
}

// sharingProbeOpcode maps only the admitted nontrapping semantics. Width refers to
// operands; comparisons produce i32.
func sharingProbeOpcode(op byte) (IntOp, bool, bool) {
	switch {
	case op >= 0x6a && op <= 0x6c:
		return []IntOp{IntAdd, IntSub, IntMul}[op-0x6a], false, true
	case op >= 0x7c && op <= 0x7e:
		return []IntOp{IntAdd, IntSub, IntMul}[op-0x7c], true, true
	case op >= 0x71 && op <= 0x76:
		return []IntOp{IntAnd, IntOr, IntXor, IntShl, IntShrS, IntShrU}[op-0x71], false, true
	case op >= 0x83 && op <= 0x88:
		return []IntOp{IntAnd, IntOr, IntXor, IntShl, IntShrS, IntShrU}[op-0x83], true, true
	case op >= 0x46 && op <= 0x4f:
		return []IntOp{IntEq, IntNe, IntLtS, IntLtU, IntGtS, IntGtU, IntLeS, IntLeU, IntGeS, IntGeU}[op-0x46], false, true
	case op >= 0x51 && op <= 0x5a:
		return []IntOp{IntEq, IntNe, IntLtS, IntLtU, IntGtS, IntGtU, IntLeS, IntLeU, IntGeS, IntGeU}[op-0x51], true, true
	}
	return IntNone, false, false
}

func BenchmarkSharingAdmission(b *testing.B) {
	for _, f := range sharingFixtures() {
		b.Run(f.name, func(b *testing.B) {
			m, e := wasm.DecodeModule(f.bytes)
			if e != nil {
				b.Fatal(e)
			}
			ft, _ := m.LocalFuncType(0)
			fn := m.Code[0]
			var summary sharingAdmissionSummary
			b.ReportAllocs()
			b.ResetTimer()
			for j := 0; j < b.N; j++ {
				var types [256]wasm.ValType
				k := copy(types[:], ft.Params)
				for _, run := range fn.Locals.Runs {
					for z := uint32(0); z < run.Count; z++ {
						types[k] = run.Type
						k++
					}
				}
				summary = sharingProbeAdmission(fn.BodyBytes, ft, types[:k])
			}
			b.StopTimer()
			b.ReportMetric(float64(summary.Nodes), "node-bound")
		})
	}
}
