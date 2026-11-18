//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

type registerArgumentMode uint8

const (
	registerArgumentsGP registerArgumentMode = iota
	registerArgumentsMixed
	registerArgumentsTail
)

// A call-local view of existing scratch, not a retained signature sidecar.
// GP calls reuse transient slices; mixed/tail calls supply bounded stack arrays.
type registerArgumentPlan struct {
	gpMoves, fpMoves []regMove
	deferred         []deferredArg
	gpArgs           int
	mode             registerArgumentMode
}

// Capture all expressions before freezing any locations. A later fixed-register
// operation may spill even a pinned earlier argument; slots must also be captured
// before the caller canonicalizes the operand prefix into those same slots.
// roots is the original logical-root view and is not retained by the plan.
func (f *fn) captureRegisterArguments(plan registerArgumentPlan, roots []*elem, params []wasm.ValType, mode registerArgumentMode) (registerArgumentPlan, *elem) {
	f.materializeCallExpressions(roots)
	args := roots[len(roots)-len(params):]
	plan.mode = mode
	gp, fp := 0, 0
	for i, root := range args {
		floating := mode != registerArgumentsGP && mtOf(params[i]).isFloat()
		resident := registerCallArgNeedsCapture(root)
		if floating {
			target := fpArgRegs[fp]
			if resident {
				src := f.materializeF(root)
				f.fpinned = f.fpinned.add(src)
				plan.fpMoves = append(plan.fpMoves, regMove{dst: target, src: src})
			} else {
				plan.deferred = append(plan.deferred, deferredArg{target: target, root: root, float: true})
			}
			fp++
		} else {
			target := intArgRegs[gp]
			if resident {
				src := f.materialize(root)
				f.pinned = f.pinned.add(src)
				plan.gpMoves = append(plan.gpMoves, regMove{dst: target, src: src})
			} else {
				plan.deferred = append(plan.deferred, deferredArg{target: target, root: root})
			}
			gp++
		}
		if mode == registerArgumentsMixed && resident {
			f.stats.peep("mixed-call-reg-arg")
		}
	}
	plan.gpArgs = gp
	if len(args) == 0 {
		return plan, nil
	}
	return plan, args[0]
}

// The caller owns the barrier between capture and commit: prefix flush and
// dirty-local spills for returning calls, neither for tail jumps. Target/frame,
// EH, collector-root, physical call, and result handling remain in the caller.
func (f *fn) commitRegisterArguments(plan *registerArgumentPlan) {
	for _, move := range plan.gpMoves {
		f.pinned = f.pinned.remove(move.src)
	}
	var checkGP func()
	if regallocCheckEnabled {
		checkGP = f.checkBeginRegMoves(plan.gpMoves, false)
		defer checkGP()
	}
	resolveRegMovesWindow(plan.gpMoves, func(dst, src Reg) { f.a.MovReg64(dst, src) }, func(x, y Reg) { f.a.Xchg64(x, y) })
	if regallocCheckEnabled {
		checkGP()
	}
	if plan.mode != registerArgumentsGP {
		for _, move := range plan.fpMoves {
			f.fpinned = f.fpinned.remove(move.src)
		}
		fpSwapSlot := -1
		var checkFP func()
		if regallocCheckEnabled {
			checkFP = f.checkBeginRegMoves(plan.fpMoves, true)
			defer checkFP()
		}
		resolveRegMovesWindow(plan.fpMoves,
			func(dst, src Reg) { f.a.FMov(dst, src, true) },
			func(x, y Reg) {
				if fpSwapSlot < 0 {
					fpSwapSlot = f.allocSpillSlot()
				}
				off := f.spillOff(fpSwapSlot)
				f.a.FStoreDisp(RSP, off, x, true)
				f.a.FMov(x, y, true)
				f.a.FLoadDisp(y, RSP, off, true)
			})
		if regallocCheckEnabled {
			checkFP()
		}
	}
	// Float constants can use a GPR scratch without a literal pool. Preserve all
	// integer argument homes after the shuffle, including otherwise-unowned RDI.
	argumentPins := f.pinned
	if plan.mode != registerArgumentsGP {
		for _, target := range intArgRegs[:plan.gpArgs] {
			f.pinned = f.pinned.add(target)
		}
	}
	for _, arg := range plan.deferred {
		if arg.float {
			switch arg.root.st.kind {
			case stConst:
				f.loadFConst(arg.target, arg.root.st)
			case stSlot:
				f.a.FLoadDisp(arg.target, RSP, f.spillOff(arg.root.st.slotIndex()), arg.root.st.typ == mtF64)
			case stLocalRef:
				f.loadFrameFloat(arg.target, f.localAddr(arg.root.st.index()), arg.root.st.typ == mtF64)
			}
		} else {
			switch arg.root.st.kind {
			case stConst:
				f.loadConst(arg.target, arg.root.st)
			case stSlot:
				f.a.Load64(arg.target, RSP, f.spillOff(arg.root.st.slotIndex()))
			case stLocalRef:
				f.loadCallLocalInt(arg.target, arg.root.st)
			}
		}
	}
	if plan.mode != registerArgumentsGP {
		f.pinned = argumentPins
	}
}
