//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// memAccessSize returns the byte width of a plain scalar memory instruction.
func memAccessSize(op byte) int {
	switch op {
	case 0x2c, 0x2d, 0x30, 0x31, 0x3a, 0x3c:
		return 1
	case 0x2e, 0x2f, 0x32, 0x33, 0x3b, 0x3d:
		return 2
	case 0x28, 0x2a, 0x34, 0x35, 0x36, 0x38, 0x3e:
		return 4
	case 0x29, 0x2b, 0x37, 0x39:
		return 8
	default:
		return 0
	}
}

// Linear-memory access: scalar loads/stores with a linear bounds check, plus
// memory.size/grow. Ported from WARP's memory lowering, adapted to wago's runtime
// memory ABI (the same one src/core/encoder/amd64 targets): the linear-memory base is
// pinned in RBX for the whole function, and a small "basedata" header sits at
// negative offsets from that base.

// Trap codes — must match jit.TrapCode / the values the engine reads (identical
// to src/core/encoder/amd64's table).
const (
	trapUnreachable        = 1
	trapBuiltin            = 2
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
	trapMax                = trapAtomicUnaligned
)

// Basedata fields at negative offsets from the linMem base (runtime/basedata.go).
const (
	bdCurPages  = 4                                // u32: current size in 64 KiB pages
	bdCurBytes  = abi.ActualLinMemByteSize64Offset // u64: bounds-check limit
	bdMaxPages  = 12                               // u32: declared/runtime grow ceiling
	wasmPageLog = 16                               // log2(65536)
)

// offTrapStackReentry is the linMem-relative slot (bytes below the linMem base)
// where the trampoline stashes the entry SP for handler-jump trap unwinding —
// see runtime/basedata.go offTrapStackReentry.
const offTrapStackReentry = 24

// smallBulkMax is the dynamic memory.copy/fill length below which the inline
// chunk loops beat `rep movs/stos` startup latency.
const smallBulkMax = 96

type rcxZeroSite struct {
	off     int
	compact bool
}

func (f *fn) rcxZero32Placeholder() rcxZeroSite {
	if f.policy.CompactNative {
		f.stats.peep("direct-jecxz")
		return rcxZeroSite{off: f.a.JcxzPlaceholder(false), compact: true}
	}
	f.a.TestSelf(RCX, false)
	return rcxZeroSite{off: f.a.JccPlaceholder(condE)}
}

func (f *fn) closeRCXZero32Loop(site rcxZeroSite, loop int) {
	if site.compact {
		if !f.a.JccRel8(condNE, loop) {
			panic("amd64: bounded byte-tail JNE exceeded rel8 range")
		}
		return
	}
	f.a.PatchRel32(f.a.JccPlaceholder(condNE), loop)
}

func (f *fn) patchRCXZero32(site rcxZeroSite) {
	if site.compact {
		if !f.a.PatchRel8(site.off, f.a.Len()) {
			panic("amd64: bounded JECXZ target exceeded rel8 range")
		}
		return
	}
	f.a.PatchRel32(site.off, f.a.Len())
}

// offTrapCellPtr is the basedata slot holding the address of the trap cell
// (runtime installTrapCell / abi.TrapCellPtrOffset). The trap pointer is NOT
// part of any call ABI: only the cold trap path reads it, so calls and returns
// carry no trap protocol (WARP's model — its passive mode has no trap cell).
const offTrapCellPtr = abi.TrapCellPtrOffset

// offPassiveDataPtr points at the per-instance passive data descriptor array.
// Descriptors are runtime.PassiveDataDescBytes bytes: {ptr u64, len u32, pad u32}.
const offPassiveDataPtr = abi.PassiveDataPtrOffset

// offMemoryDirPtr points at abi.MemoryDirEntryBytes indexed-memory entries.
// Memory 0 uses only the optional quota field on its memory.grow cold path.
const offMemoryDirPtr = abi.MemoryDirPtrOffset

// emitTrap writes the logical Wasm PC from RAX and the function index argument,
// then writes the trap code to the trap buffer (via [linMem-offTrapCellPtr]) and
// unwinds the
// ENTIRE native call tree in one jump: it restores RSP to the entry SP the
// trampoline recorded at [linMem-offTrapStackReentry] and RETs straight back into
// enterNative (WARP's handler-jump model). This is what lets callers skip the
// per-call "load *trap; test; branch" check — a trap never returns through an
// intermediate frame. Terminal, so it may freely clobber RSI (and RSP last).
func (f *fn) emitTrap(code, function uint32) {
	f.a.Load64(RSI, RBX, -offTrapCellPtr)
	f.a.StoreImm32Mem(RSI, 16, int32(function+1))
	f.a.Store32(RSI, 20, RAX)
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
	f.sc.trapSites[code] = append(f.sc.trapSites[code], f.trapSite(f.a.JccPlaceholder(cc)))
}

// trapAlways is trapIf's unconditional form (`unreachable`): a 5-byte jmp to the
// shared stub instead of the inline 20-byte trap block.
func (f *fn) trapAlways(code uint32) {
	f.sc.trapSites[code] = append(f.sc.trapSites[code], f.trapSite(f.a.JmpPlaceholder()))
}

func (f *fn) trapSite(branch int) trapSite {
	return trapSite{branch: compactTrapBranch(branch), function: f.traceFuncIdx, pc: f.wasmPC}
}

func compactTrapBranch(branch int) uint32 {
	if branch < 0 || uint64(branch) >= uint64(^uint32(0)) {
		panic("amd64: trap branch offset exceeds 32-bit function domain")
	}
	return uint32(branch)
}

// emitTrapStubs emits one trap stub per trap code used by this function and
// patches every recorded site to it. Called once, after the epilogue.
func (f *fn) emitTrapStubs() {
	before := f.a.Len()
	defer func() { f.stats.addGCTrapStubBytes(f.a.Len() - before) }()
	groups := 0
	for code := uint32(1); code <= trapMax; code++ {
		sites := f.sc.trapSites[code]
		if len(sites) == 0 {
			continue
		}
		sortTrapSitesByFunction(sites)
		groups++
		for i := 1; i < len(sites); i++ {
			if sites[i].function != sites[i-1].function {
				groups++
			}
		}
	}
	if f.opt(optSharedTrapBody) && groups >= 3 &&
		f.policy.CompactNative {
		f.emitSharedTrapStubs(groups)
		f.stats.peep("shared-trap-body")
		return
	}
	for code := uint32(1); code <= trapMax; code++ { // deterministic order
		sites := f.sc.trapSites[code]
		if len(sites) == 0 {
			continue
		}
		f.stats.addTrapStub()
		// Inlining can interleave sites attributed to many source functions. Sort
		// once so grouping and patching are linear instead of repeatedly rescanning
		// the complete site list for every distinct function.
		for start := 0; start < len(sites); {
			end := start + 1
			for end < len(sites) && sites[end].function == sites[start].function {
				end++
			}
			group := sites[start:end]
			f.stats.addTrapGroup()
			first := group[0]
			commonJump := -1
			if len(group) == 1 {
				pos := f.a.Len()
				f.a.MovImm32(RAX, int32(first.pc))
				commonJump = f.a.JmpPlaceholder()
				f.a.PatchRel32(int(first.branch), pos)
			}
			common := f.a.Len()
			if len(group) != 1 {
				f.a.MovImm32(RAX, -1)
				for _, site := range group {
					f.a.PatchRel32(int(site.branch), common)
				}
			}
			f.storeModuleGlobals(RSI)
			f.emitTrap(code, first.function)
			if commonJump >= 0 {
				f.a.PatchRel32(commonJump, common)
			}
			start = end
		}
	}
}

