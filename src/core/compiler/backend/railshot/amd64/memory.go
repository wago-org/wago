//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Linear-memory access: scalar loads/stores with a linear bounds check, plus
// memory.size/grow. Ported from WARP's memory lowering, adapted to wago's runtime
// memory ABI (the same one src/core/encoder/amd64 targets): the linear-memory base is
// pinned in RBX for the whole function, and a small "basedata" header sits at
// negative offsets from that base.

// Trap codes — must match jit.TrapCode / the values the engine reads (identical
// to src/core/encoder/amd64's table).
const (
	trapUnreachable   = 1
	trapMemOOB        = 3
	trapIndirectOOB   = 5
	trapIndirectSig   = 6
	trapDivZero       = 9
	trapDivOverflow   = 10
	trapTruncOverflow = 11
	trapInterrupted   = 12
	trapStackFence    = 13
)

// Basedata fields at negative offsets from the linMem base (runtime/basedata.go).
const (
	bdCurPages  = 4  // u32: current size in 64 KiB pages
	bdCurBytes  = 8  // u32: current size in bytes (the bounds-check limit)
	bdMaxPages  = 12 // u32: grow ceiling in pages
	wasmPageLog = 16 // log2(65536)
)

// offTrapStackReentry is the linMem-relative slot (bytes below the linMem base)
// where the trampoline stashes the entry SP for handler-jump trap unwinding —
// see runtime/basedata.go offTrapStackReentry.
const offTrapStackReentry = 24

// smallBulkMax is the dynamic memory.copy/fill length below which the inline
// chunk loops beat `rep movs/stos` startup latency.
const smallBulkMax = 96

// offTrapCellPtr is the basedata slot holding the address of the trap cell
// (runtime installTrapCell / abi.TrapCellPtrOffset). The trap pointer is NOT
// part of any call ABI: only the cold trap path reads it, so calls and returns
// carry no trap protocol (WARP's model — its passive mode has no trap cell).
const offTrapCellPtr = abi.TrapCellPtrOffset

// offPassiveDataPtr points at the per-instance passive data descriptor array.
// Descriptors are runtime.PassiveDataDescBytes bytes: {ptr u64, len u32, pad u32}.
const offPassiveDataPtr = abi.PassiveDataPtrOffset

// emitTrap writes the trap code to the trap cell (via [linMem-offTrapCellPtr])
// then unwinds the
// ENTIRE native call tree in one jump: it restores RSP to the entry SP the
// trampoline recorded at [linMem-offTrapStackReentry] and RETs straight back into
// enterNative (WARP's handler-jump model). This is what lets callers skip the
// per-call "load *trap; test; branch" check — a trap never returns through an
// intermediate frame. Terminal, so it may freely clobber RSI (and RSP last).
func (f *fn) emitTrap(code uint32) {
	f.a.Load64(RSI, RBX, -offTrapCellPtr)
	f.a.StoreImm32Mem(RSI, 0, int32(code))
	f.a.Load64(RSP, RBX, -offTrapStackReentry) // rsp = entry SP (trampoline's post-CALL SP)
	f.a.Ret()                                  // pop enterNative's return address → back to Go
}

// emitInterruptCheck polls the invocation trap cell at bounded native safe
// points (function entries and loop headers). A context watcher writes
// TrapInterrupted there; the ordinary cold trap path then unwinds the complete
// native call tree, so a running wasm loop observes cancellation within one
// iteration instead of running to completion. Mirrors arm64's emitInterruptCheck.
//
// scratch must be a register that is free at the call site (the operand stack is
// flushed at loop headers, and entry sites have not yet homed their params). The
// hot (not-interrupted) path falls through; only the pointer load and a
// compare-against-zero touch scratch, so no live value is clobbered.
func (f *fn) emitInterruptCheck(scratch Reg) {
	if !f.interruptible {
		return
	}
	f.a.Load64(scratch, RBX, -offTrapCellPtr) // scratch = &trapCell
	f.a.Load32(scratch, scratch, 0)           // scratch = *trapCell (reuse: pointer no longer needed)
	f.a.TestSelf(scratch, false)              // ZF = (*trapCell == 0)
	f.trapIf(condNE, trapInterrupted)         // nonzero → cold stub writes the code and unwinds
}

// trapIf records a conditional jump to this function's shared trap stub for
// `code` (emitted once, after the body, by emitTrapStubs). Checks branch TO the
// cold stub on failure, so the hot path falls through — instead of jumping over
// a ~20-byte inline trap block at every site (better I-cache, not-taken hot
// branches, one stub per trap code instead of one block per check).
func (f *fn) trapIf(cc Cond, code uint32) {
	if code == trapMemOOB {
		f.stats.addBoundsCheck() // inline linear-memory OOB check (P6 elides these)
	}
	f.sc.trapSites[code] = append(f.sc.trapSites[code], f.a.JccPlaceholder(cc))
}

