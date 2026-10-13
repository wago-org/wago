package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// IntegerSumLoop proves a side-effect-free, unsigned, unit-stride induction
// reduction of an affine integer term. The backend separately proves that Counter starts at zero.
// The do-while body sums Coefficient*i+Constant and leaves Counter=max(1,Bound).
// Fences may be coalesced because no memory access, call, trap or escape lies
// between them. The replacement must still emit one fence when Fenced is true.
type IntegerSumLoop struct {
	Counter, Accumulator, Bound uint32
	End                         int
	Wide, Fenced                bool
	Coefficient, Constant       uint64
}

// InspectIntegerSumLoop does not advance the source reader on either outcome.
// Recognition is bounded: a fixed grammar and at most four adjacent fences at
// each admitted position, with no recursive control or expression traversal.
func InspectIntegerSumLoop(source *wasm.Reader) (IntegerSumLoop, bool) {
	r := *source
	var p IntegerSumLoop
	byteIs := func(want byte) bool { got, err := r.Byte(); return err == nil && got == want }
	local := func(op byte) (uint32, bool) {
		if !byteIs(op) {
			return 0, false
		}
		x, err := r.U32()
		return x, err == nil
	}
	fences := func() bool {
		for n := 0; n < 4; n++ {
			op, ok := r.Peek()
			if !ok || op != 0xfe {
				return true
			}
			_, _ = r.Byte()
			sub, err := r.U32()
			if err != nil || sub != 3 || !byteIs(0) {
				return false
			}
			p.Fenced = true
		}
		op, _ := r.Peek()
		return op != 0xfe
	}
	if !byteIs(0x40) || !fences() {
		return p, false
	}
	var ok bool
	if p.Accumulator, ok = local(0x20); !ok {
		return p, false
	}
	blockType := byte(0)
	if op, _ := r.Peek(); op == 0x02 {
		_, _ = r.Byte()
		var err error
		blockType, err = r.Byte()
		if err != nil || (blockType != 0x7f && blockType != 0x7e) || !fences() {
			return p, false
		}
	}
	term, ok := inspectIntegerAffineTerm(&r, blockType != 0)
	if !ok {
		return p, false
	}
	p.Coefficient, p.Constant = term.coefficient, term.constant
	if blockType != 0 && !byteIs(0x0b) {
		return p, false
	}
	add, err := r.Byte()
	if err != nil || (add != 0x6a && add != 0x7c) {
		return p, false
	}
	p.Wide = add == 0x7c
	if term.width != 0 && (term.width == 8) != p.Wide {
		return p, false
	}
	constant, less, valueType := byte(0x41), byte(0x49), byte(0x7f)
	if p.Wide {
		constant, less, valueType = 0x42, 0x54, 0x7e
	}
	if blockType != 0 && blockType != valueType {
		return p, false
	}
	if x, ok := local(0x21); !ok || x != p.Accumulator {
		return p, false
	}
	if p.Counter, ok = local(0x20); !ok || term.hasVariable && term.variable != p.Counter {
		return p, false
	}
	if !byteIs(constant) {
		return p, false
	}
	var one int64
	var constErr error
	if p.Wide {
		one, constErr = r.I64()
	} else {
		var v int32
		v, constErr = r.I32()
		one = int64(v)
	}
	if constErr != nil || one != 1 {
		return p, false
	}
	if !byteIs(add) {
		return p, false
	}
	if x, ok := local(0x21); !ok || x != p.Counter {
		return p, false
	}
	if x, ok := local(0x20); !ok || x != p.Counter {
		return p, false
	}
	if p.Bound, ok = local(0x20); !ok || !byteIs(less) || !byteIs(0x0d) {
		return p, false
	}
	label, err := r.U32()
	if err != nil || label != 0 || !byteIs(0x0b) {
		return p, false
	}
	if p.Counter == p.Accumulator || p.Counter == p.Bound || p.Accumulator == p.Bound {
		return p, false
	}
	p.End = r.Offset()
	return p, true
}

// IntegerSumCounterUntouched proves the zero initialization from original source,
// rather than emitter write history, which can omit fused local assignments.
// Only successful reduction candidates pay this bounded prefix walk.
func IntegerSumCounterUntouched(source *wasm.Reader, counter uint32, classifier wasm.ModuleInstructionClassifier) bool {
	end := source.Offset() - 1 // source is immediately after the loop opcode
	if end < 0 || end > 256 {
		return false
	}
	r := *source
	if r.JumpTo(0) != nil {
		return false
	}
	var imm wasm.InstructionImmediate
	for r.Offset() < end {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if err := classifier.ClassifyInto(&r, op, &imm); err != nil || r.Offset() > end {
			return false
		}
		if (op == 0x21 || op == 0x22) && imm.Index == counter {
			return false
		}
	}
	return r.Offset() == end
}
