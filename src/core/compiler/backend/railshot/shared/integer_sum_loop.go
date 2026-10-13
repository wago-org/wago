package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// IntegerSumLoop proves an unsigned unit-stride integer reduction.
// A bounded same-iteration assignment block can also publish final values to
// defined globals, or reduce one additive defined-global recurrence. Effectful
// calls, traps, imported global writes and fences reject these proofs. The backend separately proves that Counter starts at zero.
// The do-while body sums Coefficient*i+Constant, or the runtime global value,
// or an affine term in the current value of the additive recurrence;
// A PopulationCount term instead sums the counter's set bits, optionally
// after constant rotations. A Divisor term includes linear combinations of
// unsigned counter quotients/remainders by one nonzero constant. Every proof
// leaves Counter=max(1,Bound).
// Fences may be coalesced because no memory access, call, trap or escape lies
// between them. The replacement must still emit one fence when Fenced is true.
type IntegerSumLoop struct {
	Counter, Accumulator, Bound                        uint32
	End                                                int
	Wide, Fenced                                       bool
	Calls                                              uint8
	InvariantGlobal                                    bool
	GlobalRecurrence                                   bool
	GlobalCoefficient                                  uint64
	IncrementCoefficient                               uint64
	IncrementConstant                                  uint64
	Global                                             uint32
	Coefficient, Constant                              uint64
	Periodic                                           bool
	SignedBits                                         uint8
	NegativeCount                                      bool
	PopulationCount                                    bool
	Divisor, QuotientCoefficient, RemainderCoefficient uint64
	EvenCoefficient, EvenConstant                      uint64
	OddCoefficient, OddConstant                        uint64
	GlobalWrites                                       [4]IntegerAffineGlobalWrite
	GlobalWriteCount                                   uint8
}

// InspectIntegerSumLoop does not advance the source reader on either outcome.
// Recognition is bounded: a fixed grammar and at most four adjacent fences at
// each admitted position, with no recursive control or expression traversal.
func InspectIntegerSumLoop(source *wasm.Reader) (IntegerSumLoop, bool) {
	return InspectIntegerSumLoopForModule(source, nil, false)
}

// InspectIntegerSumLoopForModule additionally exposes integer getter candidates
// and bounded pure unary helpers when allowCalls is true. The backend must
// prove a mutable getter is already value-pinned. A nil module retains
// the call-free arithmetic proof. Helpers cannot call helpers.
func InspectIntegerSumLoopForModule(source *wasm.Reader, module *wasm.Module, allowCalls bool) (IntegerSumLoop, bool) {
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
	termStart := r
	term, ok := inspectIntegerAffineTermForModule(&r, blockType != 0, module, allowCalls)
	if !ok && blockType != 0 {
		r = termStart
		width := 4
		if blockType == 0x7e {
			width = 8
		}
		term, ok = inspectIntegerMixedWidthTerm(&r, module, width)
		if !ok {
			r = termStart
			term, ok = inspectIntegerGlobalRecurrence(&r, module, width)
		}
	}
	if !ok && blockType == 0 {
		r = termStart
		var periodic IntegerSumLoop
		term, periodic, ok = inspectIntegerParityTerm(&r)
		p.Periodic = periodic.Periodic
		p.EvenCoefficient, p.EvenConstant = periodic.EvenCoefficient, periodic.EvenConstant
		p.OddCoefficient, p.OddConstant = periodic.OddCoefficient, periodic.OddConstant
	}
	if !ok && blockType == 0 {
		r = termStart
		term, ok = inspectIntegerSignedTerm(&r)
	}
	if !ok && blockType == 0 {
		r = termStart
		term, ok = inspectIntegerPopcountTerm(&r)
	}
	if !ok && blockType == 0 {
		r = termStart
		term, ok = inspectIntegerDivisionTerm(&r)
	}
	if !ok {
		return p, false
	}
	p.Divisor, p.QuotientCoefficient, p.RemainderCoefficient = term.divisor, term.quotientCoefficient, term.remainderCoefficient
	p.PopulationCount = term.populationCount
	p.Coefficient, p.Constant, p.Calls = term.coefficient, term.constant, term.calls
	p.InvariantGlobal, p.Global = term.hasGlobal, term.global
	p.GlobalRecurrence, p.GlobalCoefficient = term.recurrence, term.globalCoefficient
	p.IncrementCoefficient, p.IncrementConstant = term.incrementCoefficient, term.incrementConstant
	p.SignedBits, p.NegativeCount = term.signedBits, term.negativeCount
	p.GlobalWrites, p.GlobalWriteCount = term.writes, term.writeCount
	// Preserve the existing fence proof: no memory reads between fences.
	if (p.InvariantGlobal || p.GlobalWriteCount != 0 || p.GlobalRecurrence) && p.Fenced {
		return p, false
	}
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
