package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Register-based internal call ABI (opt-in via CompileOptions.RegisterCallABI).
//
// Internal wasm→wasm calls to integer functions pass arguments and the single
// result in registers instead of marshalling them through an rsp buffer, which
// is the dominant cost on recursive/call-heavy code. We control both sides, so
// the convention is our own (not the C ABI):
//
//	RDI = linMem pointer      RSI = trap pointer
//	integer args: RAX, RCX, RDX, R8, R9, R10, R11   (in order)
//	integer result: RAX
//
// Functions whose signature fits get two entry points: a thin host→internal
// adapter at offset 0 (so exports, host calls, and CompileFunction keep working
// through the wrapper ABI) and the internal entry holding the real frame+body.
// Functions that don't fit are compiled wrapper-only and called the old way; the
// caller picks the path per callee signature, so the two coexist in one module.

// intArgRegs is the integer argument/result register order for the internal ABI.
// RDI/RSI are reserved (linMem/trap); RBX,R13-R15 may be pinned locals/globals;
// R12 is avoided as a memory base elsewhere and excluded here for symmetry.
var intArgRegs = []Reg{RAX, RCX, RDX, R8, R9, R10, R11}

func isIntValType(t wasm.ValType) bool {
	return wasm.EqualValType(t, wasm.I32) || wasm.EqualValType(t, wasm.I64)
}

// sigFitsRegABI reports whether a function signature can use the register ABI:
// integer-only params and result, at most len(intArgRegs) params and one result.
func sigFitsRegABI(ft *wasm.CompType) bool {
	if len(ft.Params) > len(intArgRegs) || len(ft.Results) > 1 {
		return false
	}
	for _, t := range ft.Params {
		if !isIntValType(t) {
			return false
		}
	}
	for _, t := range ft.Results {
		if !isIntValType(t) {
			return false
		}
	}
	return true
}

// emitRegABIFunction emits a fitting function as [host adapter | internal entry].
// It returns code, relocs, and the internal entry's offset within the code.
func (g *cg) emitRegABIFunction(c *wasm.Func, ft *wasm.CompType, nParams, nLocals int) ([]byte, []callReloc, int, error) {
	a := g.a

	// --- host→internal adapter (offset 0) ---
	// In:  RDI=serArgs, RSI=linMem, RDX=trap, RCX=results (wrapper ABI).
	// Out: calls the internal entry with the register ABI, stores the result.
	a.Push(RCX) // save results ptr
	a.Push(RDX) // save trap
	a.Push(RSI) // save linMem
	for i := 0; i < nParams; i++ {
		a.Load64(intArgRegs[i], RDI, int32(8*i)) // serArgs[i] → arg reg
	}
	a.Pop(RDI) // linMem → RDI
	a.Pop(RSI) // trap   → RSI
	adapterCall := a.CallRel32()
	a.Pop(RCX) // results ptr
	if len(ft.Results) == 1 {
		a.Store64(RCX, 0, RAX) // result → results[0]
	}
	a.Ret()

	// --- internal entry ---
	internalOff := a.Len()
	a.Prologue()
	subRspAt := a.Len() + 3
	a.SubRsp(0)
	a.Store64(RBP, -16, RDI)        // linMem
	a.Store64(RBP, -24, RSI)        // trap
	g.emitStackFenceCheck(RDI, RSI) // linMem in RDI; RSI free (trap saved); args stay in their regs
	for i := 0; i < nParams; i++ {
		// Destinations are pinned registers or frame slots, never argument
		// registers, so the parameter moves cannot clobber a later source.
		if pr := g.localReg[i]; pr != regNone {
			a.MovReg64(pr, intArgRegs[i])
		} else {
			a.Store64(RBP, g.localOff(i), intArgRegs[i])
		}
	}
	g.emitZeroAndPrime(nParams, nLocals)

	if err := g.emitBodyAndWriteback(c); err != nil {
		return nil, nil, 0, err
	}
	if g.nResults == 1 {
		a.Load64(RAX, RBP, g.slotOff(0)) // result → RAX
	}
	a.Load64(RCX, RBP, -24) // trap ptr (RCX is never the result register)
	a.StoreImm32Mem(RCX, 0, 0)
	a.Leave()
	a.Ret()

	a.PatchU32(subRspAt, uint32(g.frameSize()))
	a.PatchRel32(adapterCall, internalOff)
	g.stats.noteFunction(len(a.B), g.maxDepth, len(g.pinned), len(g.pinnedGlob))
	return a.B, g.relocs, internalOff, nil
}

