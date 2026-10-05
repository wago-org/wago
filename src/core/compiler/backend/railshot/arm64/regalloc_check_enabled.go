//go:build arm64 && wago_regalloccheck

package arm64

import (
	"fmt"

	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

const regallocCheckEnabled = true

// Transfer regions trust incoming locations and observe only covered transfers.
// Immutable GP reservations observe all typed encoder writes; FP cache
// admission is checked at calls. Neither state is whole-function dataflow.
type regallocFnState struct {
	sourceLeaf         *shared.SourceLeaf
	sourceBranch       *shared.SourceBranch
	sourceRestore      func()
	allocationCheck    *allocationRegion
	immutableCheck     regalloccheck.State
	immutableValues    []allocationGoal
	immutableGPMask    uint32
	gpObserverActive   bool
	gpObserverPrevious func(uint32)
}
type allocationGoal struct {
	loc   regalloccheck.Location
	value regalloccheck.Value
}
type allocationRegion struct {
	state     regalloccheck.State
	values    map[*elem]regalloccheck.Value
	protected map[*elem]bool
	goals     []allocationGoal
	previous  func(regalloccheck.Effect)
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
		f.checkSeed(f.s.arg0(e))
		f.checkSeed(f.s.arg1(e))
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
	for e := f.s.next(f.s.head); e != f.s.head; e = f.s.next(e) {
		f.checkSeed(e)
	}
	slot := 0
	for _, e := range roots {
		c.goals = append(c.goals, allocationGoal{regalloccheck.Slot(f.spillOff(slot)), c.values[e]})
		slot += e.st.typ.stackSlots()
	}
	// A partial flush must also preserve the condition/argument suffix.
	c.protected = make(map[*elem]bool)
	first := f.s.next(f.s.head)
	if len(roots) != 0 {
		first = f.s.next(roots[len(roots)-1])
	}
	for e := first; e != f.s.head; e = f.s.next(e) {
		c.protected[e] = true
	}
	c.previous = f.a.ObserveRegalloc(c.observe)
	return true
}

// Nested transfer windows compose with a function-wide emission observer.
func (c *allocationRegion) observe(effect regalloccheck.Effect) {
	c.state.Apply(effect)
	if c.previous != nil {
		c.previous(effect)
	}
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
	for _, goal := range c.goals {
		c.state.Expect("canonical stack at join/call", goal.loc, goal.value)
	}

	for e := f.s.next(f.s.head); e != f.s.head; e = f.s.next(e) {
		if c.protected[e] {
			if loc, ok := f.checkLocation(e); ok {
				c.state.Expect("live suffix after partial flush", loc, c.values[e])
			}
		}
	}
}

// Check concrete leaves before condensation consumes or rewrites the deferred
// tree. The result identity must not hide a corrupted input.
func (f *fn) checkInputs(e *elem) {
	if f.allocationCheck == nil || e == nil {
		return
	}
	if e.isDeferred() {
		f.checkInputs(f.s.arg0(e))
		f.checkInputs(f.s.arg1(e))
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
// They have no spill/reload protocol and must survive until function exit.
func (f *fn) checkImmutable(reg Reg, fp bool, size int) {
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
	}
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

// checkHostSyncHomes verifies the precondition for a future selective bridge.
// It does not certify implicit context/global registers or narrow preservation.
func (f *fn) checkHostSyncHomes() {
	slot := 0
	for _, root := range f.rootsBottomToTop() {
		if root.elemKind() != ekValue || root.st.kind != stSlot || root.st.slotIndex() != slot {
			panic(fmt.Sprintf("regalloccheck: host sync operand lacks canonical home at slot %d", slot))
		}
		slot += root.st.typ.stackSlots()
	}
	if f.usesCalls {
		for i, local := range f.locals {
			if local.reg != regNone && local.state == lsReg {
				panic(fmt.Sprintf("regalloccheck: host sync dirty pinned local %d", i))
			}
		}
	}
	if len(f.fconsts) != 0 || len(f.vconsts) != 0 {
		panic("regalloccheck: host sync persistent constant cache")
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
	f.immutableGPMask = 0
	checkSourceClose(f)
}

// Trap stubs are terminal edges: they unwind directly to Go and cannot return
// to a cache use. Restore the body reservation for other emitted paths.
type regallocGPWriteMask = uint32

func (f *fn) checkTerminalGPWrites() regallocGPWriteMask {
	saved := f.immutableGPMask
	f.immutableGPMask = 0
	return saved
}
func (f *fn) checkRestoreGPWrites(saved regallocGPWriteMask) { f.immutableGPMask = saved }