// trapAlways is trapIf's unconditional form (`unreachable`): a 5-byte jmp to the
// shared stub instead of the inline 20-byte trap block.
func (f *fn) trapAlways(code uint32) {
	f.sc.trapSites[code] = append(f.sc.trapSites[code], f.a.JmpPlaceholder())
}

// emitTrapStubs emits one trap stub per trap code used by this function and
// patches every recorded site to it. Called once, after the epilogue.
func (f *fn) emitTrapStubs() {
	for code := uint32(1); code <= trapStackFence; code++ { // deterministic order
		sites := f.sc.trapSites[code]
		if len(sites) == 0 {
			continue
		}
		f.stats.addTrapStub()
		pos := f.a.Len()
		f.storeModuleGlobals(RSI) // post-trap global state stays observable (RSI is trap-path scratch)
		f.emitTrap(code)
		for _, s := range sites {
			f.a.PatchRel32(s, pos)
		}
	}
}

// memAddr pops the address operand, folds the static memarg offset, emits the
// bounds check (unless guard-page mode elides it), and returns the register
// holding the effective offset plus the displacement to fold into the access.
// aliasPinned lets a pinned-local address be used in place (no copy) — only
// valid when the access is emitted immediately (stores), not deferred (loads);
// eaOwned reports whether the caller must release ea.
func (f *fn) memAddr(off uint32, size int, aliasPinned bool) (ea Reg, eaOwned bool, borrow int, disp int32) {
	e := f.popValue()
	// Bounds-certificate source: the address's stable value carrier (a local or
	// global index), captured before materialization. A temp/computed base has no
	// stable key. See boundsCertMeasure.
	bcKind, bcIdx := uint8(0), uint32(0)
	switch e.st.kind {
	case stLocalReg, stLocalRef:
		bcKind, bcIdx = 1, uint32(e.st.idx)
	case stGlobReg:
		bcKind, bcIdx = 2, uint32(e.st.idx)
	}
	disp = 0
	borrow = -1
	leaDisp := int32(size)
	needAdd := int64(off)+int64(size) > 0x7FFFFFFF && off != 0
	if aliasPinned && !needAdd {
		ea, eaOwned = f.materializeRead(e) // a pinned local's reg is read in place
		if !eaOwned {
			borrow = e.st.idx
		}
	} else {
		ea, eaOwned = f.materialize(e), true // ea = addr (u32, zero-extended)
	}
	if int64(off)+int64(size) <= 0x7FFFFFFF {
		disp = int32(off)
		leaDisp = int32(off) + int32(size)
	} else if off != 0 {
		t := f.allocReg(maskOf(ea))
		f.a.MovImm32(t, int32(off))
		f.a.Add64(ea, t)
		f.release(t)
	}

	if f.guardMode {
		return ea, eaOwned, borrow, disp
	}
	// Loop-precheck fast body: a loop-invariant base local proven in bounds by the
	// pre-loop check needs no per-access check (memBytes only grows). See
	// boundshoist.go.
	if f.elideBases != nil && bcKind == 1 && f.elideBases[bcIdx] {
		f.stats.addBoundsHoistable()
		return ea, eaOwned, borrow, disp
	}
	// P6.1 straight-line bounds-check elision: skip the check when a prior
	// same-source check in this straight-line region already proved this access
	// in-bounds. Sound because linear memory only grows and the certificate is
	// dropped at every flush/flushBelow (all calls + control joins), memory.grow,
	// and a set of the certified source — so the proving check dominates this one
	// on every path. WAGO_NO_BOUNDS_FACTS=1 forces every check (A/B + kill switch).
	if f.boundsFacts && f.boundsCertCovers(bcKind, bcIdx, leaDisp) {
		f.stats.addBoundsElidable()
		return ea, eaOwned, borrow, disp
	}
	f.boundsCertUpdate(bcKind, bcIdx, leaDisp)
	if bcKind != 0 && f.inLoop() {
		f.stats.addBoundsInLoop()
	}
	if f.boundsHoistable(bcKind, bcIdx) {
		f.stats.addBoundsHoistable()
	}
	f.pinned = f.pinned.add(ea)
	t := f.allocReg(0)
	f.a.LeaDisp(t, ea, leaDisp) // t = ea + off + size
	if f.memSizeReg != regNone {
		f.a.Cmp64(t, f.memSizeReg) // memBytes lives in a register (WARP REGS::memSize)
	} else {
		mb := f.allocReg(maskOf(t))
		f.a.Load32(mb, RBX, -bdCurBytes) // memory size in bytes
		f.a.Cmp64(t, mb)
		f.release(mb)
	}
	f.trapIf(condA, trapMemOOB) // out of bounds when ea+off+size > memBytes
	f.release(t)
	f.pinned = f.pinned.remove(ea)
	return ea, eaOwned, borrow, disp
}

