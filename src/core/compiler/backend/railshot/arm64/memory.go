//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"

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
	trapUnreachable        = 1
	trapMemOOB             = 3
	trapIndirectOOB        = 5
	trapIndirectSig        = 6
	trapDivZero            = 9
	trapDivOverflow        = 10
	trapTruncOverflow      = 11
	trapInterrupted        = 12
	trapStackFence         = 13
	trapTailUnsupported    = 15
	trapNullReference      = 16
	trapUnhandledException = 17
	trapCastFailure        = 18
	trapTableOOB           = 19
	trapAtomicUnaligned    = 20
)

// Basedata fields at negative offsets from the linMem base (runtime/basedata.go).
const (
	bdCurPages  = 4                                // u32: current size in 64 KiB pages
	bdCurBytes  = abi.ActualLinMemByteSize64Offset // u64: bounds-check limit
	bdMaxPages  = 12                               // u32: declared/runtime grow ceiling
	wasmPageLog = 16                               // log2(65536)
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
const smallBulkMax = 32

// offTrapCellPtr is the basedata slot holding the address of the trap cell
// (runtime installTrapCell / abi.TrapCellPtrOffset). The trap pointer is NOT
// part of any call ABI: only the cold trap path reads it, so calls and returns
// carry no trap protocol (WARP's model — its passive mode has no trap cell).
const offTrapCellPtr = abi.TrapCellPtrOffset

// offPassiveDataPtr points at the per-instance passive data descriptor array.
// Descriptors are runtime.PassiveDataDescBytes bytes: {ptr u64, len u32, pad u32}.
const offPassiveDataPtr = abi.PassiveDataPtrOffset

func (f *fn) materializeMemoryTrapChain() {
	sc := f.scratchState()
	for f.memoryTrapHead != 0 {
		branch := f.takeMemoryTrapLink()
		sc.trapSites[trapMemOOB] = append(sc.trapSites[trapMemOOB], trapSite{
			branch: compactTrapBranch(branch), source: f.packTrapSource(f.rootTraceFuncIdx, f.memoryTrapPC),
		})
	}
	f.memoryTrapCount = 0
}

const (
	wideTrapSource       = uint32(1 << 31)
	compactTrapPCBits    = 19
	compactTrapPCMask    = uint32(1<<compactTrapPCBits - 1)
	compactTrapFuncLimit = uint32(1 << (31 - compactTrapPCBits))
)

func (f *fn) packWideTrapSource(function, pc uint32) uint32 {
	sc := f.scratchState()
	index := uint32(len(sc.wideTrapSources))
	if uint64(len(sc.wideTrapSources)) >= uint64(wideTrapSource) {
		panic("arm64: trap source table exceeds compact index domain")
	}
	sc.wideTrapSources = append(sc.wideTrapSources, trapSource{function, pc})
	return wideTrapSource | index
}

// memAddr64 checks full-width memory64 address and offset arithmetic. The
// staged memory64 allocation remains bounded to 65,535 pages, but neither u64
// addition may wrap into that range.
func (f *fn) memAddr64(off uint64, size int) (ea Reg, eaOwned bool, borrow int, disp int32) {
	minimumAccess := f.minimumMemoryAccess(0, off, size)
	f.materializePendingTraps()
	e := f.popValue()
	ea, eaOwned = f.materialize(e), true
	borrow, disp = -1, 0
	if off != 0 {
		t := f.allocReg(maskOf(ea))
		f.a.MovImm64(t, off)
		if minimumAccess {
			f.a.Add64(ea, ea, t)
		} else {
			f.a.Adds64(ea, ea, t)
			f.trapIf(condAE, trapMemOOB)
		} // carry set
		f.release(t)
	}
	if minimumAccess {
		f.stats.peep("memory-min-bounds")
		return ea, eaOwned, borrow, disp
	}
	f.pinned = f.pinned.add(ea)
	end := f.allocReg(maskOf(ea))
	f.a.MovImm64(end, uint64(size))
	f.a.Adds64(end, ea, end)
	f.trapIf(condAE, trapMemOOB)
	if f.memSizeReg != regNone {
		f.cmpRR(end, f.memSizeReg, true)
	} else {
		mb := f.allocReg(maskOf(end))
		f.ld64(mb, linMemReg, -bdCurBytes)
		f.cmpRR(end, mb, true)
		f.release(mb)
	}
	f.trapIf(condA, trapMemOOB)
	f.release(end)
	f.pinned = f.pinned.remove(ea)
	return ea, eaOwned, borrow, disp
}

func (f *fn) indexedMemAddr(memoryIndex uint32, off uint64, size int) (base, ea Reg, disp int32) {
	minimumAccess := f.minimumMemoryAccess(memoryIndex, off, size)
	f.materializePendingTraps()
	e := f.popValue()
	loadDefinedLocal := f.loadDefinedAddressLocal(e)
	ea = f.materialize(e)
	if !f.memoryAddr64(memoryIndex) && (!e.st.valueFacts().has(factUpper32Zero) || loadDefinedLocal) {
		f.a.MovReg32(ea, ea)
		if loadDefinedLocal && e.st.valueFacts().has(factUpper32Zero) {
			f.stats.peep("memory-address-load-rename")
		}
	} else if !f.memoryAddr64(memoryIndex) {
		f.stats.peep("memory-address-zext-elim")
		f.phasePadWords++
	}
	disp = 0
	if off != 0 {
		t := f.allocReg(maskOf(ea))
		f.a.MovImm64(t, off)
		if minimumAccess {
			f.a.Add64(ea, ea, t)
		} else {
			f.a.Adds64(ea, ea, t)
			f.trapIf(condAE, trapMemOOB)
		}
		f.release(t)
	}
	if minimumAccess {
		base = f.allocReg(maskOf(ea))
		if memoryIndex == 0 && f.cachedSharedMemoryBase() != regNone {
			f.a.MovReg64(base, f.cachedSharedMemoryBase())
		} else {
			f.ld64(base, linMemReg, -int32(offMemoryDirPtr))
			f.ld64(base, base, int32(memoryIndex)*abi.MemoryDirEntryBytes+abi.MemoryDirBaseOffset)
		}
		f.stats.peep("memory-min-bounds")
		return base, ea, disp
	}
	f.pinned = f.pinned.add(ea)
	dir := regNone
	mb := regNone
	if memoryIndex == 0 && f.cachedSharedMemoryBase() != regNone {
		base = f.allocReg(maskOf(ea))
		f.a.MovReg64(base, f.cachedSharedMemoryBase())
		mb = f.allocReg(maskOf(ea, base))
		f.loadSharedMemoryBytes(mb, base)
	} else {
		dir = f.allocReg(maskOf(ea))
		f.ld64(dir, linMemReg, -int32(offMemoryDirPtr))
		entry := int32(memoryIndex) * abi.MemoryDirEntryBytes
		mb = f.allocReg(maskOf(ea, dir))
		base = f.allocReg(maskOf(ea, dir, mb))
		f.ld64(base, dir, entry+abi.MemoryDirBaseOffset)
		if f.memoryShared(memoryIndex) {
			f.loadSharedMemoryBytes(mb, base)
		} else {
			f.ld64(mb, base, -int32(bdCurBytes))
		}
	}
	end := f.allocReg(maskOf(ea, dir, mb, base))
	f.a.MovImm64(end, uint64(size))
	f.a.Adds64(end, ea, end)
	f.trapIf(condAE, trapMemOOB)
	f.cmpRR(end, mb, true)
	f.trapIf(condA, trapMemOOB)
	f.release(end)
	f.release(mb)
	if dir != regNone {
		f.release(dir)
	}
	f.pinned = f.pinned.remove(ea)
	return base, ea, disp
}

type boundsCert struct {
	kind   uint8
	idx    uint32
	extent int32
}

const maxStraightLineRangeOps = 4096

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
			cold := f.ctrlMerge(&f.ctrl[i])
			return cold != nil && cold.hasLoopSet() && !loopSetsLocal(f.frameLoopSetLocals(&f.ctrl[i]), idx)
		}
	}
	return false // not inside a loop
}

