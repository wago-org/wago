//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Linear-memory access: scalar loads/stores with a linear bounds check, plus
// memory.size/grow. Ported from WARP's memory lowering, adapted to wago's runtime
// memory ABI (the same one src/core/encoder/arm64 targets): the linear-memory base is
// pinned in linMemReg for the whole function, and a small "basedata" header sits at
// negative offsets from that base.

// Trap codes — must match jit.TrapCode / the values the engine reads (identical
// to the amd64 twin's table).
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

// offTrapHandlerPtr/offTrapStackReentry are the linMem-relative slots (bytes
// below the linMem base) where the trampoline stashes the continuation PC and
// entry SP for handler-jump trap unwinding. The continuation uses the otherwise
// unused runtimePtr slot; offset 16 would overlap bdMaxPages at [linMem-12].
const (
	offTrapHandlerPtr   = 32
	offTrapStackReentry = 24
)

// smallBulkMax is the dynamic memory.copy/fill length below which the inline
// 8-byte chunk loops beat the NEON loop startup latency. At the boundary the
// NEON path wins, so its dispatch uses n >= smallBulkMax.
const smallBulkMax = 64

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
// ENTIRE native call tree in one jump: it restores SP to the entry SP the
// trampoline recorded at [linMem-offTrapStackReentry] and RETs straight back into
// enterNative (WARP's handler-jump model). This is what lets callers skip the
// per-call "load *trap; test; branch" check — a trap never returns through an
// intermediate frame. Terminal, so it may freely clobber the call scratch (and SP
// last).
func (f *fn) emitTrap(code uint32) {
	f.ld64(X9, linMemReg, -int32(offTrapCellPtr)) // X9 = &trapCell
	if code == 0 {
		f.st32(X9, 0, ZR)
	} else {
		f.a.MovImm64(X16, uint64(uint32(code)))
		f.st32(X9, 0, X16) // *trapCell = code
	}
	// AArch64 has no return address on the stack: BL wrote it into LR, which a
	// trap deep in the wasm call tree may have clobbered. The trampoline records
	// both the foreign-stack save-area SP and the continuation PC in basedata, so
	// the cold trap path can jump straight to the shared enterNative/resumeNative
	// epilogue like amd64's handler-jump model.
	f.ld64(X16, linMemReg, -int32(offTrapStackReentry)) // X16 = foreign-stack save area
	f.ld64(LR, linMemReg, -int32(offTrapHandlerPtr))    // LR = trampoline continuation PC
	f.a.AddImm64(SP, X16, 0)                            // MOV SP, X16 (restore entry SP)
	f.a.Ret()
}

// emitInterruptCheck polls the invocation trap cell at bounded native safe
// points. A context watcher writes TrapInterrupted there; the ordinary cold trap
// path then unwinds the complete wasm call tree.
func (f *fn) emitInterruptCheck() {
	if !f.interruptible {
		return
	}
	f.ld64(X16, linMemReg, -int32(offTrapCellPtr))
	f.ld32(X17, X16, 0)
	f.cmpImm(X17, 0, false)
	f.trapIf(condNE, trapInterrupted)
}

// trapIf records a conditional branch to this function's shared trap stub for
// `code` (emitted once, after the body, by emitTrapStubs). Checks branch TO the
// cold stub on failure, so the hot path falls through — instead of jumping over
// an inline trap block at every site (better I-cache, not-taken hot branches, one
// stub per trap code instead of one block per check).
func (f *fn) trapIf(cc Cond, code uint32) {
	if code == trapMemOOB {
		f.stats.addBoundsCheck() // inline linear-memory OOB check (P6 elides these)
	}
	// A B.cond site (imm19, ±1 MiB); bit0 of the site offset is 0 (4-aligned), so
	// emitTrapStubs uses it to tag Bcond vs Branch patch ranges (§6.2).
	f.trapSites[code] = append(f.trapSites[code], f.a.Bcond(cc))
}

// trapAlways is trapIf's unconditional form (`unreachable`): a single B to the
// shared stub instead of an inline trap block. The site is tagged with bit0 set so
// emitTrapStubs patches it with PatchBranch26 (imm26, ±128 MiB) rather than the
// PatchBranch19 range a B.cond site uses.
func (f *fn) trapAlways(code uint32) {
	f.trapSites[code] = append(f.trapSites[code], f.a.Branch()|1)
}

