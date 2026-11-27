// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo && !wago_no_gc

package wago

import (
	goruntime "runtime"

	wruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

type compiledGCInductionState struct {
	// Compiler-owned immutable arithmetic proofs.
	gcI31Inductions []uint64
}

type preparedGCInductionState struct {
	gcDeadInductionLayout uint64
	gcDeadBatch           gc.PreparedDeadAllocationBatch
	gcI31Induction        bool
}

func (c *Compiled) setPreparedGCInductions(proofs []uint64) {
	if len(proofs) != 0 {
		c.ensureCompileIndexes().gcI31Inductions = proofs
	}
}

func (fn *WasmFunc) hasPreparedI31Induction() bool  { return fn.gcI31Induction }
func (fn *WasmFunc) hasPreparedDeadInduction() bool { return fn.gcDeadInductionLayout != 0 }

func (fn *WasmFunc) prepareGCInductions(local int) {
	in := fn.in
	if fn.hasRevocableGCContext() && preparedDirectIntSupported && preparedDirectIntEnabled && !in.c.registerABIDisabled {
		if roots := in.c.genericGCFrameRoots(); roots != nil && local < len(roots.deadInductionLayouts) {
			fn.gcDeadInductionLayout = roots.deadInductionLayouts[local]
			fn.gcDeadBatch = in.gc.PrepareDeadAllocationBatch(uint32(fn.gcDeadInductionLayout), uint32(fn.gcDeadInductionLayout>>32))
		}
	}
	if !codeProfileEnabled && fn.directIntFast && fn.directIsolated && fn.directIntBounded && fn.paramSlots == 1 && fn.resultSlots == 1 {
		if indexes := in.c.loadCompileIndexes(); indexes != nil && local>>6 < len(indexes.gcI31Inductions) {
			fn.gcI31Induction = indexes.gcI31Inductions[local>>6]&(uint64(1)<<uint(local&63)) != 0
		}
	}
}

func (fn *WasmFunc) tryInvokePreparedI31(argument uint64) ([]uint64, error, bool) {
	in := fn.in
	if in.rt != nil || fn.directGate == nil || fn.directGate.state.shared != &in.invocationState.word || !in.invocationState.word.CompareAndSwap(0, instanceFastAdmission) {
		return nil, nil, false
	}
	if in.guestStorageBorrowed() || !in.preparedFastStateValid() {
		in.endFastInvocation(in.pluginState.Load())
		return nil, nil, false
	}
	n := uint64(uint32(argument))
	value := uint32(n*(n-1)/2) ^ ((uint32(n) << 31) & uint32(n))
	// Pure arithmetic has no effects before the poll. Handle a pending
	// interruption or stale trap before publishing the numeric result.
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		if err := clearPreparedInductionTrap(in); err != nil {
			in.endFastInvocation(in.pluginState.Load())
			return nil, err, true
		}
	}
	out := in.resultVals[:1]
	out[0] = uint64(value)
	in.endFastInvocation(in.pluginState.Load())
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	return out, nil, true
}

func (fn *WasmFunc) tryInvokePreparedDeadInduction(argument uint64, state *instancePluginState) ([]uint64, error, bool) {
	in := fn.in
	if err := validateNativeGCEntry(in); err != nil {
		in.endFastInvocation(state)
		return nil, err, true
	}
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		if err := clearPreparedInductionTrap(in); err != nil {
			in.endFastInvocation(state)
			return nil, err, true
		}
	}
	n := uint64(uint32(argument))
	if !fn.gcDeadBatch.TryReserve(max(uint32(n), 1)) {
		return nil, nil, false
	}
	value := uint64(uint32(n * (n - 1) / 2))
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		err := wruntime.ConsumePreparedIntTrap(in.trap)
		in.endFastInvocation(state)
		return nil, err, true
	}
	out := in.resultVals[:1]
	out[0] = value
	in.endFastInvocation(state)
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	return out, nil, true
}

//go:noinline
func clearPreparedInductionTrap(in *Instance) error {
	wruntime.PreparePreparedIntTrap(in.trap)
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return wruntime.ConsumePreparedIntTrap(in.trap)
	}
	return nil
}