func (f *fn) emitSharedTrapStubs(groupCount int) {
	emitted := 0
	for code := uint32(1); code <= trapMax; code++ {
		sites := f.sc.trapSites[code]
		if len(sites) == 0 {
			continue
		}
		f.stats.addTrapStub()
		for start := 0; start < len(sites); {
			end := start + 1
			for end < len(sites) && sites[end].function == sites[start].function {
				end++
			}
			group := sites[start:end]
			first := group[0]
			pos := f.a.Len()
			pc := int32(-1)
			if len(group) == 1 {
				pc = int32(first.pc)
			}
			f.a.MovImm32(RAX, pc)
			f.a.MovImm32(RCX, int32(first.function+1))
			f.a.MovImm32(RDX, int32(code))
			for _, site := range group {
				f.a.PatchRel32(int(site.branch), pos)
			}
			f.stats.addTrapGroup()
			emitted++
			if emitted < groupCount {
				group[0].branch = compactTrapBranch(f.a.JmpPlaceholder())
			} else {
				group[0].branch = ^uint32(0)
			}
			start = end
		}
	}

	common := f.a.Len()
	f.trapBodyOff = common
	f.storeModuleGlobals(RSI)
	f.a.Load64(RSI, RBX, -offTrapCellPtr)
	f.a.Store32(RSI, 16, RCX)
	f.a.Store32(RSI, 20, RAX)
	f.a.Store32(RSI, 0, RDX)
	f.a.Load64(RSP, RBX, -offTrapStackReentry)
	f.a.Ret()
	f.trapBodyEnd = f.a.Len()

	for code := uint32(1); code <= trapMax; code++ {
		sites := f.sc.trapSites[code]
		for start := 0; start < len(sites); {
			end := start + 1
			for end < len(sites) && sites[end].function == sites[start].function {
				end++
			}
			if sites[start].branch != ^uint32(0) {
				f.a.PatchRel32(int(sites[start].branch), common)
			}
			start = end
		}
	}
}

// sortTrapSitesByFunction uses an allocation-free heap sort. The order within
// one function is irrelevant: a singleton keeps its exact PC, while a shared
// group deliberately records an ambiguous PC.
func sortTrapSitesByFunction(sites []trapSite) {
	sift := func(root, end int) {
		for {
			child := root*2 + 1
			if child >= end {
				return
			}
			if child+1 < end && sites[child].function < sites[child+1].function {
				child++
			}
			if sites[root].function >= sites[child].function {
				return
			}
			sites[root], sites[child] = sites[child], sites[root]
			root = child
		}
	}
	for root := len(sites)/2 - 1; root >= 0; root-- {
		sift(root, len(sites))
	}
	for end := len(sites) - 1; end > 0; end-- {
		sites[0], sites[end] = sites[end], sites[0]
		sift(0, end)
	}
}