// boundsCertCovers reports whether the active straight-line certificate already
// proves this access in-bounds (P6.1): the same keyable source, with this
// access's extent (off+size) within the proven extent. A check proves
// source+extent <= memBytes; memBytes only ever grows, so a later access on the
// same source value with a smaller-or-equal extent is in bounds. The certificate
// is dropped by invalidateBoundsCert at flush/flushBelow (every call + control
// join), memory.grow, and a set of the certified source — exactly the set that
// makes the proving check dominate this one on every path, so eliding is sound.
func (f *fn) boundsCertCovers(kind uint8, idx uint32, extent int32) bool {
	return kind != 0 && f.bcKind == kind && f.bcIdx == idx && extent <= f.bcExtent
}

// boundsCertUpdate records the check about to be emitted: establish or extend the
// single-entry certificate for a keyable source; an unkeyable (computed) base
// ends the straight-line certificate.
func (f *fn) boundsCertUpdate(kind uint8, idx uint32, extent int32) {
	if kind == 0 {
		f.bcKind = 0
		return
	}
	if f.bcKind == kind && f.bcIdx == idx {
		if extent > f.bcExtent {
			f.bcExtent = extent // same source, larger reach — extend the proven extent
		}
		return
	}
	f.bcKind, f.bcIdx, f.bcExtent = kind, idx, extent
}

// invalidateBoundsCert drops the straight-line bounds certificate.
func (f *fn) invalidateBoundsCert() { f.bcKind = 0 }

// inLoop reports whether any enclosing control frame is a loop.
func (f *fn) inLoop() bool {
	for i := range f.ctrl {
		if f.ctrl[i].kind == cfLoop {
			return true
		}
	}
	return false
}

// boundsHoistable reports whether a check on address source (kind,idx) is
// hoistable out of its innermost enclosing loop: a LOCAL base that is
// loop-invariant (not set anywhere in that loop, per the loop-header scan).
// Globals are excluded — a callee can change a global but never a caller local.
func (f *fn) boundsHoistable(kind uint8, idx uint32) bool {
	if kind != 1 { // locals only
		return false
	}
	for i := len(f.ctrl) - 1; i >= 0; i-- {
		if f.ctrl[i].kind == cfLoop {
			return !f.ctrl[i].loopSetLocals[idx]
		}
	}
	return false // not inside a loop
}

