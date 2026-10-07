//go:build amd64 && wago_regalloccheck

package amd64

import (
	"fmt"

	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

const regallocCheckEnabled = true

// Transfer regions trust incoming locations and observe only covered transfers.
// Immutable GP and low-128 FP reservations observe typed encoder writes.
// Neither state is whole-function dataflow; raw emission is outside this channel.
type regallocFnState struct {
	sourcePlan         *nativeSourcePlanState
	sourceLeaf         *shared.SourceLeaf
	sourceBranch       *shared.SourceBranch
	sourceRestore      func()
	allocationCheck    *allocationRegion
	immutableCheck     regalloccheck.State
	immutableValues    []allocationGoal
	immutableGPMask    uint32
	immutableFPMask    uint32
	fpObserverActive   bool
	fpObserverPrevious func(uint32)
	gpObserverActive   bool
	gpObserverPrevious func(uint32)
}
type allocationGoal struct {
	loc   regalloccheck.Location
	value regalloccheck.Value
}
type allocationRegion struct {
	state       regalloccheck.State
	values      map[*elem]regalloccheck.Value
	protected   map[*elem]bool
	goals       []allocationGoal
	previous    func(regalloccheck.Effect)
	pendingRead regalloccheck.Value
}

func checkSize(typ machineType) int {
	switch typ {
	case mtI32, mtF32:
		return 4
	case mtV128:
		return 16
	default:
		return 8
	}
}
func checkReg(reg Reg, fp bool) regalloccheck.Location {
	bank := regalloccheck.GP
	if fp {
		bank = regalloccheck.FP
	}
	return regalloccheck.Register(bank, uint8(reg))
}

// Only register and frame homes provide input facts. Constants, deferred
// results and non-frame loads rely on trusted semantic definitions instead.
func (f *fn) checkLocation(e *elem) (regalloccheck.Location, bool) {
	if e.isDeferred() {
		return regalloccheck.Location{}, false
	}
	switch e.st.kind {
	case stReg, stLocalReg, stGlobReg:
		return checkReg(e.st.reg, e.st.typ.isXMM()), true
	case stSlot:
		return regalloccheck.Slot(f.spillOff(e.st.slotIndex())), true
	case stLocalRef:
		return regalloccheck.Slot(f.localOff(e.st.index())), true
	}
	return regalloccheck.Location{}, false
}
func (f *fn) checkSeed(e *elem) {
	if e == nil {
		return
	}
	c := f.allocationCheck
	if _, ok := c.values[e]; ok {
		return
	}
	size := checkSize(e.st.typ)
	// Deferred comparisons carry the operand width until condensation, but
	// their result is an i32 even when both inputs are i64.
	if e.isDeferred() && (isCompare(e.deferredOp()) || e.deferredOp() == opEqz) {
		size = 4
	}
	if loc, ok := f.checkLocation(e); ok {
		c.values[e] = c.state.Seed(loc, size)
	} else {
		c.values[e] = c.state.Fresh(size)
	}
	if e.isDeferred() {
		f.checkSeed(e.arg0)
		f.checkSeed(e.arg1)
	}
}

// checkBeginFlush trusts the incoming value locations, then checks the whole
// canonicalization without reseeding after spills, reloads or slot overwrites.
// Arithmetic definitions are trusted; transfer effects come from the encoders.
func (f *fn) checkBeginFlush(roots []*elem) bool {
	if f.allocationCheck != nil {
		panic("regalloccheck: nested canonicalization")
	}
	c := &allocationRegion{values: make(map[*elem]regalloccheck.Value)}
	f.allocationCheck = c
	for _, e := range roots {
		f.checkSeed(e)
	}
	// Values above a flushed prefix can be evicted while its roots materialize.
	for e := f.s.head.next; e != f.s.head; e = e.next {
		f.checkSeed(e)
	}
	slot := 0
	for _, e := range roots {
		c.goals = append(c.goals, allocationGoal{regalloccheck.Slot(f.spillOff(slot)), c.values[e]})
		slot += e.st.typ.stackSlots()
	}
	// A partial flush must also preserve the condition/argument suffix.
	c.protected = make(map[*elem]bool)
	first := f.s.head.next
	if len(roots) != 0 {
		first = roots[len(roots)-1].next
	}
	for e := first; e != f.s.head; e = e.next {
		c.protected[e] = true
	}
	c.previous = f.a.ObserveRegalloc(c.observe)
	return true
}

// Restore the enclosing observer on every exit. Preserve an existing panic
// rather than checking incomplete emission; otherwise verify the final image
// and every protected suffix value still present on the physical stack.
func (f *fn) checkEndFlush() {
	c := f.allocationCheck
	f.a.ObserveRegalloc(c.previous)
	f.allocationCheck = nil
	if failure := recover(); failure != nil {
		panic(failure)
	}
	if c.pendingRead != nil {
		panic("regalloccheck: missing folded machine input at window exit")
	}
	for _, goal := range c.goals {
		c.state.Expect("canonical stack at join/call", goal.loc, goal.value)
	}

	for e := f.s.head.next; e != f.s.head; e = e.next {
		if c.protected[e] {
			if loc, ok := f.checkLocation(e); ok {
				c.state.Expect("live suffix after partial flush", loc, c.values[e])
			}
		}
	}
}

// observe validates a folded read at its actual encoder address before allowing
// a semantic definition to replace the input identities.
func (c *allocationRegion) observe(effect regalloccheck.Effect) {
	if effect.Kind == regalloccheck.Read {
		if c.pendingRead != nil {
			if effect.Size < len(c.pendingRead) {
				panic("regalloccheck: short folded input")
			}
			c.state.Expect("folded machine input", effect.Src, c.pendingRead)
			c.pendingRead = nil
		}
	} else {
		c.state.Apply(effect)
	}
	if c.previous != nil {
		c.previous(effect)
	}
}
func (f *fn) checkFoldedUse(e *elem) {
	c := f.allocationCheck
	if c == nil {
		return
	}
	f.checkUse(e)
	if c.pendingRead != nil {
		panic("regalloccheck: missing folded machine input")
	}
	c.pendingRead = c.values[e]
}

// Check concrete leaves before condensation consumes or rewrites the deferred
// tree. The result identity must not hide a corrupted input.
func (f *fn) checkInputs(e *elem) {
	if e != nil && e.isDeferred() {
		checkNativeSourceProducerBefore(f, e)
	}
	if f.allocationCheck == nil || e == nil {
		return
	}
	if e.isDeferred() {
		f.checkInputs(e.arg0)
		f.checkInputs(e.arg1)
	} else {
		f.checkUse(e)
	}
}
func (f *fn) checkUse(e *elem) {
	c := f.allocationCheck
	if c == nil {
		return
	}
	value, ok := c.values[e]
	if !ok {
		panic("regalloccheck: unmodeled value in transfer region")
	}
	if loc, concrete := f.checkLocation(e); concrete {
		c.state.Expect(fmt.Sprintf("function %d pc %d materialize input", f.traceFuncIdx, f.wasmPC), loc, value)
	}
}

// Run after emission but before storage or register ownership is rewritten.
// Concrete inputs must already reach the emitted destination; only semantic
// definitions may install an identity.
func (f *fn) checkOccupy(e *elem, reg Reg, fp bool) {
	c := f.allocationCheck
	if c == nil {
		return
	}
	value, ok := c.values[e]
	if !ok {
		panic("regalloccheck: unmodeled definition in transfer region")
	}
	if c.pendingRead != nil {
		panic("regalloccheck: missing folded machine input before definition")
	}
	dst := checkReg(reg, fp)
	if _, concrete := f.checkLocation(e); concrete {
		c.state.Expect(fmt.Sprintf("function %d pc %d materialize transfer", f.traceFuncIdx, f.wasmPC), dst, value)
	} else {
		// Only semantic definitions introduce identities. Reloads and moves cannot.
		c.state.Put(dst, value)
	}
}

// checkBeginSlots verifies an edge's simultaneous slot assignment, including
// overlap. A nested staged-flush copy inherits the existing physical state.
func (f *fn) checkBeginSlots(from, to, n int) func() {
	c := f.allocationCheck
	owned := c == nil
	if owned {
		c = &allocationRegion{}
		c.previous = f.a.ObserveRegalloc(c.observe)
	}
	goals := make([]allocationGoal, n)
	for i := range goals {
		src := regalloccheck.Slot(f.spillOff(from + i))
		var value regalloccheck.Value
		if owned {
			value = c.state.Seed(src, 8)
		} else {
			value = c.state.Read(src, 8)
		}
		// A scalar i32/f32 only promises its low four bytes. Preserve every known
		// byte, rather than inventing identities for unspecified carrier high bits.
		goals[i] = allocationGoal{regalloccheck.Slot(f.spillOff(to + i)), value}
	}
	return func() {
		if owned {
			f.a.ObserveRegalloc(c.previous)
		}
		for _, goal := range goals {
			c.state.ExpectKnown("control-edge slot copy", goal.loc, goal.value)
		}
	}
}

// Immutable caches are defined at their actual preload, not seeded at a call.
// They have no spill/reload protocol and must survive until their cache scope ends.
func (f *fn) checkImmutable(reg Reg, fp bool, size int) {
	if fp && (size > 16 || uint8(reg) >= 16) {
		panic("regalloccheck: unsupported immutable FP reservation")
	}
	loc := checkReg(reg, fp)
	value := f.immutableCheck.Fresh(size)
	f.immutableCheck.Put(loc, value)
	f.immutableValues = append(f.immutableValues, allocationGoal{loc, value})
	if !fp {
		f.immutableGPMask |= uint32(1) << uint8(reg)
		if !f.gpObserverActive && f.a != nil {
			f.gpObserverActive = true
			f.gpObserverPrevious = f.a.ObserveGPWrites(func(mask uint32) {
				if mask&f.immutableGPMask != 0 {
					panic(fmt.Sprintf("regalloccheck: function %d pc %d: immutable GP reservation overwritten (mask %#x)", f.traceFuncIdx, f.wasmPC, mask&f.immutableGPMask))
				}
				if f.gpObserverPrevious != nil {
					f.gpObserverPrevious(mask)
				}
			})
		}
	} else {
		f.immutableFPMask |= uint32(1) << uint8(reg)
		if !f.fpObserverActive && f.a != nil {
			f.fpObserverActive = true
			f.fpObserverPrevious = f.a.ObserveFPWrites(func(mask uint32) {
				if mask&f.immutableFPMask != 0 {
					panic(fmt.Sprintf("regalloccheck: function %d pc %d: immutable FP reservation may be overwritten (mask %#x)", f.traceFuncIdx, f.wasmPC, mask&f.immutableFPMask))
				}
				if f.fpObserverPrevious != nil {
					f.fpObserverPrevious(mask)
				}
			})
		}
	}
}

// A loop-scoped cache ceases to reserve its register at the loop's lexical end.
// Retire only that location's expectation; outer caches retain their original
// identities, and calls emitted before this boundary still reject clobbers.
func (f *fn) checkReleaseImmutable(reg Reg, fp bool) {
	loc := checkReg(reg, fp)
	for i, goal := range f.immutableValues {
		if goal.loc == loc {
			if !fp {
				f.immutableGPMask &^= uint32(1) << uint8(reg)
			} else {
				f.immutableFPMask &^= uint32(1) << uint8(reg)
			}
			copy(f.immutableValues[i:], f.immutableValues[i+1:])
			f.immutableValues[len(f.immutableValues)-1] = allocationGoal{}
			f.immutableValues = f.immutableValues[:len(f.immutableValues)-1]
			return
		}
	}
	panic("regalloccheck: retiring an unknown immutable cache")
}

// Invoke at every physical call in a cache-bearing function, including helper
// and alternate paths. Call-presence hints cannot prove preservation: this
// rejects bad cache admission, not arbitrary non-call register clobbers.
func (f *fn) checkCallClobber() {
	f.immutableCheck.Apply(regalloccheck.Effect{Kind: regalloccheck.Call})
	for _, goal := range f.immutableValues {
		f.immutableCheck.Expect(fmt.Sprintf("function %d pc %d: immutable cache across physical call", f.traceFuncIdx, f.wasmPC), goal.loc, goal.value)
	}
}

// checkBeginRegMoves snapshots the original parallel assignment, then observes
// the actual encoder transfers. Requested resolver operations never update state.
// Close immediately after the shuffle, before argument materialization or calls;
// also defer the idempotent closer to restore the observer on failure.
func (f *fn) checkBeginRegMoves(moves []regMove, fp bool) func() {
	var state regalloccheck.State
	for _, m := range moves {
		state.Seed(checkReg(m.src, fp), 8)
	}
	goals := make([]allocationGoal, len(moves))
	for i, m := range moves {
		goals[i] = allocationGoal{checkReg(m.dst, fp), state.Read(checkReg(m.src, fp), 8)}
	}
	var previous func(regalloccheck.Effect)
	previous = f.a.ObserveRegalloc(func(effect regalloccheck.Effect) {
		state.Apply(effect)
		if previous != nil {
			previous(effect)
		}
	})
	closed := false
	return func() {
		if closed {
			return
		}
		closed = true
		f.a.ObserveRegalloc(previous)
		if failure := recover(); failure != nil {
			panic(failure)
		}
		for _, goal := range goals {
			state.Expect("parallel ABI move", goal.loc, goal.value)
		}
	}
}

// A reservation forbids every physical write until its lifetime ends. This
// path-independent rule cannot be repaired by copying a value from another arm.
func (f *fn) checkEndLifetimes() {
	if f.gpObserverActive {
		f.a.ObserveGPWrites(f.gpObserverPrevious)
		f.gpObserverPrevious = nil
		f.gpObserverActive = false
	}
	if f.fpObserverActive {
		f.a.ObserveFPWrites(f.fpObserverPrevious)
		f.fpObserverPrevious = nil
		f.fpObserverActive = false
	}
	f.immutableGPMask = 0
	f.immutableFPMask = 0
	checkSourceClose(f)
}

// Returns and trap stubs are terminal edges and cannot reach a body-cache use.
// Restore body reservations before emitting other control-flow arms.
type regallocWriteMask struct{ gp, fp uint32 }

func (f *fn) checkTerminalWrites() regallocWriteMask {
	saved := regallocWriteMask{f.immutableGPMask, f.immutableFPMask}
	f.immutableGPMask = 0
	f.immutableFPMask = 0
	return saved
}
func (f *fn) checkRestoreWrites(saved regallocWriteMask) {
	f.immutableGPMask, f.immutableFPMask = saved.gp, saved.fp
}