// trapUnlessLE emits `cmp t, mb; b.hi trap-stub` — trap when t > mb.
func (f *fn) trapUnlessLE(t, mb Reg) {
	f.cmpRR(t, mb, true)
	f.trapIf(condA, trapMemOOB)
}

// memoryInit lowers memory.init. The three i32 operands (dst, src, n) are read
// from canonical slots into the scratch registers X9/X10/X11. The source is
// immutable passive data, so a forward byte-copy loop is sufficient.
func (f *fn) memoryInit(r *wasm.Reader) error {
	dataIdx, err := r.U32()
	if err != nil {
		return err
	}
	memoryIndex, err := r.U32()
	if err != nil {
		return err
	}
	if memoryIndex == 0 && f.threadedMemory0 {
		if top := f.s.back(); top != nil && top.elemKind() == ekValue && top.st.kind == stConst {
			if n := uint32(top.st.cval); n <= 64 {
				f.memoryInitConst(dataIdx, int(n))
				return nil
			}
		}
	}
	f.materializePendingLoads()
	f.flush()
	d := f.depth()
	countArg := f.s.back()
	valueArg := f.s.prev(countArg)
	dstArg := f.s.prev(valueArg)
	f.ld64(X9, SP, f.spillOff(dstArg.st.slotIndex()))    // dst offset
	f.ld64(X10, SP, f.spillOff(valueArg.st.slotIndex())) // src offset in passive segment
	f.ld64(X11, SP, f.spillOff(countArg.st.slotIndex())) // n
	if !f.memoryAddr64(memoryIndex) {
		f.a.MovReg32(X9, X9)
	}
	f.a.MovReg32(X10, X10)
	f.a.MovReg32(X11, X11)
	f.absoluteBulkAddr(memoryIndex, X9, X11)

	disp := int32(dataIdx) * 16
	f.ld64(X13, linMemReg, -int32(offPassiveDataPtr)) // descriptor array
	f.ld32(X14, X13, disp+8)                          // current segment length (zero after data.drop)
	f.leaScaled(X12, X10, X11, 0, 0, true)            // src + n
	f.trapUnlessLE(X12, X14)
	f.ld64(X13, X13, disp) // segment base pointer

	f.a.Add64(X10, X10, X13) // absolute src
	f.copyFwdLoop(X9, X10, X11)

	f.setDepth(d - 3)
	return nil
}