// memAddr pops the address operand, folds the static memarg offset, emits the
// bounds check (unless guard-page mode elides it), and returns the register
// holding the effective offset plus the displacement to fold into the access.
// aliasPinned lets a pinned-local address be used in place (no copy) — only
// valid when the access is emitted immediately (stores), not deferred (loads);
// eaOwned reports whether the caller must release ea.
func (f *fn) memAddr(off uint32, size int, aliasPinned bool, rangeExtent int32) (ea Reg, eaOwned bool, borrow int, disp int32) {
	e := f.popValue()
	cleanAddress := f.cleanMemory32Address(e)
	// Bounds-certificate source: the address's stable value carrier (a local or
	// global index), captured before materialization. A temp/computed base has no
	// stable key. See boundsCertMeasure.
	bcKind, bcIdx := boundsSource(e.st)
	disp = 0
	borrow = -1
	leaDisp := int32(size)
	needAdd := int64(off)+int64(size) > 0x7FFFFFFF && off != 0
	if aliasPinned && !needAdd {
		ea, eaOwned = f.materializeRead(e) // a pinned local's reg is read in place
		if !eaOwned {
			borrow = e.st.index()
		}
	} else {
		ea, eaOwned = f.materialize(e), true
	}
	// Host results and canonical spill slots are 64-bit ABI words. Establish the
	// memory32 consuming-side invariant before any native-width arithmetic.
	if cleanAddress {
		f.stats.peep("addr-zext-elim")
	} else {
		f.a.MovRegReg32(ea, ea)
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
	// A bounded lookahead can prove that this is the first potentially trapping
	// operation before later direct loads from the same stable base. Check their
	// largest extent now: moving the trap across local-only, non-trapping integer
	// work and other plain loads is unobservable, and the larger certificate
	// removes all intervening checks. Stores, calls, control, and non-memory traps
	// are hard barriers in straightLineLoadExtent, preserving Wasm trap ordering.
	if rangeExtent > leaDisp {
		leaDisp = rangeExtent
	}
	f.boundsCertUpdate(bcKind, bcIdx, leaDisp)
	if bcKind != 0 && f.inLoop() {
		f.stats.addBoundsInLoop()
	}
	f.pinned = f.pinned.add(ea)
	t := f.allocReg(0)
	f.a.LeaDisp(t, ea, leaDisp) // t = ea + off + size
	if f.memSizeReg != regNone {
		f.a.Cmp64(t, f.memSizeReg) // memBytes lives in a register (WARP REGS::memSize)
	} else {
		mb := f.allocReg(maskOf(t))
		f.a.Load64(mb, RBX, -bdCurBytes) // memory size in bytes
		f.a.Cmp64(t, mb)
		f.release(mb)
	}
	f.trapIf(condA, trapMemOOB) // out of bounds when ea+off+size > memBytes
	f.release(t)
	f.pinned = f.pinned.remove(ea)
	return ea, eaOwned, borrow, disp
}

type boundsCert struct {
	kind   uint8
	idx    uint32
	extent int32
}

func boundsSource(s storage) (kind uint8, idx uint32) {
	switch s.kind {
	case stLocalReg, stLocalRef:
		return 1, uint32(s.idx)
	case stGlobReg:
		return 2, uint32(s.idx)
	default:
		return 0, 0
	}
}

const maxStraightLineRangeOps = 4096

// straightLineLoadExtent finds later direct scalar loads from the same stable
// source that may safely share the current access's bounds check. The scan is
// deliberately narrow: only local state and non-trapping integer operations
// may separate the loads. Anything effectful, control-flowing, potentially
// trapping, or difficult to classify ends the range. That makes it safe to use
// the largest discovered extent at the current load without changing observable
// trap order.
func (f *fn) straightLineLoadExtent(r *wasm.Reader, kind uint8, idx uint32, extent int32) int32 {
	scan := *r
	prevKind, prevIdx := uint8(0), uint32(0)
	for n := 0; n < maxStraightLineRangeOps && scan.HasNext(); n++ {
		op, err := scan.Byte()
		if err != nil {
			break
		}
		nextKind, nextIdx := uint8(0), uint32(0)
		switch {
		case op == 0x20: // local.get
			x, err := scan.U32()
			if err != nil {
				return extent
			}
			nextKind, nextIdx = 1, x
		case op == 0x23: // global.get
			x, err := scan.U32()
			if err != nil {
				return extent
			}
			nextKind, nextIdx = 2, x
		case op == 0x21 || op == 0x22: // local.set / local.tee
			x, err := scan.U32()
			if err != nil || (kind == 1 && x == idx) {
				return extent
			}
		case op >= 0x28 && op <= 0x35: // scalar loads
			size := memAccessSize(op)
			align, err := scan.U32()
			if err != nil {
				return extent
			}
			memoryIndex := uint32(0)
			if align >= 64 && align < 128 {
				memoryIndex, err = scan.U32()
				if err != nil {
					return extent
				}
			}
			off, err := scan.U32() // this helper is only used for memory32
			if err != nil || memoryIndex != 0 {
				return extent
			}
			if prevKind == kind && prevIdx == idx {
				if candidate := int64(off) + int64(size); candidate > 0x7fffffff {
					return extent
				} else if int32(candidate) > extent {
					extent = int32(candidate)
				}
			}
		case op == 0x01 || op == 0x1a || op == 0x1b, // nop, drop, select
			op == 0x41 || op == 0x42,               // integer constants
			op >= 0x45 && op <= 0x6c,               // integer tests/arithmetic before div/rem
			op >= 0x71 && op <= 0x7e,               // i32 bit ops + i64 clz/ctz/popcnt/add/sub/mul
			op >= 0x83 && op <= 0x8a,               // i64 bit ops
			op == 0xa7 || op == 0xac || op == 0xad: // non-trapping integer conversions
			if err := skipImmediates(&scan, op); err != nil {
				return extent
			}
		default:
			return extent
		}
		prevKind, prevIdx = nextKind, nextIdx
	}
	return extent
}

// memAddr64 is the bounded memory64 counterpart to memAddr. The staged memory64
// runtime still reserves at most 65,535 pages, but addresses and static offsets
// are full u64 values. Both additions are checked for carry before comparing against the
// zero-extended byte-size cache, so wraparound cannot turn an OOB access valid.
func (f *fn) memAddr64(off uint64, size int) (ea Reg, eaOwned bool, borrow int, disp int32) {
	e := f.popValue()
	ea, eaOwned = f.materialize(e), true
	borrow, disp = -1, 0
	if off != 0 {
		t := f.allocReg(maskOf(ea))
		f.a.MovImm64(t, off)
		f.a.Add64(ea, t)
		f.trapIf(condB, trapMemOOB)
		f.release(t)
	}
	f.pinned = f.pinned.add(ea)
	t := f.allocReg(maskOf(ea))
	f.a.MovReg64(t, ea)
	f.a.AluRI(0, t, int32(size), true)
	f.trapIf(condB, trapMemOOB)
	if f.memSizeReg != regNone {
		f.a.Cmp64(t, f.memSizeReg)
	} else {
		mb := f.allocReg(maskOf(t))
		f.a.Load64(mb, RBX, -bdCurBytes)
		f.a.Cmp64(t, mb)
		f.release(mb)
	}
	f.trapIf(condA, trapMemOOB)
	f.release(t)
	f.pinned = f.pinned.remove(ea)
	return ea, eaOwned, borrow, disp
}

// boundsCertCovers reports whether the active straight-line certificate already
// proves this access in-bounds (P6.1): the same keyable source, with this
// access's extent (off+size) within the proven extent. A check proves
// source+extent <= memBytes; memBytes only ever grows, so a later access on the
// same source value with a smaller-or-equal extent is in bounds. The certificate
// set is dropped at flush/flushBelow (every call + control join) and memory.grow;
// setting one certified source drops only that source's entry.
func (f *fn) boundsCertCovers(kind uint8, idx uint32, extent int32) bool {
	if kind == 0 {
		return false
	}
	if !f.opt(optMultiBoundsCert) {
		c := &f.boundsCerts[0]
		return c.kind == kind && c.idx == idx && extent <= c.extent
	}
	for i := range f.boundsCerts {
		c := &f.boundsCerts[i]
		if c.kind == kind && c.idx == idx {
			return extent <= c.extent
		}
	}
	return false
}

// boundsCertUpdate records the check about to be emitted. The tiny round-robin
// set covers the common two/three-array loop while keeping compiler state fixed.
func (f *fn) boundsCertUpdate(kind uint8, idx uint32, extent int32) {
	if !f.opt(optMultiBoundsCert) {
		c := &f.boundsCerts[0]
		if kind == 0 {
			*c = boundsCert{}
		} else if c.kind == kind && c.idx == idx {
			if extent > c.extent {
				c.extent = extent
			}
		} else {
			*c = boundsCert{kind: kind, idx: idx, extent: extent}
		}
		return
	}
	if kind == 0 {
		return
	}
	for i := range f.boundsCerts {
		c := &f.boundsCerts[i]
		if c.kind == kind && c.idx == idx {
			if extent > c.extent {
				c.extent = extent
			}
			return
		}
	}
	for i := range f.boundsCerts {
		if f.boundsCerts[i].kind == 0 {
			f.boundsCerts[i] = boundsCert{kind: kind, idx: idx, extent: extent}
			return
		}
	}
	i := int(f.nextBoundsCert) % len(f.boundsCerts)
	f.boundsCerts[i] = boundsCert{kind: kind, idx: idx, extent: extent}
	f.nextBoundsCert++
}

// invalidateBoundsCert drops every straight-line bounds certificate.
func (f *fn) invalidateBoundsCert() {
	for i := range f.boundsCerts {
		f.boundsCerts[i] = boundsCert{}
	}
	f.nextBoundsCert = 0
}

// invalidateBoundsCertFor drops the proof for one source whose value changed.
func (f *fn) invalidateBoundsCertFor(kind uint8, idx uint32) {
	for i := range f.boundsCerts {
		c := &f.boundsCerts[i]
		if c.kind == kind && c.idx == idx {
			*c = boundsCert{}
		}
	}
}

// inLoop reports whether any enclosing control frame is a loop.
func (f *fn) inLoop() bool {
	for i := range f.ctrl {
		if f.ctrl[i].kind == cfLoop {
			return true
		}
	}
	return false
}

func (f *fn) memoryAddr64(memoryIndex uint32) bool {
	mt, ok := f.memoryType(memoryIndex)
	return ok && mt.Limits.Addr64
}

func (f *fn) readMemArg(r *wasm.Reader) (memoryIndex uint32, off uint64, err error) {
	align, err := r.U32()
	if err != nil {
		return 0, 0, err
	}
	if align >= 64 && align < 128 {
		memoryIndex, err = r.U32()
		if err != nil {
			return 0, 0, err
		}
	}
	if f.memoryAddr64(memoryIndex) {
		off, err = r.U64()
	} else {
		var off32 uint32
		off32, err = r.U32()
		off = uint64(off32)
	}
	return memoryIndex, off, err
}

func (f *fn) indexedMemAddr(memoryIndex uint32, off uint64, size int) (base, ea Reg, disp int32) {
	e := f.popValue()
	cleanAddress := f.cleanMemory32Address(e)
	ea = f.materialize(e)
	addr64 := f.memoryAddr64(memoryIndex)
	if !addr64 {
		if cleanAddress {
			f.stats.peep("addr-zext-elim")
		} else {
			f.a.MovRegReg32(ea, ea)
		}
		off32 := uint32(off)
		disp = int32(off32)
		if int64(off32)+int64(size) > 0x7fffffff {
			t := f.allocReg(maskOf(ea))
			f.a.MovImm32(t, int32(off32))
			f.a.Add64(ea, t)
			f.release(t)
			disp = 0
		}
	} else if off != 0 {
		// Indexed memory64 accesses need the same full-width carry proof as
		// memory 0. Truncating the memarg or using a wrapping LEA would let
		// addresses such as UINT64_MAX wrap into the finite reservation.
		t := f.allocReg(maskOf(ea))
		f.a.MovImm64(t, off)
		f.a.Add64(ea, t)
		f.trapIf(condB, trapMemOOB)
		f.release(t)
	}
	f.pinned = f.pinned.add(ea)
	base = f.allocReg(maskOf(ea))
	f.a.Load64(base, RBX, -offMemoryDirPtr)
	entry := int32(memoryIndex) * abi.MemoryDirEntryBytes
	mb := f.allocReg(maskOf(ea).add(base))
	f.a.Load64(mb, base, entry+abi.MemoryDirCurrentBytesOffset)
	f.a.Load64(base, base, entry+abi.MemoryDirBaseOffset)
	t := f.allocReg(maskOf(ea).add(base).add(mb))
	if addr64 {
		f.a.MovReg64(t, ea)
		f.a.AluRI(0, t, int32(size), true)
		f.trapIf(condB, trapMemOOB)
	} else {
		f.a.LeaDisp(t, ea, disp+int32(size))
	}
	f.a.Cmp64(t, mb)
	f.trapIf(condA, trapMemOOB)
	f.release(t)
	f.release(mb)
	f.pinned = f.pinned.remove(ea)
	return base, ea, disp
}

// cleanMemory32Address reports concrete storage forms whose materialization
// necessarily writes a 32-bit destination. Borrowed local/global registers,
// spill slots, and deferred operations are deliberately excluded: their
// native-width carriers may have nonzero high bits, including after local.tee,
// a sign-extending narrow load, or an identity-folded deferred operation.
func (f *fn) cleanMemory32Address(e *elem) bool {
	if !f.opt(optAddrZExtElim) || e == nil {
		return false
	}
	if !e.isValue() || e.st.typ != mtI32 {
		return false
	}
	switch e.st.kind {
	case stConst, stLocalRef:
		return true
	}
	return false
}

// memLoad lowers a scalar load of `size` bytes. signed selects sign-extension;
// wide selects an i64 result (so signed sub-width loads extend to all 64 bits).
func (f *fn) memLoad(r *wasm.Reader, size int, signed, wide bool) error {
	memoryIndex, off, err := f.readMemArg(r)
	if err != nil {
		return err
	}
	if memoryIndex != 0 {
		f.invalidateStoreForward()
		base, ea, disp := f.indexedMemAddr(memoryIndex, off, size)
		out := f.allocReg(maskOf(base).add(ea))
		f.a.LoadIdx(out, base, ea, disp, size, signed, wide)
		f.release(base)
		f.release(ea)
		var e *elem
		if wide {
			e = f.pushReg(out, mtI64)
		} else {
			e = f.pushReg(out, mtI32)
		}
		if f.opt(optValueFacts) && !wide {
			e.st.setValueFacts(factUpper32Zero)
		}
		return nil
	}
	if f.memoryAddr64(0) {
		f.invalidateStoreForward()
		ea, eaOwned, borrow, disp := f.memAddr64(off, size)
		st := memRefStorage(ea, disp, size, signed, wide, borrow)
		if f.opt(optValueFacts) && !wide {
			st.setValueFacts(factUpper32Zero)
		}
		e := f.pushValue(st)
		if eaOwned {
			f.regUser[ea] = e
		}
		return nil
	}
	off32 := uint32(off)
	if f.forwardStoredLoad(off32, size, signed, wide) {
		return nil
	}
	f.invalidateStoreForward()
	rangeExtent := int32(0)
	// Do not move a later extent proof earlier for shared memory: another agent
	// may grow it between the two loads, so an early check could spuriously trap.
	if f.boundsFacts && !f.guardMode && !f.threadedMemory0 && int64(off32)+int64(size) <= 0x7fffffff {
		if top := f.s.back(); top != nil && top.isValue() {
			kind, idx := boundsSource(top.st)
			currentExtent := int32(off32) + int32(size)
			if kind != 0 && !f.boundsCertCovers(kind, idx, currentExtent) {
				rangeExtent = f.straightLineLoadExtent(r, kind, idx, currentExtent)
			}
		}
	}
	// The address may read a pinned local's register in place (WARP
	// liftToRegInPlace): the deferred load records the borrow so a local.set of
	// that local realizes the load first, and consumers neither write nor
	// release the register.
	ea, eaOwned, borrow, disp := f.memAddr(off32, size, true, rangeExtent)
	// Defer the load: push a bounds-checked memory reference (the mov is emitted
	// when the value is materialized, or folded as an r/m operand into a consumer).
	st := memRefStorage(ea, disp, size, signed, wide, borrow)
	if f.opt(optValueFacts) && !wide {
		// Every i32 load writes a 32-bit destination, including sign-extending
		// byte/word forms, so the physical register upper half is known zero.
		st.setValueFacts(factUpper32Zero)
	}
	e := f.pushValue(st)
	if eaOwned {
		f.regUser[ea] = e // an owned address register belongs to the deferred load
	}
	return nil
}

// memStore lowers a scalar store of `size` bytes.
func (f *fn) memStore(r *wasm.Reader, size int) error {
	memoryIndex, off, err := f.readMemArg(r)
	if err != nil {
		return err
	}
	if memoryIndex != 0 {
		f.materializePendingLoads()
		f.invalidateStoreForward()
		value := f.materialize(f.popValue())
		f.pinned = f.pinned.add(value)
		base, ea, disp := f.indexedMemAddr(memoryIndex, off, size)
		f.a.StoreIdx(base, ea, value, disp, size)
		f.release(base)
		f.release(ea)
		f.pinned = f.pinned.remove(value)
		f.release(value)
		return nil
	}
	if f.memoryAddr64(0) {
		f.materializePendingLoads()
		f.invalidateStoreForward()
		value := f.materialize(f.popValue())
		f.pinned = f.pinned.add(value)
		ea, eaOwned, _, disp := f.memAddr64(off, size)
		f.a.StoreIdx(RBX, ea, value, disp, size)
		if eaOwned {
			f.release(ea)
		}
		f.pinned = f.pinned.remove(value)
		f.release(value)
		return nil
	}
	off32 := uint32(off)
	f.materializePendingLoads() // deferred loads must read pre-store memory
	// A constant value stores as an immediate directly (selectInstr's `mov r/m,
	// imm` form) — no register, no load-then-store dependency chain. i64 needs
	// two 4-byte immediate stores (low32 at disp, high32 at disp+4): a single
	// 64-bit imm-store sign-extends imm32, which is wrong for an arbitrary
	// 64-bit pattern; narrower stores truncate to the low `size` bytes exactly
	// like a materialized constant would (i64.store8/16/32 route here too).
	if top := f.s.back(); top != nil && top.isValue() && top.st.kind == stConst {
		f.stats.peep("store-imm")
		v := top.st.cval
		f.erase(top)
		ea, eaOwned, _, disp := f.memAddr(off32, size, true, 0)
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
	// An integer comparison immediately consumed by an 8-bit store needs only its
	// low byte. Keep SETcc's upper-register garbage dead and omit MOVZX; the byte
	// store cannot observe it. Pending loads were materialized above, preserving
	// pre-store reads and trap order before this dedicated sink condenses the tree.
	if top := f.s.back(); size == 1 && f.opt(optStore8Flags) && isFusableCompare(top) && !top.valueType().isFloat() {
		// condenseToFlags may recursively lower div/rem or a variable shift. Those
		// paths temporarily claim and then unpin x86's fixed-role registers; because
		// the pin mask is not reference-counted, nesting would drop this outer
		// reservation. Keep the byte result in a neutral scratch instead.
		vreg := f.allocReg(maskOf(RAX, RDX, RCX))
		f.pinned = f.pinned.add(vreg)
		cc := f.condenseToFlags(top)
		f.stats.reclassifyPeep("cmp-branch-fuse", "store8-flags")
		f.a.SetccReg8(cc, vreg)
		ea, eaOwned, _, disp := f.memAddr(off32, size, true, 0)
		f.a.StoreIdx(RBX, ea, vreg, disp, size)
		f.pinned = f.pinned.remove(vreg)
		if eaOwned {
			f.release(ea)
		}
		f.release(vreg)
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
	ea, eaOwned, _, disp := f.memAddr(off32, size, true, 0)
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
		f.nextLoadMatchesStore(r, addrLocal, off32, size, vtyp) {
		f.storeFwd = storeForward{valid: true, reg: vreg, typ: vtyp, local: addrLocal, offset: off32, size: size}
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
	if e == nil || !e.isValue() {
		return 0, false
	}
	switch e.st.kind {
	case stLocalReg, stLocalRef:
		return e.st.index(), true
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

func (f *fn) trapTableUnlessLE(t, limit Reg) {
	f.a.Cmp64(t, limit)
	f.trapIf(condA, trapTableOOB)
}

// absoluteBulkAddr checks offset+n against one exact memory and turns offset
// into an absolute native pointer. Callers flush first and reserve RDI/RSI/RCX
// for operands; this helper uses only the fixed RDX/R8 scratch pair. Memory64
// checks the full u64 addition for carry before comparing with the bounded u32
// byte-size cache; memory32 retains its existing instruction sequence.
func (f *fn) absoluteBulkAddr(memoryIndex uint32, offset, n Reg) {
	if f.memoryAddr64(memoryIndex) {
		f.a.MovReg64(RDX, offset)
		f.a.Add64(RDX, n)
		f.trapIf(condB, trapMemOOB)
	} else {
		f.a.LeaScaled(RDX, offset, n, 0, 0)
	}
	if memoryIndex == 0 {
		if f.memSizeReg != regNone {
			f.trapUnlessLE(RDX, f.memSizeReg)
		} else {
			f.a.Load64(R8, RBX, -bdCurBytes)
			f.trapUnlessLE(RDX, R8)
		}
		f.a.Add64(offset, RBX)
		return
	}
	entry := int32(memoryIndex) * abi.MemoryDirEntryBytes
	f.a.Load64(R8, RBX, -offMemoryDirPtr)
	f.a.Load64(R8, R8, entry+abi.MemoryDirCurrentBytesOffset)
	f.trapUnlessLE(RDX, R8)
	f.a.Load64(R8, RBX, -offMemoryDirPtr)
	f.a.Load64(R8, R8, entry+abi.MemoryDirBaseOffset)
	f.a.Add64(offset, R8)
}

// memoryInit lowers memory.init. Memory32 uses three i32 operands; memory64
// widens only the destination to i64 while the passive source offset and length
// remain i32. The source is immutable passive data, so forward rep movsb is
// sufficient after both ranges have been validated.
func (f *fn) memoryInit(r *wasm.Reader) error {
	dataIdx, err := r.U32()
	if err != nil {
		return err
	}
	memoryIndex, err := r.U32()
	if err != nil {
		return err
	}
	f.materializePendingLoads()
	f.flush()
	d := f.depth()
	f.a.Load64(RDI, RSP, f.spillOff(d-3)) // dst offset (i64 for memory64)
	if f.memoryAddr64(memoryIndex) {
		// Core 3 keeps passive-segment source and length operands i32. Loading
		// them explicitly as u32 prevents stale high spill bits from widening
		// the source range while leaving the memory32 instruction stream intact.
		f.a.Load32(RSI, RSP, f.spillOff(d-2))
		f.a.Load32(RCX, RSP, f.spillOff(d-1))
	} else {
		f.a.Load64(RSI, RSP, f.spillOff(d-2))
		f.a.Load64(RCX, RSP, f.spillOff(d-1))
		f.a.MovRegReg32(RDI, RDI)
		f.a.MovRegReg32(RSI, RSI)
		f.a.MovRegReg32(RCX, RCX)
	}

	f.absoluteBulkAddr(memoryIndex, RDI, RCX)

	disp := int32(dataIdx) * 16
	f.a.Load64(R8, RBX, -offPassiveDataPtr) // descriptor array
	f.a.Load32(RAX, R8, disp+8)             // current segment length (zero after data.drop)
	f.a.LeaScaled(RDX, RSI, RCX, 0, 0)      // src + n
	f.trapUnlessLE(RDX, RAX)
	f.a.Load64(R8, R8, disp) // segment base pointer

	f.a.Add64(RSI, R8) // absolute src
	f.a.RepMovsb()

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
	f.a.Load64(R8, RBX, -offPassiveDataPtr)
	f.a.StoreImm32Mem(R8, disp, 0)
	return nil
}

// memoryCopy lowers memory.copy with memmove semantics (rep movsb, overlap-safe).
// The three i32 operands (dst, src, n) are read from canonical slots into the
// fixed rep registers RDI/RSI/RCX; RDX/R8 are the free scratch after the flush.
func (f *fn) memoryCopy(r *wasm.Reader) error {
	dstMemory, err := r.U32()
	if err != nil {
		return err
	}
	srcMemory, err := r.U32()
	if err != nil {
		return err
	}
	if dstMemory == 0 && srcMemory == 0 && !f.memoryAddr64(0) {
		if top := f.s.back(); top != nil && top.isValue() && top.st.kind == stConst {
			if n := uint64(uint32(top.st.cval)); n <= 64 {
				f.stats.peep("memcopy-unroll")
				f.memoryCopyConst(int(n), dstMemory, srcMemory)
				return nil
			}
		}
	}
	f.materializePendingLoads()
	f.flush()
	d := f.depth()
	f.a.Load64(RDI, RSP, f.spillOff(d-3)) // dst offset
	f.a.Load64(RSI, RSP, f.spillOff(d-2)) // src offset
	f.a.Load64(RCX, RSP, f.spillOff(d-1)) // n
	if !f.memoryAddr64(dstMemory) {
		f.a.MovRegReg32(RDI, RDI)
	}
	if !f.memoryAddr64(srcMemory) {
		f.a.MovRegReg32(RSI, RSI)
	}
	if !f.memoryAddr64(dstMemory) || !f.memoryAddr64(srcMemory) {
		f.a.MovRegReg32(RCX, RCX)
	}

	// Scratch in RDX/R8 only (never pinnable); R9 may hold a pinned local.
	f.absoluteBulkAddr(dstMemory, RDI, RCX)
	f.absoluteBulkAddr(srcMemory, RSI, RCX)

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
	f.unitAdjust(RCX, false, false)
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
	f.unitAdjust(RCX, true, true)
	f.a.PatchRel32(f.a.JccPlaceholder(condNE), fwd1)
	joins = append(joins, f.a.JmpPlaceholder())

	// Large: forward-safe copies retain ERMS/FSRM-accelerated rep movsb. True
	// backward overlap uses vector loops: x86 string engines do not
	// accelerate DF=1 and commonly fall to roughly one byte per cycle. Load the
	// complete chunk before storing it so even a one-byte overlap retains memmove
	// semantics. Medium copies stay on the lower-startup XMM path; copies of at
	// least 1 KiB use 128-byte YMM chunks before the XMM/scalar tail. XMM0..3 are
	// scratch after flush; pinned float/vector locals use the high register bank.
	f.a.PatchRel32(big, f.a.Len())
	f.a.Cmp64(RDI, RSI)
	fwd := f.a.JccPlaceholder(condBE)  // dst <= src → forward
	f.a.LeaScaled(RDX, RSI, RCX, 0, 0) // rdx = src + n
	f.a.Cmp64(RDI, RDX)
	fwdDisjoint := f.a.JccPlaceholder(condAE) // dst >= src+n → disjoint → forward
	f.a.AluRI(cmpDigit, RCX, 1024, false)
	mediumBack := f.a.JccPlaceholder(condB)
	back128 := f.a.Len()
	f.a.AluRI(cmpDigit, RCX, 128, false)
	ymmDone := f.a.JccPlaceholder(condB)
	for i, disp := range [...]int32{-128, -96, -64, -32} {
		f.a.YMovdquLoadIdx(Reg(i), RSI, RCX, disp)
	}
	for i, disp := range [...]int32{-128, -96, -64, -32} {
		f.a.YMovdquStoreIdx(RDI, RCX, Reg(i), disp)
	}
	f.a.AluRI(5, RCX, 128, false)
	f.a.JmpBack(back128)
	f.a.PatchRel32(ymmDone, f.a.Len())
	f.a.VZeroUpper()
	f.a.PatchRel32(mediumBack, f.a.Len())
	back64 := f.a.Len()
	f.a.AluRI(cmpDigit, RCX, 64, false)
	backTail := f.a.JccPlaceholder(condB)
	for i, disp := range [...]int32{-64, -48, -32, -16} {
		f.a.VMovdquLoadIdx(Reg(i), RSI, RCX, disp)
	}
	for i, disp := range [...]int32{-64, -48, -32, -16} {
		f.a.VMovdquStoreIdx(RDI, RCX, Reg(i), disp)
	}
	f.a.AluRI(5, RCX, 64, false)
	f.a.JmpBack(back64)
	f.a.PatchRel32(backTail, f.a.Len())
	backTail8 := f.a.Len()
	f.a.AluRI(cmpDigit, RCX, 8, false)
	backTail1 := f.a.JccPlaceholder(condB)
	f.a.LoadIdx(RDX, RSI, RCX, -8, 8, false, true)
	f.a.StoreIdx(RDI, RCX, RDX, -8, 8)
	f.a.AluRI(5, RCX, 8, false)
	f.a.JmpBack(backTail8)
	f.a.PatchRel32(backTail1, f.a.Len())
	backDone := f.rcxZero32Placeholder()
	backByte := f.a.Len()
	f.a.LoadIdx(RDX, RSI, RCX, -1, 1, false, false)
	f.a.StoreIdx(RDI, RCX, RDX, -1, 1)
	f.unitAdjust(RCX, false, false)
	f.closeRCXZero32Loop(backDone, backByte)
	f.patchRCXZero32(backDone)
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(fwd, f.a.Len())
	f.a.PatchRel32(fwdDisjoint, f.a.Len())
	f.a.RepMovsb() // forward (DF=0 by ABI)
	f.a.PatchRel32(done, f.a.Len())
	for _, j := range joins {
		f.a.PatchRel32(j, f.a.Len())
	}

	f.setDepth(d - 3)
	return nil
}

// memoryFill lowers memory.fill (memset of the low byte of val) via rep stosb.
func (f *fn) memoryFill(r *wasm.Reader) error {
	memoryIndex, err := r.U32()
	if err != nil {
		return err
	}
	if memoryIndex == 0 && !f.memoryAddr64(0) {
		if top := f.s.back(); top != nil && top.isValue() && top.st.kind == stConst {
			if n := uint64(uint32(top.st.cval)); n <= 64 {
				f.memoryFillConst(int(n), memoryIndex)
				return nil
			}
		}
	}
	f.materializePendingLoads()
	f.flush()
	d := f.depth()
	f.a.Load64(RDI, RSP, f.spillOff(d-3)) // dst offset
	f.a.Load64(RAX, RSP, f.spillOff(d-2)) // AL = fill byte
	f.a.Load64(RCX, RSP, f.spillOff(d-1)) // n
	if !f.memoryAddr64(memoryIndex) {
		f.a.MovRegReg32(RDI, RDI)
		f.a.MovRegReg32(RCX, RCX)
	}

	// Scratch in RDX/R8 only (never pinnable); R9 may hold a pinned local.
	f.absoluteBulkAddr(memoryIndex, RDI, RCX)

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
	fillDone := f.rcxZero32Placeholder()
	fill1 := f.a.Len()
	f.a.StoreIdx(RDI, RCX, RAX, -1, 1)
	f.unitAdjust(RCX, false, false)
	f.closeRCXZero32Loop(fillDone, fill1)
	if fillDone.compact {
		skipRep := f.a.JmpRel8Placeholder()
		f.a.PatchRel32(bigF, f.a.Len())
		f.a.RepStosb() // [RDI..] = AL, RCX times (DF=0)
		if !f.a.PatchRel8(skipRep, f.a.Len()) {
			panic("amd64: bounded memory.fill skip exceeded rel8 range")
		}
		f.patchRCXZero32(fillDone)
		f.setDepth(d - 3)
		return nil
	}
	skipRep := f.a.JmpPlaceholder()
	f.a.PatchRel32(bigF, f.a.Len())
	f.a.RepStosb() // [RDI..] = AL, RCX times (DF=0)
	f.a.PatchRel32(skipRep, f.a.Len())
	f.patchRCXZero32(fillDone)

	f.setDepth(d - 3)
	return nil
}

// memorySize pushes the current linear-memory size in pages.
func (f *fn) memorySize(r *wasm.Reader) error {
	memoryIndex, err := r.U32()
	if err != nil {
		return err
	}
	out := f.allocReg(0)
	if memoryIndex == 0 {
		f.a.Load32(out, RBX, -bdCurPages)
	} else {
		dir := f.allocReg(maskOf(out))
		f.a.Load64(dir, RBX, -offMemoryDirPtr)
		f.a.Load32(out, dir, int32(memoryIndex)*abi.MemoryDirEntryBytes+abi.MemoryDirCurrentPagesOffset)
		f.release(dir)
	}
	if f.memoryAddr64(memoryIndex) {
		f.pushReg(out, mtI64)
	} else {
		f.pushReg(out, mtI32)
	}
	return nil
}

// memoryGrow grows linear memory by the popped page delta, pushing the previous
// size in pages or -1 on failure. The reservation is mapped up front, so this is
// a pure size-cache update (matching src/core/encoder/amd64); the base never moves.
func (f *fn) memoryGrow(r *wasm.Reader) error {
	memoryIndex, err := r.U32()
	if err != nil {
		return err
	}
	f.invalidateBoundsCert() // memBytes changes; end the certificate conservatively
	delta := f.materialize(f.popValue())
	f.pinned = f.pinned.add(delta)
	memory64 := f.memoryAddr64(memoryIndex)
	failDelta := -1
	if memory64 {
		high := f.allocReg(maskOf(delta))
		f.a.MovReg64(high, delta)
		f.a.ShiftImm(5, high, 32, true)
		f.a.TestSelf(high, true)
		failDelta = f.a.JccPlaceholder(condNE)
		f.release(high)
	}
	res := f.allocReg(maskOf(delta))
	base := RBX
	dir := regNone
	entry := int32(0)
	if memoryIndex != 0 {
		dir = f.allocReg(maskOf(delta).add(res))
		f.a.Load64(dir, RBX, -offMemoryDirPtr)
		entry = int32(memoryIndex) * abi.MemoryDirEntryBytes
		base = f.allocReg(maskOf(delta).add(res).add(dir))
		f.a.Load64(base, dir, entry)
	}
	f.a.Load32(res, base, -bdCurPages) // old pages — the success result
	avoid := maskOf(delta).add(res).add(base)
	if dir != regNone {
		avoid = avoid.add(dir)
	}
	nw := f.allocReg(avoid)
	f.a.MovRegReg32(nw, res)
	f.a.Add32(nw, delta) // new = old + delta; CF on u32 overflow
	failOverflow := f.a.JccPlaceholder(condB)
	mx := f.allocReg(avoid.add(nw))
	f.a.Load32(mx, base, -bdMaxPages)
	f.a.Cmp32(nw, mx)
	failMax := f.a.JccPlaceholder(condA) // new > declared/runtime max
	noPolicyDir := -1
	if memoryIndex == 0 {
		dir = f.allocReg(avoid.add(nw).add(mx))
		f.a.Load64(dir, RBX, -offMemoryDirPtr)
		f.a.TestSelf(dir, true)
		noPolicyDir = f.a.JccPlaceholder(condE)
	}
	f.a.Load32(mx, dir, entry+abi.MemoryDirPolicyMaxPagesOffset)
	f.a.TestSelf(mx, false)
	noPolicy := f.a.JccPlaceholder(condE)
	f.a.Cmp32(nw, mx)
	failPolicy := f.a.JccPlaceholder(condA)
	policyDone := f.a.Len()
	if noPolicyDir >= 0 {
		f.a.PatchRel32(noPolicyDir, policyDone)
	}
	f.a.PatchRel32(noPolicy, policyDone)
	f.a.Store32(base, -bdCurPages, nw)
	f.a.MovRegReg32(mx, nw)
	f.a.ShiftImm(4, mx, wasmPageLog, true) // bytes = uint64(pages) << 16
	f.a.Store64(base, -bdCurBytes, mx)
	f.a.Store32(base, -8, mx) // legacy u32 cache; wraps only at exactly 4 GiB
	if memoryIndex != 0 {
		// The directory caches form one semantic size pair. Publish them only after
		// every overflow/maximum check succeeds; failure leaves both fields intact.
		f.a.Store64(dir, entry+abi.MemoryDirCurrentBytesOffset, mx)
		f.a.Store32(dir, entry+abi.MemoryDirCurrentPagesOffset, nw)
	}
	done := f.a.JmpPlaceholder()
	if failDelta >= 0 {
		f.a.PatchRel32(failDelta, f.a.Len())
	}
	f.a.PatchRel32(failOverflow, f.a.Len())
	f.a.PatchRel32(failMax, f.a.Len())
	f.a.PatchRel32(failPolicy, f.a.Len())
	if memory64 {
		f.a.MovImm64(res, ^uint64(0))
	} else {
		f.a.MovImm32(res, -1)
	}
	f.a.PatchRel32(done, f.a.Len())
	if memoryIndex == 0 && f.memSizeReg != regNone {
		f.a.Load64(f.memSizeReg, RBX, -bdCurBytes) // refresh the memory-0 cache (both paths)
	}
	f.pinned = f.pinned.remove(delta)
	f.release(delta)
	f.release(nw)
	f.release(mx)
	if memoryIndex != 0 {
		f.release(base)
	}
	if dir != regNone {
		f.release(dir)
	}
	if memory64 {
		f.pushReg(res, mtI64)
	} else {
		f.pushReg(res, mtI32)
	}
	return nil
}

// bulkChunks returns the (offset, size) store/load plan for a small constant
// bulk-memory op: 8-byte blocks with an overlapping tail (memmove's small-size
// technique). For n >= 9 it is a straight run of 8-byte chunks plus a final
// overlapping {n-8,8} tail, which reproduces the earlier fixed cases for n <= 32
// and extends cleanly to 64 (used by fill, whose single pattern register makes
// the chunk count irrelevant to register pressure; copy uses bulkChunks16 past 32).
func bulkChunks(n int, buf *[8][2]int) [][2]int {
	chunks := buf[:0]
	switch {
	case n == 0:
		return chunks
	case n == 1 || n == 2 || n == 4 || n == 8:
		return append(chunks, [2]int{0, n})
	case n < 4:
		return append(chunks, [2]int{0, 2}, [2]int{n - 2, 2}) // n == 3
	case n < 8:
		return append(chunks, [2]int{0, 4}, [2]int{n - 4, 4})
	}
	for off := 0; off+8 < n; off += 8 {
		chunks = append(chunks, [2]int{off, 8})
	}
	return append(chunks, [2]int{n - 8, 8})
}

// bulkChunks16 is bulkChunks with 16-byte (XMM) blocks, for 33..64-byte constant
// copies: at most four SSE loads/stores instead of five-to-eight GP ones, which
// keeps the load-all-then-store-all register footprint within the XMM pool. The
// final {n-16,16} tail overlaps the previous block, so no access exceeds n bytes.
func bulkChunks16(n int, buf *[4][2]int) [][2]int {
	chunks := buf[:0]
	for off := 0; off+16 < n; off += 16 {
		chunks = append(chunks, [2]int{off, 16})
	}
	return append(chunks, [2]int{n - 16, 16})
}

// bulkBoundsCheck emits `trap unless base+n <= memBytes` for an unrolled bulk
// op. Constant paths always check, including signals-based mode: a zero-length
// operation has no later load/store to fault and must still reject base > size.
func (f *fn) bulkBoundsCheck(base Reg, n int, memoryIndex uint32) {
	f.pinned = f.pinned.add(base)
	t := f.allocReg(0)
	if f.memoryAddr64(memoryIndex) {
		f.a.MovReg64(t, base)
		f.a.AluRI(0, t, int32(n), true)
		f.trapIf(condB, trapMemOOB)
	} else {
		f.a.LeaDisp(t, base, int32(n))
	}
	if memoryIndex == 0 {
		if f.memSizeReg != regNone {
			f.a.Cmp64(t, f.memSizeReg)
		} else {
			mb := f.allocReg(maskOf(t))
			f.a.Load64(mb, RBX, -bdCurBytes)
			f.a.Cmp64(t, mb)
			f.release(mb)
		}
	} else {
		mb := f.allocReg(maskOf(t))
		f.a.Load64(mb, RBX, -offMemoryDirPtr)
		f.a.Load64(mb, mb, int32(memoryIndex)*abi.MemoryDirEntryBytes+abi.MemoryDirCurrentBytesOffset)
		f.a.Cmp64(t, mb)
		f.release(mb)
	}
	f.trapIf(condA, trapMemOOB)
	f.release(t)
	f.pinned = f.pinned.remove(base)
}

// memoryFillConst lowers memory.fill with a small constant length as unrolled
// stores of a byte-replicated pattern — no flush, no rep-stos microcode startup.
func (f *fn) memoryFillConst(n int, memoryIndex uint32) {
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
	if !f.memoryAddr64(memoryIndex) {
		f.a.MovRegReg32(dst, dst)
	}
	f.bulkBoundsCheck(dst, n, memoryIndex)
	var chunkBuf [8][2]int
	for _, c := range bulkChunks(n, &chunkBuf) {
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
func (f *fn) memoryCopyConst(n int, dstMemory, srcMemory uint32) {
	f.materializePendingLoads()
	f.erase(f.s.back()) // n (const)
	src, srcOwned := f.materializeRead(f.popValue())
	f.pinned = f.pinned.add(src)
	dst, dstOwned := f.materializeRead(f.popValue())
	f.pinned = f.pinned.add(dst)
	if !f.memoryAddr64(dstMemory) {
		f.a.MovRegReg32(dst, dst)
	}
	if !f.memoryAddr64(srcMemory) {
		f.a.MovRegReg32(src, src)
	}
	f.bulkBoundsCheck(dst, n, dstMemory)
	f.bulkBoundsCheck(src, n, srcMemory)
	if n > 32 {
		// 33..64 bytes: SSE 16-byte load-all-then-store-all. At most four XMM
		// registers, so the load-all footprint stays in the float pool (the GP
		// 8-byte form would need five-to-eight registers). Overlap-safe (memmove
		// semantics) because every load precedes every store.
		var chunkBuf [4][2]int
		chunks := bulkChunks16(n, &chunkBuf)
		var xregBuf [4]Reg
		xregs := xregBuf[:len(chunks)]
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
	var chunkBuf [8][2]int
	chunks := bulkChunks(n, &chunkBuf)
	var regBuf [8]Reg
	regs := regBuf[:len(chunks)]
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