// emitRegisterCall lowers an internal call to a register-ABI function. The top
// p operands are the arguments; the single result (if any) is left on the stack.
func (g *cg) emitRegisterCall(localIdx int, ft *wasm.CompType) {
	a := g.a
	p := len(ft.Params)
	rN := len(ft.Results)
	depth := len(g.st)
	g.flushBelow(depth - p) // operands below the args go to their slots

	// Place each argument into its ABI register. Register-resident sources form
	// a parallel move (resolved to avoid clobbering); constants and memory-resident
	// sources are loaded directly afterward, once the moves have vacated the regs.
	args := g.st[depth-p:]
	var moves []regMove
	type deferredArg struct {
		target Reg
		e      ventry
	}
	var deferred []deferredArg
	for i := 0; i < p; i++ {
		t := intArgRegs[i]
		switch e := args[i]; e.kind {
		case vReg, vPinned:
			moves = append(moves, regMove{dst: t, src: e.reg})
		default:
			deferred = append(deferred, deferredArg{target: t, e: e})
		}
	}
	resolveRegMoves(moves, func(dst, src Reg) { a.MovReg64(dst, src) }, func(x, y Reg) { a.Xchg64(x, y) })
	for _, d := range deferred {
		switch d.e.kind {
		case vConst:
			if d.e.wide {
				a.MovImm64(d.target, uint64(d.e.cval))
			} else {
				a.MovImm32(d.target, int32(d.e.cval))
			}
		case vLocal:
			a.Load64(d.target, RBP, g.localOff(d.e.local))
		case vSpill:
			a.Load64(d.target, RBP, g.slotOff(d.e.slot))
		}
	}

	g.st = g.st[:depth-p]
	for i := range g.busy { // callee clobbers all scratch and the arg/result registers
		g.busy[i] = false
		g.fbusy[i] = false
	}

	// Preserve caller state the callee will clobber. RDI is free here (not an arg
	// register) so it serves as the global write-back scratch without touching the
	// argument registers; it is then loaded with linMem for the call.
	for _, pl := range g.pinned {
		a.Store64(RBP, g.localOff(pl.local), pl.reg)
	}
	g.writeBackGlobals(RDI)
	a.Load64(RDI, RBP, -16) // linMem
	a.Load64(RSI, RBP, -24) // trap

	site := a.CallRel32()
	g.relocs = append(g.relocs, callReloc{at: site, target: localIdx, internal: true})

	// Capture the result out of RAX before reusing RAX as scratch below.
	var resReg Reg
	if rN == 1 {
		resReg = g.allocRegExcept(RAX)
		a.MovReg64(resReg, RAX)
	}
	g.reloadGlobals(RAX)
	for _, pl := range g.pinned {
		a.Load64(pl.reg, RBP, g.localOff(pl.local))
	}

	// Propagate a callee trap: if the trap slot is non-zero, unwind immediately.
	a.Load64(RAX, RBP, -24)
	a.Load32(RAX, RAX, 0)
	a.TestSelf(RAX, false)
	ok := a.JccPlaceholder(CondE)
	a.Leave()
	a.Ret()
	a.PatchRel32(ok, a.Len())

	if rN == 1 {
		g.pushReg(resReg)
	}
}
