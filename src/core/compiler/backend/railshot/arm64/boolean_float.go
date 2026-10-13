//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

var booleanFloatSelectEnabled = true

type booleanFloatKey struct {
	typ  machineType
	bits int64
}

// Bound lookbehind and compile cost to small call-free loop bodies. Reserve the
// usual two FP cache registers only when no other hot literal would be evicted.
func (f *fn) preloadBooleanFloatConsts(code []byte) bool {
	if !f.opt(optBooleanFloatSelect) || !f.hasLoop || f.makesCalls || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || len(code) > 512 || len(f.fconsts) != 0 {
		return false
	}
	var positions [32]int
	seen := 0
	var frames [64]bool
	depth, loops := 0, 0
	var hot [32]booleanFloatKey
	hotN := 0
	var pair [2]booleanFloatKey
	found := false
	r := wasm.NewReader(code)
	var immediate wasm.InstructionImmediate
	for r.HasNext() {
		at := r.Offset()
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if loops > 0 && (op == 0xb2 || op == 0xb3 || op == 0xb7 || op == 0xb8) {
			matched := false
			for n := 1; n <= min(seen, len(positions)); n++ {
				candidate := wasm.NewReader(code)
				if candidate.JumpTo(positions[(seen-n)%len(positions)]) != nil {
					return false
				}
				expression, ok := shared.InspectBooleanFloat(candidate)
				if !ok || expression.End != r.Offset() {
					continue
				}
				typ := mtOf2(expression.Wide)
				keys := [2]booleanFloatKey{{typ, int64(shared.BooleanFloatBits(expression.Values[0], expression.Wide, expression.Signed))}, {typ, int64(shared.BooleanFloatBits(expression.Values[1], expression.Wide, expression.Signed))}}
				if keys[0] == keys[1] {
					return false
				}
				if found && !(pair == keys || pair == ([2]booleanFloatKey{keys[1], keys[0]})) {
					return false
				}
				pair = keys
				found = true
				matched = true
				break
			}
			if !matched {
				return false
			}
		}
		switch op {
		case 0x02, 0x03, 0x04:
			if depth == len(frames) {
				return false
			}
			frames[depth] = op == 0x03
			depth++
			if op == 0x03 {
				loops++
			}
		case 0x0b:
			if depth > 0 {
				depth--
				if frames[depth] {
					loops--
				}
			}
		}
		if op == 0x43 || op == 0x44 {
			key := booleanFloatKey{typ: mtF32}
			if op == 0x43 {
				v, err := r.LEU32()
				if err != nil {
					return false
				}
				key.bits = int64(v)
			} else {
				v, err := r.LEU64()
				if err != nil {
					return false
				}
				key.typ = mtF64
				key.bits = int64(v)
			}
			if loops > 0 {
				if hotN == len(hot) {
					return false
				}
				hot[hotN] = key
				hotN++
			}
		} else if f.classifier.ClassifyInto(r, op, &immediate) != nil {
			return false
		}
		positions[seen%len(positions)] = at
		seen++
	}
	if !found {
		return false
	}
	for _, key := range hot[:hotN] {
		if key != pair[0] && key != pair[1] {
			return false
		}
	}
	for _, key := range pair {
		if _, ok := f.preloadFloatConst(storage{kind: stConst, typ: key.typ, cval: key.bits}); !ok {
			return false
		}
	}
	return true
}

// Evaluate bounded pure valent trees at the two values of one local's low bit.
// Only borrowed local reads inside exact &1 leaves participate; captured older
// reads and effectful/nonlocal nodes fall back to ordinary conversion.
type booleanFloatTree struct {
	f           *fn
	budget      int
	variable    uint32
	hasVariable bool
	source      *elem
}

func (f *fn) inspectBooleanFloatTree(root *elem) (values [2]uint32, source *elem, ok bool) {
	walk := booleanFloatTree{f: f, budget: 32}
	values, ok = walk.walk(root, 0)
	return values, walk.source, ok && walk.hasVariable
}

func (w *booleanFloatTree) walk(e *elem, depth int) ([2]uint32, bool) {
	if e == nil || w.budget == 0 || depth > 16 || e.st.typ != mtI32 {
		return [2]uint32{}, false
	}
	w.budget--
	if e.elemKind() == ekValue && e.st.kind == stConst {
		v := uint32(e.st.cval)
		return [2]uint32{v, v}, true
	}
	if !e.isDeferred() || !shared.BooleanFloatBinary(shared.IntOp(e.deferredOp())) {
		return [2]uint32{}, false
	}
	left, right := w.f.s.arg0(e), w.f.s.arg1(e)
	if e.deferredOp() == opAnd {
		local, mask := left, right
		if local.elemKind() == ekValue && local.st.kind == stConst {
			local, mask = mask, local
		}
		if mask.elemKind() == ekValue && mask.st.kind == stConst && uint32(mask.st.cval) == 1 && local.elemKind() == ekValue && (local.st.kind == stLocalReg || local.st.kind == stLocalRef) {
			if w.hasVariable && w.variable != local.st.idx {
				return [2]uint32{}, false
			}
			w.variable, w.hasVariable = local.st.idx, true
			w.source = local
			return [2]uint32{0, 1}, true
		}
	}
	a, ok := w.walk(left, depth+1)
	if !ok {
		return [2]uint32{}, false
	}
	b, ok := w.walk(right, depth+1)
	if !ok {
		return [2]uint32{}, false
	}
	return [2]uint32{shared.FoldI32(shared.IntOp(e.deferredOp()), a[0], b[0]), shared.FoldI32(shared.IntOp(e.deferredOp()), a[1], b[1])}, true
}

func (f *fn) tryBooleanFloatConversion(f64, signed bool) bool {
	if !f.opt(optBooleanFloatSelect) || len(f.fconsts) != 2 {
		return false
	}
	root := f.s.back()
	if root == nil || !root.isDeferred() {
		return false
	}
	values, source, ok := f.inspectBooleanFloatTree(root)
	if !ok {
		return false
	}
	typ := mtOf2(f64)
	var constants [2]Reg
	for i, v := range values {
		bits := int64(shared.BooleanFloatBits(v, f64, signed))
		reg, found := f.floatConstReg(storage{kind: stConst, typ: typ, cval: bits})
		if !found {
			return false
		}
		constants[i] = reg
	}
	operand, owned := f.materializeRead(source)
	result := f.allocFReg(maskOf(constants[0], constants[1]))
	f.a.TstImm32(operand, 1)
	f.a.Fcsel(result, constants[1], constants[0], a64.CondNE, f64)
	if owned {
		f.release(operand)
	}
	f.consumeBlockBelow(root)
	f.erase(root)
	f.pushFReg(result, typ)
	f.stats.peep("boolean-float-select")
	return true
}
