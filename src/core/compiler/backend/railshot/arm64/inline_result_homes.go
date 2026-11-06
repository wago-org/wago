//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

type inlineResultHomes struct {
	locals [2]int
	regs   [2]Reg
	active bool
}

func (h inlineResultHomes) restore(f *fn) {
	if h.active {
		for i, local := range h.locals {
			f.locals[local].reg = h.regs[i]
		}
	}
}

// Immediate result assignments make the old caller values dead once all older
// reads are realized. A pure, fallthrough-only splice may then compute its final
// local values in the caller's homes. Keep the old private pins reserved until
// every remaining private reference has been realized by inlineCall.
func (f *fn) coalesceInlineTupleResultHomes(t *inlineTarget, stores tupleStoreHint) inlineResultHomes {
	var plan inlineResultHomes
	if !t.tupleControl || f.makesCalls || f.usesCalls || f.preserveCallerPins || f.interruptible || f.inlineDepth != 0 ||
		stores.locals[0] < 0 || stores.locals[1] < 0 || !f.inlineTupleFallsThrough(t) {
		return plan
	}
	type instruction struct {
		op    byte
		index uint32
	}
	var tail [2]instruction
	r := wasm.NewReader(t.body)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return plan
		}
		if op == 0x0b && !r.HasNext() {
			break
		}
		if f.classifier.ClassifyInto(r, op, &imm) != nil {
			return plan
		}
		tail[0], tail[1] = tail[1], instruction{op, imm.Index}
	}
	if tail[0].op != 0x20 || tail[1].op != 0x20 || tail[0].index == tail[1].index {
		return plan
	}
	base := f.inlineBase[t.globalIdx]
	types := f.inlineTargets.localTypes(t)
	results := f.inlineTargets.resultTypes(t)
	if len(results) != 2 || base < f.nLocals || base > len(f.locals)-len(types) {
		return plan
	}
	var targets [2]Reg
	for i, source := range tail {
		x := stores.locals[i]
		if uint64(source.index) >= uint64(len(types)) || x >= f.nLocals ||
			types[source.index] != results[i] || f.localType[x] != results[i] {
			return inlineResultHomes{}
		}
		local := base + int(source.index)
		old, floating, _ := f.pinReg(local)
		reg, targetFloat, targetPinned := f.pinReg(x)
		if floating || !targetPinned || targetFloat || f.locals[x].state == lsMem {
			return inlineResultHomes{}
		}
		plan.locals[i], plan.regs[i], targets[i] = local, old, reg
	}
	if targets[0] == targets[1] {
		return inlineResultHomes{}
	}
	for i, local := range plan.locals {
		f.realizeLocalRefs(local, nil)
		f.realizeLocalRefs(stores.locals[i], nil)
	}
	for i, local := range plan.locals {
		f.locals[local].reg = targets[i]
		// Private loop entry must not rematerialize the caller's former zero
		// over the new parameter or declared-local value in this shared home.
		f.locals[stores.locals[i]].state = lsReg
	}
	plan.active = true
	f.stats.peep("inline-tuple-result-homes")
	return plan
}
