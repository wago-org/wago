package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Bulk memory ops: memory.copy (memmove) and memory.fill (memset), lowered to
// rep movsb / rep stosb. Both bounds-check the whole range up front and trap
// (MemOOB) before writing anything, matching wasm semantics.
//
// rep movs/stos require their operands in fixed registers (RDI/RSI/RCX, and AL
// for stos). To set those up without register-assignment hazards, the three
// i32 operands are first spilled to free frame slots and reloaded into the
// fixed registers — the rep then dominates, so the spill is negligible. RDI and
// RSI are never used by the value-stack allocator, so only RCX/RAX (which are)
// need ensureFree before use.

// spill3 pops the three i32 operands and stores them to three free frame slots,
// returning the slot indices. Net stack effect is -3 (these ops push nothing).
func (g *cg) spill3() (s0, s1, s2 int) {
	c := g.pop()
	b := g.pop()
	a := g.pop()
	base := len(g.st) // first slot above the new stack top
	if base+3 > g.maxDepth {
		g.maxDepth = base + 3
	}
	for i, e := range []ventry{a, b, c} {
		r := g.materialize(e)
		g.a.Store64(RBP, g.slotOff(base+i), r)
		g.freeReg(r)
	}
	return base, base + 1, base + 2
}

// trapUnless emits `cmp t, mb; jbe ok; trap; ok:` — trap when t > mb (out of bounds).
func (g *cg) trapUnlessLE(t, mb Reg) {
	g.a.Cmp64(t, mb)
	ok := g.a.JccPlaceholder(CondBE)
	g.emitTrap(trapMemOOB)
	g.a.PatchRel32(ok, g.a.Len())
}

// memoryCopy lowers memory.copy with memmove semantics (handles overlap).
func (g *cg) memoryCopy(r *wasm.Reader) error {
	if _, err := r.U32(); err != nil { // dst memidx (only memory 0)
		return err
	}
	if _, err := r.U32(); err != nil { // src memidx
		return err
	}
	dstS, srcS, nS := g.spill3()

	g.a.Load64(RDI, RBP, g.slotOff(dstS)) // RDI = dst offset
	g.a.Load64(RSI, RBP, g.slotOff(srcS)) // RSI = src offset
	g.ensureFree(RCX)
	g.a.Load64(RCX, RBP, g.slotOff(nS)) // RCX = n
	g.busy[RCX] = true

	lm := g.allocReg()
	g.a.Load64(lm, RBP, -16) // linear memory base
	mb := g.allocReg()
	g.a.Load32(mb, lm, -8) // memory size in bytes
	t := g.allocReg()
	g.a.LeaScaled(t, RDI, RCX, 0, 0) // dst + n
	g.trapUnlessLE(t, mb)
	g.a.LeaScaled(t, RSI, RCX, 0, 0) // src + n
	g.trapUnlessLE(t, mb)
	g.freeReg(t)
	g.freeReg(mb)

	g.a.Add64(RDI, lm) // RDI = base + dst
	g.a.Add64(RSI, lm) // RSI = base + src
	g.freeReg(lm)

	// memmove: copy forward when dst <= src, else backward, so overlapping
	// ranges are copied as if through a temporary.
	g.a.Cmp64(RDI, RSI)
	fwd := g.a.JccPlaceholder(CondBE)
	g.a.LeaScaled(RDI, RDI, RCX, 0, -1) // last dst byte
	g.a.LeaScaled(RSI, RSI, RCX, 0, -1) // last src byte
	g.a.Std()
	g.a.RepMovsb()
	g.a.Cld()
	done := g.a.JmpPlaceholder()
	g.a.PatchRel32(fwd, g.a.Len())
	g.a.RepMovsb() // forward (DF=0 by ABI)
	g.a.PatchRel32(done, g.a.Len())

	g.freeReg(RCX)
	return nil
}

// memoryFill lowers memory.fill (memset of the low byte of val).
func (g *cg) memoryFill(r *wasm.Reader) error {
	if _, err := r.U32(); err != nil { // memidx
		return err
	}
	dstS, valS, nS := g.spill3()

	g.a.Load64(RDI, RBP, g.slotOff(dstS)) // RDI = dst offset
	g.ensureFree(RAX)
	g.a.Load64(RAX, RBP, g.slotOff(valS)) // AL = fill byte
	g.busy[RAX] = true
	g.ensureFree(RCX)
	g.a.Load64(RCX, RBP, g.slotOff(nS)) // RCX = n
	g.busy[RCX] = true

	lm := g.allocReg()
	g.a.Load64(lm, RBP, -16)
	mb := g.allocReg()
	g.a.Load32(mb, lm, -8)
	t := g.allocReg()
	g.a.LeaScaled(t, RDI, RCX, 0, 0) // dst + n
	g.trapUnlessLE(t, mb)
	g.freeReg(t)
	g.freeReg(mb)

	g.a.Add64(RDI, lm) // RDI = base + dst
	g.freeReg(lm)
	g.a.RepStosb() // [RDI..] = AL, RCX times (DF=0)

	g.freeReg(RAX)
	g.freeReg(RCX)
	return nil
}