// emitTrapStubs emits one trap stub per trap code used by this function and
// patches every recorded site to it. Called once, after the epilogue.
func (f *fn) emitTrapStubs() {
	for code := uint32(1); code <= trapStackFence; code++ { // deterministic order
		sites := f.trapSites[code]
		if len(sites) == 0 {
			continue
		}
		f.stats.addTrapStub()
		pos := f.a.Len()
		f.storeModuleGlobals(X9) // post-trap global state stays observable (X9 is trap-path scratch)
		f.emitTrap(code)
		for _, s := range sites {
			if s&1 != 0 {
				f.a.PatchBranch26(s&^1, pos) // trapAlways: unconditional B (imm26)
			} else {
				f.a.PatchBranch19(s, pos) // trapIf: B.cond (imm19)
			}
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
		f.a.MovImm64(t, uint64(uint32(off)))
		f.a.Add64(ea, ea, t)
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
	f.leaDisp(t, ea, leaDisp, true) // t = ea + off + size
	if f.memSizeReg != regNone {
		f.cmpRR(t, f.memSizeReg, true) // memBytes lives in a register (WARP REGS::memSize)
	} else {
		mb := f.allocReg(maskOf(t))
		f.ld32(mb, linMemReg, -int32(bdCurBytes)) // memory size in bytes
		f.cmpRR(t, mb, true)
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
	// Defer the load: push a bounds-checked memory reference (the LDR is emitted
	// when the value is materialized — arm64 has no memory operand to fold into,
	// so unlike x86 there is no r/m consumer, but deferring still lets the consumer
	// pick the destination register and elide dead loads).
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
	// A constant value stores as an immediate directly (StoreImmIdx materializes
	// the constant into scratch and stores it) — no long-lived register, no
	// load-then-store dependency chain. i64 needs two 4-byte immediate stores
	// (low32 at disp, high32 at disp+4); narrower stores truncate to the low `size`
	// bytes exactly like a materialized constant would (i64.store8/16/32 route here
	// too).
	if top := f.s.back(); top != nil && top.kind == ekValue && top.st.kind == stConst {
		f.stats.peep("store-imm")
		v := top.st.cval
		f.erase(top)
		ea, eaOwned, _, disp := f.memAddr(off, size, true)
		f.pinned = f.pinned.add(ea)
		f.materializePendingLoadsBeforeStore(ea, disp, size)
		if size == 8 {
			f.a.StoreImmIdx(linMemReg, ea, disp, int32(v), 4)
			f.a.StoreImmIdx(linMemReg, ea, disp+4, int32(v>>32), 4)
		} else {
			f.a.StoreImmIdx(linMemReg, ea, disp, int32(v), size)
		}
		f.pinned = f.pinned.remove(ea)
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
	f.pinned = f.pinned.add(ea)
	f.materializePendingLoadsBeforeStore(ea, disp, size)
	f.a.StoreIdx(linMemReg, ea, vreg, disp, size)
	f.pinned = f.pinned.remove(ea)
	f.pinned = f.pinned.remove(vreg)
	if eaOwned {
		f.release(ea)
	}
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

// nextLoadMatchesStore bounds the protected-register lifetime before opening a
// forwarding window. It accepts at most three local.get leaves followed by the
// exact full-width load; the reader is restored, so normal one-pass lowering
// still consumes every instruction exactly once. This captures accumulator +
// address shapes without retaining hidden state across arbitrary expressions.
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
		if op == 0x20 {
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
		panic("arm64: store-forward address lost local identity")
	}
	f.storeFwd = storeForward{}
	f.pinned = f.pinned.remove(c.reg)
	f.pushReg(c.reg, c.typ)
	f.stats.peep("linear-store-load-fwd")
	return true
}

// trapUnlessLE emits `cmp t, mb; b.hi trap-stub` — trap when t > mb.
func (f *fn) trapUnlessLE(t, mb Reg) {
	f.cmpRR(t, mb, true)
	f.trapIf(condA, trapMemOOB)
}

// copyFwdLoop emits a forward block-copy loop (AArch64 has no `rep movsb`, §4f):
// copy 64-byte NEON groups while possible, then 32-, 16-, and 8-byte chunks and
// a byte tail. dst/src are absolute byte pointers and n the count; all three are
// clobbered, and V16/X16 holds each chunk.
func (f *fn) copyFwdLoop(dst, src, n Reg) {
	skip := f.a.Cbz64(n) // nothing to copy
	f.cmpImm(n, 64, true)
	wideTail := f.a.Bcond(condB)
	wideLoop := f.a.Len()
	f.a.LdrQ(X16, src, 0)
	f.a.LdrQ(X17, src, 16)
	f.a.LdrQ(X18, src, 32)
	f.a.LdrQ(X19, src, 48)
	f.a.StrQ(dst, 0, X16)
	f.a.StrQ(dst, 16, X17)
	f.a.StrQ(dst, 32, X18)
	f.a.StrQ(dst, 48, X19)
	f.a.AddImm64(src, src, 64)
	f.a.AddImm64(dst, dst, 64)
	f.a.SubImm64(n, n, 64)
	f.cmpImm(n, 64, true)
	f.a.PatchBranch19(f.a.Bcond(condAE), wideLoop)
	f.a.PatchBranch19(wideTail, f.a.Len())
	f.cmpImm(n, 32, true)
	vecTail := f.a.Bcond(condB)
	vecLoop32 := f.a.Len()
	f.a.LdrQ(X16, src, 0)
	f.a.LdrQ(X17, src, 16)
	f.a.StrQ(dst, 0, X16)
	f.a.StrQ(dst, 16, X17)
	f.a.AddImm64(src, src, 32)
	f.a.AddImm64(dst, dst, 32)
	f.a.SubImm64(n, n, 32)
	f.cmpImm(n, 32, true)
	f.a.PatchBranch19(f.a.Bcond(condAE), vecLoop32)
	f.a.PatchBranch19(vecTail, f.a.Len())
	f.cmpImm(n, 16, true)
	wordTail := f.a.Bcond(condB)
	vecLoop := f.a.Len()
	f.a.LdrQ(X16, src, 0)
	f.a.StrQ(dst, 0, X16)
	f.a.AddImm64(src, src, 16)
	f.a.AddImm64(dst, dst, 16)
	f.a.SubImm64(n, n, 16)
	f.cmpImm(n, 16, true)
	f.a.PatchBranch19(f.a.Bcond(condAE), vecLoop)
	f.a.PatchBranch19(wordTail, f.a.Len())
	f.cmpImm(n, 8, true)
	byteTail := f.a.Bcond(condB)
	wordLoop := f.a.Len()
	f.a.Load64(X16, src, 0)
	f.a.Store64(X16, dst, 0)
	f.a.AddImm64(src, src, 8)
	f.a.AddImm64(dst, dst, 8)
	f.a.SubImm64(n, n, 8)
	f.cmpImm(n, 8, true)
	f.a.PatchBranch19(f.a.Bcond(condAE), wordLoop)
	f.a.PatchBranch19(byteTail, f.a.Len())
	done := f.a.Cbz64(n)
	loop := f.a.Len()
	f.a.Ldrb(X16, src, 0)
	f.a.Strb(X16, dst, 0)
	f.a.AddImm64(src, src, 1)
	f.a.AddImm64(dst, dst, 1)
	f.a.SubImm64(n, n, 1)
	f.a.PatchBranch19(f.a.Cbnz64(n), loop)
	f.a.PatchBranch19(done, f.a.Len())
	f.a.PatchBranch19(skip, f.a.Len())
}

// copyBackLoop emits a backward byte-copy loop for overlap-safe memmove when dst
// is ahead of src (the arm64 analog of amd64's `std; rep movsb; cld`): it walks
// from the end down, copying 64-byte NEON groups, then 32-, 16-, and 8-byte
// chunks and the byte tail. dst/src are absolute base pointers, n the count;
// all clobbered, and
// V16/X16 holds each chunk.
func (f *fn) copyBackLoop(dst, src, n Reg) {
	skip := f.a.Cbz64(n)
	f.a.Add64(dst, dst, n)
	f.a.Add64(src, src, n)
	f.cmpImm(n, 64, true)
	wideTail := f.a.Bcond(condB)
	wideLoop := f.a.Len()
	f.a.SubImm64(src, src, 64)
	f.a.SubImm64(dst, dst, 64)
	f.a.LdrQ(X16, src, 0)
	f.a.LdrQ(X17, src, 16)
	f.a.LdrQ(X18, src, 32)
	f.a.LdrQ(X19, src, 48)
	f.a.StrQ(dst, 0, X16)
	f.a.StrQ(dst, 16, X17)
	f.a.StrQ(dst, 32, X18)
	f.a.StrQ(dst, 48, X19)
	f.a.SubImm64(n, n, 64)
	f.cmpImm(n, 64, true)
	f.a.PatchBranch19(f.a.Bcond(condAE), wideLoop)
	f.a.PatchBranch19(wideTail, f.a.Len())
	f.cmpImm(n, 32, true)
	vecTail := f.a.Bcond(condB)
	vecLoop32 := f.a.Len()
	f.a.SubImm64(src, src, 32)
	f.a.SubImm64(dst, dst, 32)
	f.a.LdrQ(X16, src, 0)
	f.a.LdrQ(X17, src, 16)
	f.a.StrQ(dst, 0, X16)
	f.a.StrQ(dst, 16, X17)
	f.a.SubImm64(n, n, 32)
	f.cmpImm(n, 32, true)
	f.a.PatchBranch19(f.a.Bcond(condAE), vecLoop32)
	f.a.PatchBranch19(vecTail, f.a.Len())
	f.cmpImm(n, 16, true)
	wordTail := f.a.Bcond(condB)
	vecLoop := f.a.Len()
	f.a.SubImm64(src, src, 16)
	f.a.SubImm64(dst, dst, 16)
	f.a.LdrQ(X16, src, 0)
	f.a.StrQ(dst, 0, X16)
	f.a.SubImm64(n, n, 16)
	f.cmpImm(n, 16, true)
	f.a.PatchBranch19(f.a.Bcond(condAE), vecLoop)
	f.a.PatchBranch19(wordTail, f.a.Len())
	f.cmpImm(n, 8, true)
	byteTail := f.a.Bcond(condB)
	wordLoop := f.a.Len()
	f.a.SubImm64(src, src, 8)
	f.a.SubImm64(dst, dst, 8)
	f.a.Load64(X16, src, 0)
	f.a.Store64(X16, dst, 0)
	f.a.SubImm64(n, n, 8)
	f.cmpImm(n, 8, true)
	f.a.PatchBranch19(f.a.Bcond(condAE), wordLoop)
	f.a.PatchBranch19(byteTail, f.a.Len())
	done := f.a.Cbz64(n)
	loop := f.a.Len()
	f.a.SubImm64(src, src, 1)
	f.a.SubImm64(dst, dst, 1)
	f.a.Ldrb(X16, src, 0)
	f.a.Strb(X16, dst, 0)
	f.a.SubImm64(n, n, 1)
	f.a.PatchBranch19(f.a.Cbnz64(n), loop)
	f.a.PatchBranch19(done, f.a.Len())
	f.a.PatchBranch19(skip, f.a.Len())
}

// fillLoop emits a forward byte-fill loop (the arm64 analog of `rep stosb`):
// write 64-byte NEON groups while possible, then 32-, 16-, and 8-byte chunks
// and a byte tail.
func (f *fn) fillLoop(dst, pat, n Reg) {
	skip := f.a.Cbz64(n)
	f.a.FmovFromGpr(X16, pat, true)
	f.a.NeonInsD(X16, pat, 1)
	f.cmpImm(n, 64, true)
	wideTail := f.a.Bcond(condB)
	wideLoop := f.a.Len()
	f.a.StrQ(dst, 0, X16)
	f.a.StrQ(dst, 16, X16)
	f.a.StrQ(dst, 32, X16)
	f.a.StrQ(dst, 48, X16)
	f.a.AddImm64(dst, dst, 64)
	f.a.SubImm64(n, n, 64)
	f.cmpImm(n, 64, true)
	f.a.PatchBranch19(f.a.Bcond(condAE), wideLoop)
	f.a.PatchBranch19(wideTail, f.a.Len())
	f.cmpImm(n, 32, true)
	vecTail := f.a.Bcond(condB)
	vecLoop32 := f.a.Len()
	f.a.StrQ(dst, 0, X16)
	f.a.StrQ(dst, 16, X16)
	f.a.AddImm64(dst, dst, 32)
	f.a.SubImm64(n, n, 32)
	f.cmpImm(n, 32, true)
	f.a.PatchBranch19(f.a.Bcond(condAE), vecLoop32)
	f.a.PatchBranch19(vecTail, f.a.Len())
	f.cmpImm(n, 16, true)
	wordTail := f.a.Bcond(condB)
	vecLoop := f.a.Len()
	f.a.StrQ(dst, 0, X16)
	f.a.AddImm64(dst, dst, 16)
	f.a.SubImm64(n, n, 16)
	f.cmpImm(n, 16, true)
	f.a.PatchBranch19(f.a.Bcond(condAE), vecLoop)
	f.a.PatchBranch19(wordTail, f.a.Len())
	f.cmpImm(n, 8, true)
	byteTail := f.a.Bcond(condB)
	wordLoop := f.a.Len()
	f.a.Store64(pat, dst, 0)
	f.a.AddImm64(dst, dst, 8)
	f.a.SubImm64(n, n, 8)
	f.cmpImm(n, 8, true)
	f.a.PatchBranch19(f.a.Bcond(condAE), wordLoop)
	f.a.PatchBranch19(byteTail, f.a.Len())
	done := f.a.Cbz64(n)
	loop := f.a.Len()
	f.a.Strb(pat, dst, 0)
	f.a.AddImm64(dst, dst, 1)
	f.a.SubImm64(n, n, 1)
	f.a.PatchBranch19(f.a.Cbnz64(n), loop)
	f.a.PatchBranch19(done, f.a.Len())
	f.a.PatchBranch19(skip, f.a.Len())
}

// memoryInit lowers memory.init. The three i32 operands (dst, src, n) are read
// from canonical slots into the scratch registers X9/X10/X11. The source is
// immutable passive data, so a forward byte-copy loop is sufficient.
func (f *fn) memoryInit(r *wasm.Reader) error {
	dataIdx, err := r.U32()
	if err != nil {
		return err
	}
	if _, err := r.U32(); err != nil { // memidx, validated == 0
		return err
	}
	f.materializePendingLoads()
	f.flush()
	d := f.depth()
	f.ld64(X9, SP, f.spillOff(d-3))  // dst offset
	f.ld64(X10, SP, f.spillOff(d-2)) // src offset in passive segment
	f.ld64(X11, SP, f.spillOff(d-1)) // n

	mb := f.memSizeReg
	if mb == regNone {
		mb = X13
		f.ld32(X13, linMemReg, -int32(bdCurBytes)) // memBytes
	}
	f.leaScaled(X12, X9, X11, 0, 0, true) // dst + n
	f.trapUnlessLE(X12, mb)

	disp := int32(dataIdx) * 16
	f.ld64(X13, linMemReg, -int32(offPassiveDataPtr)) // descriptor array
	f.ld32(X14, X13, disp+8)                          // current segment length (zero after data.drop)
	f.leaScaled(X12, X10, X11, 0, 0, true)            // src + n
	f.trapUnlessLE(X12, X14)
	f.ld64(X13, X13, disp) // segment base pointer

	f.a.Add64(X9, X9, linMemReg) // absolute dst
	f.a.Add64(X10, X10, X13)     // absolute src
	f.copyFwdLoop(X9, X10, X11)

	f.setDepth(d - 3)
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
	f.ld64(X13, linMemReg, -int32(offPassiveDataPtr))
	f.st32(X13, disp, ZR)
	return nil
}

// memoryCopy lowers memory.copy with memmove semantics (overlap-safe). The three
// i32 operands (dst, src, n) are read from canonical slots into the scratch
// registers X9/X10/X11; X12/X13 are free scratch after the flush.
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
	f.flush()
	d := f.depth()
	f.ld64(X9, SP, f.spillOff(d-3))  // dst offset
	f.ld64(X10, SP, f.spillOff(d-2)) // src offset
	f.ld64(X11, SP, f.spillOff(d-1)) // n

	// Scratch in X12/X13 only (never pinnable); X9-X11 hold dst/src/n.
	mb := f.memSizeReg
	if mb == regNone {
		mb = X13
		f.ld32(X13, linMemReg, -int32(bdCurBytes)) // memBytes
	}
	f.leaScaled(X12, X9, X11, 0, 0, true) // dst + n
	f.trapUnlessLE(X12, mb)
	f.leaScaled(X12, X10, X11, 0, 0, true) // src + n
	f.trapUnlessLE(X12, mb)

	f.a.Add64(X9, X9, linMemReg)   // absolute dst
	f.a.Add64(X10, X10, linMemReg) // absolute src

	// Hybrid dispatch: small dynamic copies take an inline 8-byte-chunk memmove
	// loop (WARP emitMemcpyNoBoundsCheck) — the byte-copy loop's per-element cost
	// dominates the string-append copies AssemblyScript's __renew makes constantly;
	// large copies fall through to the block byte-copy loops. joins26 collects the
	// unconditional B (imm26) exits, joins19 the CBZ (imm19) exits.
	var joins26, joins19 []int
	f.cmpImm(X11, smallBulkMax, true)
	big := f.a.Bcond(condAE)

	f.cmpRR(X10, X9, true)
	fwdSmall := f.a.Bcond(condA) // src > dst → forward copy is overlap-safe
	// dst >= src: copy backward, indexing [ptr+n-k] while counting n down.
	back8 := f.a.Len()
	f.cmpImm(X11, 8, false)
	b8done := f.a.Bcond(condB)
	f.a.LoadIdx(X12, X10, X11, -8, 8, false, true)
	f.a.StoreIdx(X9, X11, X12, -8, 8)
	f.a.SubImm32(X11, X11, 8) // n -= 8
	f.a.PatchBranch26(f.a.Branch(), back8)
	f.a.PatchBranch19(b8done, f.a.Len())
	joins19 = append(joins19, f.a.Cbz64(X11)) // n == 0 → done
	back1 := f.a.Len()
	f.a.LoadIdx(X12, X10, X11, -1, 1, false, false)
	f.a.StoreIdx(X9, X11, X12, -1, 1)
	f.a.SubImm32(X11, X11, 1)
	f.a.PatchBranch19(f.a.Cbnz64(X11), back1)
	joins26 = append(joins26, f.a.Branch())

	// src > dst: copy forward via a negative index climbing to zero (WARP's shape).
	f.a.PatchBranch19(fwdSmall, f.a.Len())
	f.a.Add64(X10, X10, X11)
	f.a.Add64(X9, X9, X11)
	f.a.Sub64(X11, ZR, X11) // neg n (NEG = SUB from XZR)
	fwd8 := f.a.Len()
	f.cmpImmS(X11, -8, true)
	f8done := f.a.Bcond(condG)
	f.a.LoadIdx(X12, X10, X11, 0, 8, false, true)
	f.a.StoreIdx(X9, X11, X12, 0, 8)
	f.a.AddImm64(X11, X11, 8) // n += 8
	f.a.PatchBranch26(f.a.Branch(), fwd8)
	f.a.PatchBranch19(f8done, f.a.Len())
	joins19 = append(joins19, f.a.Cbz64(X11))
	fwd1 := f.a.Len()
	f.a.LoadIdx(X12, X10, X11, 0, 1, false, false)
	f.a.StoreIdx(X9, X11, X12, 0, 1)
	f.a.AddImm64(X11, X11, 1)
	f.a.PatchBranch19(f.a.Cbnz64(X11), fwd1)
	joins26 = append(joins26, f.a.Branch())

	// Large: overlap-safe block copy. Copy backward only when the regions truly
	// overlap with dst ahead of src; a disjoint copy (dst >= src+n, e.g.
	// AssemblyScript __renew growing a buffer) is forward-safe, and the forward
	// byte loop is the common case — so route disjoint high-dst copies to the
	// forward loop instead of the slower backward one.
	f.a.PatchBranch19(big, f.a.Len())
	f.cmpRR(X9, X10, true)
	fwd := f.a.Bcond(condBE)               // dst <= src → forward
	f.leaScaled(X12, X10, X11, 0, 0, true) // X12 = src + n
	f.cmpRR(X9, X12, true)
	fwdDisjoint := f.a.Bcond(condAE) // dst >= src+n → disjoint → forward
	f.copyBackLoop(X9, X10, X11)     // dst ahead of src and overlapping → backward
	done := f.a.Branch()
	f.a.PatchBranch19(fwd, f.a.Len())
	f.a.PatchBranch19(fwdDisjoint, f.a.Len())
	f.copyFwdLoop(X9, X10, X11) // forward
	f.a.PatchBranch26(done, f.a.Len())
	for _, j := range joins26 {
		f.a.PatchBranch26(j, f.a.Len())
	}
	for _, j := range joins19 {
		f.a.PatchBranch19(j, f.a.Len())
	}

	f.setDepth(d - 3)
	return nil
}

// memoryFill lowers memory.fill (memset of the low byte of val) via a byte-fill
// loop.
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
	f.flush()
	d := f.depth()
	f.ld64(X9, SP, f.spillOff(d-3))  // dst offset
	f.ld64(X14, SP, f.spillOff(d-2)) // fill byte (low 8 bits)
	f.ld64(X11, SP, f.spillOff(d-1)) // n

	// Scratch in X12/X13 only (never pinnable).
	mb := f.memSizeReg
	if mb == regNone {
		mb = X13
		f.ld32(X13, linMemReg, -int32(bdCurBytes))
	}
	f.leaScaled(X12, X9, X11, 0, 0, true) // dst + n
	f.trapUnlessLE(X12, mb)

	f.a.Add64(X9, X9, linMemReg) // absolute dst

	// Byte-replicate the fill value once so the inline 8-byte chunk stores below
	// broadcast the pattern; the byte loop only reads the low byte.
	f.a.AndImm32(X14, X14, 0xFF) // keep the fill byte only
	f.a.MovImm64(X12, 0x0101010101010101)
	f.a.Mul64(X14, X14, X12) // replicate the byte across all 8 lanes

	// Small dynamic fills: inline 8-byte pattern stores; large fall through to the
	// byte-fill loop.
	f.cmpImm(X11, smallBulkMax, true)
	bigF := f.a.Bcond(condAE)
	fill8 := f.a.Len()
	f.cmpImm(X11, 8, false)
	f8done := f.a.Bcond(condB)
	f.a.StoreIdx(X9, X11, X14, -8, 8)
	f.a.SubImm32(X11, X11, 8)
	f.a.PatchBranch26(f.a.Branch(), fill8)
	f.a.PatchBranch19(f8done, f.a.Len())
	fillDone := f.a.Cbz64(X11)
	fill1 := f.a.Len()
	f.a.StoreIdx(X9, X11, X14, -1, 1)
	f.a.SubImm32(X11, X11, 1)
	f.a.PatchBranch19(f.a.Cbnz64(X11), fill1)
	skipFill := f.a.Branch()
	f.a.PatchBranch19(bigF, f.a.Len())
	f.fillLoop(X9, X14, X11) // [X9..] = X14[7:0], X11 times
	f.a.PatchBranch26(skipFill, f.a.Len())
	f.a.PatchBranch19(fillDone, f.a.Len())

	f.setDepth(d - 3)
	return nil
}

// memorySize pushes the current linear-memory size in pages.
func (f *fn) memorySize(r *wasm.Reader) error {
	if _, err := r.Byte(); err != nil { // memory index (validated == 0)
		return err
	}
	out := f.allocReg(0)
	f.ld32(out, linMemReg, -int32(bdCurPages))
	f.pushReg(out, mtI32)
	return nil
}

// memoryGrow grows linear memory by the popped page delta, pushing the previous
// size in pages or -1 on failure. The reservation is mapped up front, so this is
// a pure size-cache update (matching the amd64 twin); the base never moves.
func (f *fn) memoryGrow(r *wasm.Reader) error {
	if _, err := r.Byte(); err != nil { // memory index (validated == 0)
		return err
	}
	f.invalidateBoundsCert() // memBytes changes; end the certificate conservatively
	delta := f.materialize(f.popValue())
	f.pinned = f.pinned.add(delta)
	res := f.allocReg(maskOf(delta))
	f.ld32(res, linMemReg, -int32(bdCurPages)) // old pages — the success result
	nw := f.allocReg(maskOf(delta).add(res))
	f.a.MovReg32(nw, res)
	// new = old + delta; ADDS sets the carry flag on u32 overflow. Note x86's `jb`
	// after `add` tests CF=1 (carry out); on AArch64 that is CondCS (carry set),
	// NOT condB (which is CondCC and only correct after a CMP/SUBS).
	f.a.Adds32(nw, nw, delta)
	failOverflow := f.a.Bcond(a64.CondCS)
	mx := f.allocReg(maskOf(delta).add(res).add(nw))
	f.ld32(mx, linMemReg, -int32(bdMaxPages))
	f.cmpRR(nw, mx, false)
	failMax := f.a.Bcond(condA) // new > max
	f.st32(linMemReg, -int32(bdCurPages), nw)
	f.a.MovReg32(mx, nw)
	f.shiftImm(shLSL, mx, wasmPageLog, false) // bytes = pages << 16
	f.st32(linMemReg, -int32(bdCurBytes), mx)
	done := f.a.Branch()
	f.a.PatchBranch19(failOverflow, f.a.Len())
	f.a.PatchBranch19(failMax, f.a.Len())
	f.a.MovImm64(res, uint64(0xffffffff))
	f.a.PatchBranch26(done, f.a.Len())
	if f.memSizeReg != regNone {
		f.ld32(f.memSizeReg, linMemReg, -int32(bdCurBytes)) // refresh the memBytes cache (both paths)
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
// technique), so n bytes are covered by at most 8 chunks for n <= 64.
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
	case n <= 16:
		return [][2]int{{0, 8}, {n - 8, 8}}
	case n <= 24:
		return [][2]int{{0, 8}, {8, 8}, {n - 8, 8}}
	case n <= 32:
		return [][2]int{{0, 8}, {8, 8}, {16, 8}, {n - 8, 8}}
	default:
		chunks := make([][2]int, 0, (n+7)/8)
		for off := 0; off+8 < n; off += 8 {
			chunks = append(chunks, [2]int{off, 8})
		}
		return append(chunks, [2]int{n - 8, 8})
	}
}

// bulkBoundsCheck emits `trap unless base+n <= memBytes` for an unrolled bulk
// op (skipped in guard mode: the stores/loads fault like scalar accesses).
func (f *fn) bulkBoundsCheck(base Reg, n int) {
	if f.guardMode {
		return
	}
	f.pinned = f.pinned.add(base)
	t := f.allocReg(0)
	f.leaDisp(t, base, int32(n), true)
	if f.memSizeReg != regNone {
		f.cmpRR(t, f.memSizeReg, true)
	} else {
		mb := f.allocReg(maskOf(t))
		f.ld32(mb, linMemReg, -int32(bdCurBytes))
		f.cmpRR(t, mb, true)
		f.release(mb)
	}
	f.trapIf(condA, trapMemOOB)
	f.release(t)
	f.pinned = f.pinned.remove(base)
}

// memoryFillConst lowers memory.fill with a small constant length as unrolled
// stores of a byte-replicated pattern — no flush, no fill-loop startup.
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
			v := f.materialize(valElem) // owned: the low-byte mask below mutates it
			f.a.AndImm32(v, v, 0xFF)    // v &= 0xFF (only the low byte matters)
			pat = f.allocReg(maskOf(v))
			f.a.MovImm64(pat, 0x0101010101010101)
			f.a.Mul64(pat, pat, v) // replicate the byte across all 8 lanes
			f.release(v)
		}
		f.pinned = f.pinned.add(pat)
	}
	dst, dstOwned := f.materializeRead(f.popValue())
	f.bulkBoundsCheck(dst, n)
	for _, c := range bulkChunks(n) {
		f.a.StoreIdx(linMemReg, dst, pat, int32(c[0]), c[1])
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
	chunks := bulkChunks(n)
	regs := make([]Reg, len(chunks))
	avoid := maskOf(src, dst)
	for i, c := range chunks {
		r := f.allocReg(avoid)
		f.a.LoadIdx(r, linMemReg, src, int32(c[0]), c[1], false, c[1] == 8)
		regs[i] = r
		avoid = avoid.add(r)
	}
	for i, c := range chunks {
		f.a.StoreIdx(linMemReg, dst, regs[i], int32(c[0]), c[1])
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