// memLoad lowers a scalar load of `size` bytes. signed selects sign-extension;
// wide selects an i64 result (so signed sub-width loads extend to all 64 bits).
func (f *fn) memLoad(r *wasm.Reader, size int, signed, wide bool) error {
	if _, err := r.U32(); err != nil { // align (unused)
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	if f.forwardStoredLoad(off, size, signed, wide) {
		return nil
	}
	f.invalidateStoreForward()
	// The address may read a pinned local's register in place (WARP
	// liftToRegInPlace): the deferred load records the borrow so a local.set of
	// that local realizes the load first, and consumers neither write nor
	// release the register.
	ea, eaOwned, borrow, disp := f.memAddr(off, size, true)
	// Defer the load: push a bounds-checked memory reference (the mov is emitted
	// when the value is materialized, or folded as an r/m operand into a consumer).
	e := f.pushValue(memRefStorage(ea, disp, size, signed, wide, borrow))
	if eaOwned {
		f.regUser[ea] = e // an owned address register belongs to the deferred load
	}
	return nil
}

// memStore lowers a scalar store of `size` bytes.
func (f *fn) memStore(r *wasm.Reader, size int) error {
	if _, err := r.U32(); err != nil { // align (unused)
		return err
	}
	off, err := r.U32()
	if err != nil {
		return err
	}
	f.materializePendingLoads() // deferred loads must read pre-store memory
	// A constant value stores as an immediate directly (selectInstr's `mov r/m,
	// imm` form) — no register, no load-then-store dependency chain. i64 needs
	// two 4-byte immediate stores (low32 at disp, high32 at disp+4): a single
	// 64-bit imm-store sign-extends imm32, which is wrong for an arbitrary
	// 64-bit pattern; narrower stores truncate to the low `size` bytes exactly
	// like a materialized constant would (i64.store8/16/32 route here too).
	if top := f.s.back(); top != nil && top.kind == ekValue && top.st.kind == stConst {
		f.stats.peep("store-imm")
		v := top.st.cval
		f.erase(top)
		ea, eaOwned, _, disp := f.memAddr(off, size, true)
		if size == 8 {
			f.a.StoreImmIdx(RBX, ea, disp, int32(v), 4)
			f.a.StoreImmIdx(RBX, ea, disp+4, int32(v>>32), 4)
		} else {
			f.a.StoreImmIdx(RBX, ea, disp, int32(v), size)
		}
		if eaOwned {
			f.release(ea)
		}
		return nil
	}
	// Both the value and the address are immediate read-only uses here, so a
	// pinned local feeds the store in place — no copy (nothing between the reads
	// and the StoreIdx can write a local).
	value := f.popValue()
	vtyp := value.st.typ
	vreg, vOwned := f.materializeRead(value)
	f.pinned = f.pinned.add(vreg)
	addrLocal, addrOK := localAddressKey(f.s.back())
	ea, eaOwned, _, disp := f.memAddr(off, size, true)
	f.a.StoreIdx(RBX, ea, vreg, disp, size)
	f.pinned = f.pinned.remove(vreg)
	if eaOwned {
		f.release(ea)
	}
	// Open a forwarding window when this store's owned full-width value is about to
	// be re-read from the same local address: keep the value register pinned so the
	// upcoming load forwards it instead of reloading.
	if f.storeForwardOK && vOwned && addrOK &&
		((size == 8 && vtyp == mtI64) || (size == 4 && vtyp == mtI32)) &&
		f.nextLoadMatchesStore(r, addrLocal, off, size, vtyp) {
		f.storeFwd = storeForward{valid: true, reg: vreg, typ: vtyp, local: addrLocal, offset: off, size: size}
		f.pinned = f.pinned.add(vreg)
	} else if vOwned {
		f.release(vreg)
	}
	return nil
}

// localAddressKey returns the local index backing e's value (a local.get result),
// or ok=false if e is not a local reference. Store forwarding keys the address on
// a local identity, not a physical register.
func localAddressKey(e *elem) (int, bool) {
	if e == nil || e.kind != ekValue {
		return 0, false
	}
	switch e.st.kind {
	case stLocalReg, stLocalRef:
		return e.st.idx, true
	default:
		return 0, false
	}
}

// nextLoadMatchesStore bounds the protected-register lifetime before opening a
// forwarding window. It accepts at most three local.get leaves followed by the
// exact full-width load of the same local address+offset; the reader is restored,
// so normal one-pass lowering still consumes every instruction exactly once. This
// captures accumulator + address shapes without retaining state across arbitrary
// expressions.
func (f *fn) nextLoadMatchesStore(r *wasm.Reader, addrLocal int, off uint32, size int, typ machineType) bool {
	save := r.Offset()
	defer func() { _ = r.JumpTo(save) }()
	wantOp := byte(0x28) // i32.load
	if size == 8 && typ == mtI64 {
		wantOp = 0x29 // i64.load
	} else if size != 4 || typ != mtI32 {
		return false
	}
	lastLocal := -1
	for gets := 0; gets <= 3; gets++ {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if op == 0x20 { // local.get
			x, err := r.U32()
			if err != nil {
				return false
			}
			lastLocal = int(x) + f.localBase
			continue
		}
		if op != wantOp || lastLocal != addrLocal {
			return false
		}
		if _, err := r.U32(); err != nil { // alignment
			return false
		}
		loadOff, err := r.U32()
		return err == nil && loadOff == off
	}
	return false
}

// prepareStoreForward keeps the one-entry forwarding value only across local.get
// instructions and a scalar load that may consume it. Every other opcode can
// change memory/address state or makes retaining a register unjustified.
func (f *fn) prepareStoreForward(op byte) {
	if !f.storeFwd.valid {
		return
	}
	if op == 0x20 || (op >= 0x28 && op <= 0x35) { // local.get or scalar load
		return
	}
	f.invalidateStoreForward()
}

func (f *fn) invalidateStoreForward() {
	if !f.storeFwd.valid {
		return
	}
	r := f.storeFwd.reg
	f.storeFwd = storeForward{}
	f.pinned = f.pinned.remove(r)
	f.release(r)
}

// forwardStoredLoad short-circuits a load that exactly re-reads the value a prior
// store just wrote: it pops the (local) address, drops the window, and pushes the
// retained value register directly — no memory access. Returns false (leaving the
// window intact) when the pending load does not match.
func (f *fn) forwardStoredLoad(off uint32, size int, signed, wide bool) bool {
	c := f.storeFwd
	if !c.valid || signed || c.offset != off || c.size != size ||
		(size == 8 && (!wide || c.typ != mtI64)) ||
		(size == 4 && (wide || c.typ != mtI32)) {
		return false
	}
	local, ok := localAddressKey(f.s.back())
	if !ok || local != c.local {
		return false
	}
	addr := f.popValue()
	// local.get is a borrowed reference; no owned register is released here.
	if addr.st.kind != stLocalReg && addr.st.kind != stLocalRef {
		panic("amd64: store-forward address lost local identity")
	}
	f.storeFwd = storeForward{}
	f.pinned = f.pinned.remove(c.reg)
	f.pushReg(c.reg, c.typ)
	f.stats.peep("linear-store-load-fwd")
	return true
}

// trapUnlessLE emits `cmp t, mb; ja trap-stub` — trap when t > mb.
func (f *fn) trapUnlessLE(t, mb Reg) {
	f.a.Cmp64(t, mb)
	f.trapIf(condA, trapMemOOB)
}

// memoryInit lowers memory.init. The three i32 operands (dst, src, n) are read
// from canonical slots into the fixed rep registers RDI/RSI/RCX. The source is
// immutable passive data, so forward rep movsb is sufficient.
func (f *fn) memoryInit(r *wasm.Reader) error {
	dataIdx, err := r.U32()
	if err != nil {
		return err
	}
	if _, err := r.U32(); err != nil { // memidx, validated == 0
		return err
	}
	f.materializePendingLoads()
	types, argsSlot := f.flushSuffix(3)
	f.a.Load64(RDI, RSP, f.spillOff(argsSlot))   // dst offset
	f.a.Load64(RSI, RSP, f.spillOff(argsSlot+1)) // src offset in passive segment
	f.a.Load64(RCX, RSP, f.spillOff(argsSlot+2)) // n

	mb := f.memSizeReg
	if mb == regNone {
		mb = R8
		f.a.Load32(R8, RBX, -bdCurBytes) // memBytes
	}
	f.a.LeaScaled(RDX, RDI, RCX, 0, 0) // dst + n
	f.trapUnlessLE(RDX, mb)

	disp := int32(dataIdx) * 16
	f.a.Load64(R8, RBX, -offPassiveDataPtr) // descriptor array
	f.a.Load32(RAX, R8, disp+8)             // current segment length (zero after data.drop)
	f.a.LeaScaled(RDX, RSI, RCX, 0, 0)      // src + n
	f.trapUnlessLE(RDX, RAX)
	f.a.Load64(R8, R8, disp) // segment base pointer

	f.a.Add64(RDI, RBX) // absolute dst
	f.a.Add64(RSI, R8)  // absolute src
	f.a.RepMovsb()

	f.dropFlushedSuffix(types, 3)
	return nil
}

// dataDrop lowers data.drop by setting the passive segment descriptor length to
// zero. The immutable bytes remain live in the compiled module, but future
// memory.init checks see a zero-length source.
func (f *fn) dataDrop(r *wasm.Reader) error {
	dataIdx, err := r.U32()
	if err != nil {
		return err
	}
	f.materializePendingLoads()
	f.flush()
	disp := int32(dataIdx)*16 + 8
	f.a.Load64(R8, RBX, -offPassiveDataPtr)
	f.a.StoreImm32Mem(R8, disp, 0)
	return nil
}

// memoryCopy lowers memory.copy with memmove semantics (rep movsb, overlap-safe).
// The three i32 operands (dst, src, n) are read from canonical slots into the
// fixed rep registers RDI/RSI/RCX; RDX/R8 are the free scratch after the flush.
func (f *fn) memoryCopy(r *wasm.Reader) error {
	if _, err := r.U32(); err != nil { // dst memidx
		return err
	}
	if _, err := r.U32(); err != nil { // src memidx
		return err
	}
	if top := f.s.back(); top != nil && top.kind == ekValue && top.st.kind == stConst {
		if n := uint64(uint32(top.st.cval)); n <= 64 {
			f.stats.peep("memcopy-unroll")
			f.memoryCopyConst(int(n))
			return nil
		}
	}
	f.materializePendingLoads()
	types, argsSlot := f.flushSuffix(3)
	f.a.Load64(RDI, RSP, f.spillOff(argsSlot))   // dst offset
	f.a.Load64(RSI, RSP, f.spillOff(argsSlot+1)) // src offset
	f.a.Load64(RCX, RSP, f.spillOff(argsSlot+2)) // n

	// Scratch in RDX/R8 only (never pinnable); R9 may hold a pinned local.
	mb := f.memSizeReg
	if mb == regNone {
		mb = R8
		f.a.Load32(R8, RBX, -bdCurBytes) // memBytes
	}
	f.a.LeaScaled(RDX, RDI, RCX, 0, 0) // dst + n
	f.trapUnlessLE(RDX, mb)
	f.a.LeaScaled(RDX, RSI, RCX, 0, 0) // src + n
	f.trapUnlessLE(RDX, mb)

	f.a.Add64(RDI, RBX) // absolute dst
	f.a.Add64(RSI, RBX) // absolute src

	// Hybrid dispatch: small dynamic copies take an inline 8-byte-chunk memmove
	// loop (WARP emitMemcpyNoBoundsCheck) — `rep movsb`'s ~30-cycle startup
	// dominates the string-append copies AssemblyScript's __renew makes
	// constantly; large copies keep rep movsb (ERMSB wins at size).
	var joins []int
	f.a.AluRI(cmpDigit, RCX, smallBulkMax, true)
	big := f.a.JccPlaceholder(condAE)

	f.a.Cmp64(RSI, RDI)
	fwdSmall := f.a.JccPlaceholder(condA) // src > dst → forward copy is overlap-safe
	// dst >= src: copy backward, indexing [ptr+rcx-k] while counting rcx down.
	back8 := f.a.Len()
	f.a.AluRI(cmpDigit, RCX, 8, false)
	b8done := f.a.JccPlaceholder(condB)
	f.a.LoadIdx(RDX, RSI, RCX, -8, 8, false, true)
	f.a.StoreIdx(RDI, RCX, RDX, -8, 8)
	f.a.AluRI(5, RCX, 8, false) // rcx -= 8
	f.a.JmpBack(back8)
	f.a.PatchRel32(b8done, f.a.Len())
	f.a.TestSelf(RCX, false)
	joins = append(joins, f.a.JccPlaceholder(condE))
	back1 := f.a.Len()
	f.a.LoadIdx(RDX, RSI, RCX, -1, 1, false, false)
	f.a.StoreIdx(RDI, RCX, RDX, -1, 1)
	f.a.AluRI(5, RCX, 1, false)
	f.a.PatchRel32(f.a.JccPlaceholder(condNE), back1)
	joins = append(joins, f.a.JmpPlaceholder())

	// src > dst: copy forward via a negative index climbing to zero (WARP's shape).
	f.a.PatchRel32(fwdSmall, f.a.Len())
	f.a.Add64(RSI, RCX)
	f.a.Add64(RDI, RCX)
	f.a.Neg(RCX, true)
	fwd8 := f.a.Len()
	f.a.AluRI(cmpDigit, RCX, -8, true)
	f8done := f.a.JccPlaceholder(condG)
	f.a.LoadIdx(RDX, RSI, RCX, 0, 8, false, true)
	f.a.StoreIdx(RDI, RCX, RDX, 0, 8)
	f.a.AluRI(0, RCX, 8, true) // rcx += 8
	f.a.JmpBack(fwd8)
	f.a.PatchRel32(f8done, f.a.Len())
	f.a.TestSelf(RCX, true)
	joins = append(joins, f.a.JccPlaceholder(condE))
	fwd1 := f.a.Len()
	f.a.LoadIdx(RDX, RSI, RCX, 0, 1, false, false)
	f.a.StoreIdx(RDI, RCX, RDX, 0, 1)
	f.a.AluRI(0, RCX, 1, true)
	f.a.PatchRel32(f.a.JccPlaceholder(condNE), fwd1)
	joins = append(joins, f.a.JmpPlaceholder())

	// Large: overlap-safe rep movsb. Backward (DF=1) is only REQUIRED when the
	// regions truly overlap with dst ahead of src; a disjoint copy (dst >= src+n,
	// e.g. AssemblyScript __renew growing a buffer) is forward-safe. This matters
	// because backward `rep movsb` gets no ERMSB/FSRM acceleration — it runs at
	// ~1 byte/cycle — while forward does, so route disjoint high-dst copies to
	// the fast forward path instead of the slow backward one.
	f.a.PatchRel32(big, f.a.Len())
	f.a.Cmp64(RDI, RSI)
	fwd := f.a.JccPlaceholder(condBE)  // dst <= src → forward
	f.a.LeaScaled(RDX, RSI, RCX, 0, 0) // rdx = src + n
	f.a.Cmp64(RDI, RDX)
	fwdDisjoint := f.a.JccPlaceholder(condAE) // dst >= src+n → disjoint → forward
	f.a.LeaScaled(RDI, RDI, RCX, 0, -1)       // last dst byte
	f.a.LeaScaled(RSI, RSI, RCX, 0, -1)       // last src byte
	f.a.Std()
	f.a.RepMovsb()
	f.a.Cld()
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(fwd, f.a.Len())
	f.a.PatchRel32(fwdDisjoint, f.a.Len())
	f.a.RepMovsb() // forward (DF=0 by ABI)
	f.a.PatchRel32(done, f.a.Len())
	for _, j := range joins {
		f.a.PatchRel32(j, f.a.Len())
	}

	f.dropFlushedSuffix(types, 3)
	return nil
}

// memoryFill lowers memory.fill (memset of the low byte of val) via rep stosb.
func (f *fn) memoryFill(r *wasm.Reader) error {
	if _, err := r.U32(); err != nil { // memidx
		return err
	}
	if top := f.s.back(); top != nil && top.kind == ekValue && top.st.kind == stConst {
		if n := uint64(uint32(top.st.cval)); n <= 64 {
			f.memoryFillConst(int(n))
			return nil
		}
	}
	f.materializePendingLoads()
	types, argsSlot := f.flushSuffix(3)
	f.a.Load64(RDI, RSP, f.spillOff(argsSlot))   // dst offset
	f.a.Load64(RAX, RSP, f.spillOff(argsSlot+1)) // AL = fill byte
	f.a.Load64(RCX, RSP, f.spillOff(argsSlot+2)) // n

	// Scratch in RDX/R8 only (never pinnable); R9 may hold a pinned local.
	mb := f.memSizeReg
	if mb == regNone {
		mb = R8
		f.a.Load32(R8, RBX, -bdCurBytes)
	}
	f.a.LeaScaled(RDX, RDI, RCX, 0, 0) // dst + n
	f.trapUnlessLE(RDX, mb)

	f.a.Add64(RDI, RBX) // absolute dst

	// Byte-replicate the fill value once (rep stosb only reads AL, so the
	// pattern's low byte keeps the big path compatible).
	f.a.AluRI(4, RAX, 0xFF, false) // and eax, 0xff
	f.a.MovImm64(RDX, 0x0101010101010101)
	f.a.IMul(RAX, RDX, true)

	// Small dynamic fills: inline 8-byte pattern stores (rep stosb startup
	// dominates); large keep rep stosb.
	f.a.AluRI(cmpDigit, RCX, smallBulkMax, true)
	bigF := f.a.JccPlaceholder(condAE)
	fill8 := f.a.Len()
	f.a.AluRI(cmpDigit, RCX, 8, false)
	f8done := f.a.JccPlaceholder(condB)
	f.a.StoreIdx(RDI, RCX, RAX, -8, 8)
	f.a.AluRI(5, RCX, 8, false)
	f.a.JmpBack(fill8)
	f.a.PatchRel32(f8done, f.a.Len())
	f.a.TestSelf(RCX, false)
	fillDone := f.a.JccPlaceholder(condE)
	fill1 := f.a.Len()
	f.a.StoreIdx(RDI, RCX, RAX, -1, 1)
	f.a.AluRI(5, RCX, 1, false)
	f.a.PatchRel32(f.a.JccPlaceholder(condNE), fill1)
	skipRep := f.a.JmpPlaceholder()
	f.a.PatchRel32(bigF, f.a.Len())
	f.a.RepStosb() // [RDI..] = AL, RCX times (DF=0)
	f.a.PatchRel32(skipRep, f.a.Len())
	f.a.PatchRel32(fillDone, f.a.Len())

	f.dropFlushedSuffix(types, 3)
	return nil
}

// memorySize pushes the current linear-memory size in pages.
func (f *fn) memorySize(r *wasm.Reader) error {
	if _, err := r.Byte(); err != nil { // memory index (validated == 0)
		return err
	}
	out := f.allocReg(0)
	f.a.Load32(out, RBX, -bdCurPages)
	f.pushReg(out, mtI32)
	return nil
}

// memoryGrow grows linear memory by the popped page delta, pushing the previous
// size in pages or -1 on failure. The reservation is mapped up front, so this is
// a pure size-cache update (matching src/core/encoder/amd64); the base never moves.
func (f *fn) memoryGrow(r *wasm.Reader) error {
	if _, err := r.Byte(); err != nil { // memory index (validated == 0)
		return err
	}
	f.invalidateBoundsCert() // memBytes changes; end the certificate conservatively
	delta := f.materialize(f.popValue())
	f.pinned = f.pinned.add(delta)
	res := f.allocReg(maskOf(delta))
	f.a.Load32(res, RBX, -bdCurPages) // old pages — the success result
	nw := f.allocReg(maskOf(delta).add(res))
	f.a.MovRegReg32(nw, res)
	f.a.Add32(nw, delta) // new = old + delta; CF on u32 overflow
	failOverflow := f.a.JccPlaceholder(condB)
	mx := f.allocReg(maskOf(delta).add(res).add(nw))
	f.a.Load32(mx, RBX, -bdMaxPages)
	f.a.Cmp32(nw, mx)
	failMax := f.a.JccPlaceholder(condA) // new > max
	f.a.Store32(RBX, -bdCurPages, nw)
	f.a.MovRegReg32(mx, nw)
	f.a.ShiftImm(4, mx, wasmPageLog, false) // bytes = pages << 16 (digit 4 = shl)
	f.a.Store32(RBX, -bdCurBytes, mx)
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(failOverflow, f.a.Len())
	f.a.PatchRel32(failMax, f.a.Len())
	f.a.MovImm32(res, -1)
	f.a.PatchRel32(done, f.a.Len())
	if f.memSizeReg != regNone {
		f.a.Load32(f.memSizeReg, RBX, -bdCurBytes) // refresh the memBytes cache (both paths)
	}
	f.pinned = f.pinned.remove(delta)
	f.release(delta)
	f.release(nw)
	f.release(mx)
	f.pushReg(res, mtI32)
	return nil
}

// bulkChunks returns the (offset, size) store/load plan for a small constant
// bulk-memory op: 8-byte blocks with an overlapping tail (memmove's small-size
// technique). For n >= 9 it is a straight run of 8-byte chunks plus a final
// overlapping {n-8,8} tail, which reproduces the earlier fixed cases for n <= 32
// and extends cleanly to 64 (used by fill, whose single pattern register makes
// the chunk count irrelevant to register pressure; copy uses bulkChunks16 past 32).
func bulkChunks(n int) [][2]int {
	switch {
	case n == 0:
		return nil
	case n == 1 || n == 2 || n == 4 || n == 8:
		return [][2]int{{0, n}}
	case n < 4:
		return [][2]int{{0, 2}, {n - 2, 2}} // n == 3
	case n < 8:
		return [][2]int{{0, 4}, {n - 4, 4}}
	}
	var chunks [][2]int
	for off := 0; off+8 < n; off += 8 {
		chunks = append(chunks, [2]int{off, 8})
	}
	return append(chunks, [2]int{n - 8, 8})
}

// bulkChunks16 is bulkChunks with 16-byte (XMM) blocks, for 33..64-byte constant
// copies: at most four SSE loads/stores instead of five-to-eight GP ones, which
// keeps the load-all-then-store-all register footprint within the XMM pool. The
// final {n-16,16} tail overlaps the previous block, so no access exceeds n bytes.
func bulkChunks16(n int) [][2]int {
	var chunks [][2]int
	for off := 0; off+16 < n; off += 16 {
		chunks = append(chunks, [2]int{off, 16})
	}
	return append(chunks, [2]int{n - 16, 16})
}

// bulkBoundsCheck emits `trap unless base+n <= memBytes` for an unrolled bulk
// op (skipped in guard mode: the stores/loads fault like scalar accesses).
func (f *fn) bulkBoundsCheck(base Reg, n int) {
	if f.guardMode {
		return
	}
	f.pinned = f.pinned.add(base)
	t := f.allocReg(0)
	f.a.LeaDisp(t, base, int32(n))
	if f.memSizeReg != regNone {
		f.a.Cmp64(t, f.memSizeReg)
	} else {
		mb := f.allocReg(maskOf(t))
		f.a.Load32(mb, RBX, -bdCurBytes)
		f.a.Cmp64(t, mb)
		f.release(mb)
	}
	f.trapIf(condA, trapMemOOB)
	f.release(t)
	f.pinned = f.pinned.remove(base)
}

// memoryFillConst lowers memory.fill with a small constant length as unrolled
// stores of a byte-replicated pattern — no flush, no rep-stos microcode startup.
func (f *fn) memoryFillConst(n int) {
	f.stats.peep("memfill-unroll")
	f.materializePendingLoads() // pending loads must read pre-fill memory
	f.erase(f.s.back())         // n (const)
	valElem := f.popValue()
	pat := regNone
	if n > 0 {
		if valElem.st.kind == stConst {
			b := uint64(valElem.st.cval) & 0xFF
			pat = f.allocReg(0)
			f.a.MovImm64(pat, b*0x0101010101010101)
		} else {
			v := f.materialize(valElem)  // owned: the low-byte mask below mutates it
			f.a.AluRI(4, v, 0xFF, false) // v &= 0xFF (only AL matters, like rep stosb)
			pat = f.allocReg(maskOf(v))
			f.a.MovImm64(pat, 0x0101010101010101)
			f.a.IMul(pat, v, true) // replicate the byte across all 8 lanes
			f.release(v)
		}
		f.pinned = f.pinned.add(pat)
	}
	dst, dstOwned := f.materializeRead(f.popValue())
	f.bulkBoundsCheck(dst, n)
	for _, c := range bulkChunks(n) {
		f.a.StoreIdx(RBX, dst, pat, int32(c[0]), c[1])
	}
	if pat != regNone {
		f.pinned = f.pinned.remove(pat)
		f.release(pat)
	}
	if dstOwned {
		f.release(dst)
	}
}

// memoryCopyConst lowers memory.copy with a small constant length as
// load-all-then-store-all chunks — inherently overlap-safe (memmove semantics).
func (f *fn) memoryCopyConst(n int) {
	f.materializePendingLoads()
	f.erase(f.s.back()) // n (const)
	src, srcOwned := f.materializeRead(f.popValue())
	f.pinned = f.pinned.add(src)
	dst, dstOwned := f.materializeRead(f.popValue())
	f.pinned = f.pinned.add(dst)
	f.bulkBoundsCheck(dst, n)
	f.bulkBoundsCheck(src, n)
	if n > 32 {
		// 33..64 bytes: SSE 16-byte load-all-then-store-all. At most four XMM
		// registers, so the load-all footprint stays in the float pool (the GP
		// 8-byte form would need five-to-eight registers). Overlap-safe (memmove
		// semantics) because every load precedes every store.
		chunks := bulkChunks16(n)
		xregs := make([]Reg, len(chunks))
		var favoid regMask
		for i, c := range chunks {
			x := f.allocFReg(favoid)
			f.a.VMovdquLoadIdx(x, RBX, src, int32(c[0]))
			xregs[i] = x
			favoid = favoid.add(x)
		}
		for i, c := range chunks {
			f.a.VMovdquStoreIdx(RBX, dst, xregs[i], int32(c[0]))
			f.releaseF(xregs[i])
		}
		f.pinned = f.pinned.remove(src)
		f.pinned = f.pinned.remove(dst)
		if srcOwned {
			f.release(src)
		}
		if dstOwned {
			f.release(dst)
		}
		return
	}
	chunks := bulkChunks(n)
	regs := make([]Reg, len(chunks))
	avoid := maskOf(src, dst)
	for i, c := range chunks {
		r := f.allocReg(avoid)
		f.a.LoadIdx(r, RBX, src, int32(c[0]), c[1], false, c[1] == 8)
		regs[i] = r
		avoid = avoid.add(r)
	}
	for i, c := range chunks {
		f.a.StoreIdx(RBX, dst, regs[i], int32(c[0]), c[1])
		f.release(regs[i])
	}
	f.pinned = f.pinned.remove(src)
	f.pinned = f.pinned.remove(dst)
	if srcOwned {
		f.release(src)
	}
	if dstOwned {
		f.release(dst)
	}
}
