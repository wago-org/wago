//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

// WARP's STACK_REG lazy local-spill model (Common.cpp saveLocalsAndParamsFor
// FuncCall / recoverLocalToReg / recoverAllLocalsToRegBranch), for CALL-MAKING
// functions. Each pinned local has a dedicated register AND a frame slot; the
// live value is tracked in one of four states:
//
//	lsConstZero — declared local's initial zero; neither register nor slot is live
//	lsReg       — value only in the register (register is dirty vs the slot)
//	lsStackReg  — value in BOTH register and slot (register is clean/mirrors memory)
//	lsMem       — value only in the slot (register was clobbered by a call)
//
// The point is to avoid spilling/reloading every pinned local around every call:
//   - at a call we store only DIRTY locals (a clean one is already in its slot),
//     then mark all as clobbered (lsMem) — and DON'T eagerly reload;
//   - a subsequent local.get reloads lazily (recoverLocal);
//   - branches converge everything to lsStackReg so all edges agree.
//
// Call-free functions never enter this path: their pinned locals live in
// registers for the whole function (no calls to clobber them), so locals[].state is
// unused and no reconcile stores are emitted (keeps tight compute loops fast).

type locState uint8

const (
	lsReg       locState = iota // dirty: value only in the register; keep zero-value for old eager path
	lsStackReg                  // clean: value in both register and slot
	lsMem                       // spilled: value only in the slot
	lsConstZero                 // declared local's initial zero, not materialized yet
)

// packedLocStates stores up to 64 local merge states in two bits each. Call-making
// functions with more locals do not admit whole-function pins; call-free wide
// pins do not use these merge snapshots. A recorded snapshot contains a pin in
// lsStackReg or lsMem; the all-zero value is therefore the absent sentinel.
type packedLocStates [2]uint64

type localStateSnapshot [64]locState

type localDef struct {
	facts   valueFacts
	reg     Reg
	isFloat bool
	state   locState
}

func zeroStorage(typ machineType) storage {
	return storage{kind: stConst, typ: typ, cval: 0}
}

func (f *fn) materializeGCFrameLocalsAt(site int, call bool) {
	if f.gcFrameRoots == nil || !f.lazyZero {
		return
	}
	if !f.gcFrameRoots.VisitLiveLocals(site, call, func(root int) {
		index := f.gcFrameRoots.Locals[root].Index
		x := int(index)
		if x < 0 || x >= f.nLocals {
			f.gcFrameRoots.Exact = false
			return
		}
		if f.locals[x].state == lsConstZero {
			f.materializeZeroLocal(x, true)
		}
	}) {
		f.gcFrameRoots.Exact = false
	}
}

// convergeEdgeTo converges pinned-local state for a control edge into the
// per-frame target *target, RECORDING the target from the current state when
// this is the frame's first edge. Targets are per-local, ∈ {lsStackReg, lsMem}:
//   - lsStackReg: register AND slot valid at the merge;
//   - lsMem: only the slot is guaranteed — a call-clobbered local stays
//     unloaded across the merge until a read actually needs it (the lazy-merge
//     win: post-call branch-dense code stops reloading every pinned local at
//     every boundary).
//
// An edge may arrive STRONGER than the target (lsStackReg where lsMem is
// recorded) — always safe: the merge assumes only the target. The merge point
// itself must then install the recorded target as the tracked state
// (setLocalsState).

const frameEndConditional uint32 = 1 << 31

const maxMergeNextUseOps = shared.MergeNextUseFuel