// memoryInitConst retains live values in their allocated registers instead of
// flushing the entire operand stack. Both checks precede all destination writes.
func (f *fn) memoryInitConst(dataIdx uint32, n int) {
	f.stats.peep("meminit-unroll")
	f.materializePendingLoads()
	f.erase(f.s.back())
	callerPins := f.pinned
	src := f.materialize(f.popValue())
	f.pinned = f.pinned.add(src)
	dst := f.materialize(f.popValue())
	f.pinned = f.pinned.add(dst)
	f.a.MovReg32(src, src)
	if !f.memoryAddr64(0) {
		f.a.MovReg32(dst, dst)
	}
	f.bulkBoundsCheck(dst, n, 0)
	desc := f.allocReg(maskOf(src, dst))
	f.pinned = f.pinned.add(desc)
	f.ld64(desc, linMemReg, -int32(offPassiveDataPtr))
	length := f.allocReg(maskOf(src, dst, desc))
	f.pinned = f.pinned.add(length)
	f.ld32(length, desc, int32(dataIdx)*16+8)
	end := f.allocReg(maskOf(src, dst, desc, length))
	f.leaDisp(end, src, int32(n), true)
	f.trapUnlessLE(end, length)
	f.release(end)
	f.pinned = f.pinned.remove(length)
	f.release(length)
	if n != 0 {
		base, owned := f.indexedMemoryBase(0, maskOf(src, dst, desc))
		f.a.Add64(dst, dst, base)
		if owned {
			f.release(base)
		}
		f.ld64(desc, desc, int32(dataIdx)*16)
		f.a.Add64(src, src, desc)
	}
	f.pinned = f.pinned.remove(desc)
	f.release(desc)
	off := 0
	if n >= 16 {
		vec := f.allocFReg(0)
		for ; off+16 <= n; off += 16 {
			f.a.LdrQ(vec, src, int32(off))
			f.a.StrQ(dst, int32(off), vec)
		}
		f.releaseF(vec)
	}
	if off < n {
		scratch := f.allocReg(maskOf(src, dst))
		for off < n {
			size := 8
			for size > n-off {
				size >>= 1
			}
			switch size {
			case 8:
				f.a.Load64(scratch, src, uint32(off))
				f.a.Store64(scratch, dst, uint32(off))
			case 4:
				f.a.Load32(scratch, src, uint32(off))
				f.a.Store32(scratch, dst, uint32(off))
			case 2:
				f.a.Ldrh(scratch, src, uint32(off))
				f.a.Strh(scratch, dst, uint32(off))
			case 1:
				f.a.Ldrb(scratch, src, uint32(off))
				f.a.Strb(scratch, dst, uint32(off))
			}
			off += size
		}
		f.release(scratch)
	}
	f.pinned = callerPins
	f.release(src)
	f.release(dst)
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

var sharedMemoryBaseEnabled = true

// loadSharedMemoryBytes acquires the backing's authoritative size, rather
// than an instance directory snapshot that another grower can make stale.
func (f *fn) loadSharedMemoryBytes(dst, base Reg) {
	f.a.SubImm64(dst, base, uint32(bdCurBytes))
	f.a.Ldar(dst, dst, 8)
}

func (f *fn) sharedMemorySize(memoryIndex uint32) {
	out := f.allocReg(0)
	if base := f.cachedSharedMemoryBase(); memoryIndex == 0 && base != regNone {
		f.loadSharedMemoryBytes(out, base)
	} else {
		f.ld64(out, linMemReg, -int32(offMemoryDirPtr))
		f.ld64(out, out, int32(memoryIndex)*abi.MemoryDirEntryBytes+abi.MemoryDirBaseOffset)
		f.loadSharedMemoryBytes(out, out)
	}
	f.a.LsrImm(out, out, 16, false)
	if f.memoryAddr64(memoryIndex) {
		f.pushReg(out, mtI64)
	} else {
		f.pushReg(out, mtI32)
	}
}

func (f *fn) sharedMemoryGrow(memoryIndex uint32) error {
	f.materializePendingLoads()
	f.materializePendingTraps()
	f.invalidateBoundsCert()
	if top := f.s.back(); top != nil && top.elemKind() == ekValue && top.st.kind == stConst && (top.st.cval == 0 || !f.memoryAddr64(memoryIndex) && uint32(top.st.cval) == 0) {
		f.dropValue()
		f.sharedMemorySize(memoryIndex)
		return nil
	}
	typ := wasm.I32
	if f.memoryAddr64(memoryIndex) {
		typ = wasm.I64
	}
	params := []wasm.ValType{typ}
	if memoryIndex != 0 {
		params = append(params, wasm.I32)
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(memoryIndex)})
	}
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: params, Results: []wasm.ValType{typ}}
	return f.callHostSync(int(shared.AtomicWaitDispatchBit|shared.AtomicHelperMemoryGrow), ft)
}

func (f *fn) bulkDynamicBoundsCheck(base, n Reg, memoryIndex uint32) {
	f.a.Adds64(X12, base, n)
	f.trapIf(condAE, trapMemOOB)
	if f.memSizeReg != regNone && memoryIndex == 0 {
		f.cmpRR(X12, f.memSizeReg, true)
	} else {
		f.ld64(X13, linMemReg, -int32(bdCurBytes))
		f.cmpRR(X12, X13, true)
	}
	f.trapIf(condA, trapMemOOB)
}
