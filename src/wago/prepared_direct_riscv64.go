// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	goruntime "runtime"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

const preparedDirectIntSupported = true
const preparedDirectIntPrivateSupported = true
const preparedIntCallBlockDefault = false

func prepareRV64DirectTrap(in *Instance) {
	railshotSetNativeContextIdentity(in.jm, in.nativeContext)
	wruntime.PreparePreparedIntTrap(in.trap)
}

func (fn *WasmFunc) initDirectIntCall() {
	if directIntBoundedLightForImage && preparedIntCallBlockEnabled && fn.directIntBounded && fn.directIntLight {
		fn.in.eng.PrepareIntCall(&fn.directIntCall, fn.directEntry, fn.directLinMem)
		fn.directIntMode = preparedIntCallBlock
	}
}

func (in *Instance) invokeCachedDirectInt1(ic *invokeCache, entry uintptr, arg uint64) ([]uint64, error) {
	if fixedIntArgumentIsNarrow(1, ic.scalarWideMask) {
		arg = uint64(uint32(arg))
	}
	prepareRV64DirectTrap(in)
	var result uint64
	var err error
	if useBoundedDirectInt(ic.directIntBounded) {
		if useBoundedLightDirectInt(ic.directIntLight) {
			result, err = in.eng.EnterPreparedIntLightBounded(entry, in.jm.LinMemBase(), arg, 0, 0, 0)
		} else if directIntBoundedFullForImage {
			result, err = in.eng.EnterPreparedIntBounded(entry, in.jm.LinMemBase(), arg, 0, 0, 0)
		}
	} else if useUnboundedLightDirectInt(ic.directIntLight) {
		result, err = in.eng.EnterPreparedIntLight(entry, in.jm.LinMemBase(), arg, 0, 0, 0)
	} else if directIntUnboundedFullForImage {
		result, err = in.eng.EnterPreparedInt(entry, in.jm.LinMemBase(), arg, 0, 0, 0)
	}
	if err != nil {
		return nil, fmt.Errorf("wago: map prepared integer entry: %w", err)
	}
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:ic.resultSlots]
	storeFixedIntResult(out, result, ic.scalarResultWide)
	return out, nil
}

func (in *Instance) invokeCachedDirectI32ToI32(ic *invokeCache, arg uint64) ([]uint64, error) {
	prepareRV64DirectTrap(in)
	var result uint64
	var err error
	if useBoundedLightDirectInt(ic.directIntLight) {
		result, err = in.eng.EnterPreparedIntLightBounded(ic.directEntry, in.jm.LinMemBase(), uint64(uint32(arg)), 0, 0, 0)
	} else if directIntBoundedFullForImage {
		result, err = in.eng.EnterPreparedIntBounded(ic.directEntry, in.jm.LinMemBase(), uint64(uint32(arg)), 0, 0, 0)
	}
	if err != nil {
		return nil, fmt.Errorf("wago: map prepared integer entry: %w", err)
	}
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:1]
	out[0] = uint64(uint32(result))
	return out, nil
}

func (fn *WasmFunc) invokeDirectInt(args []uint64) ([]uint64, error) {
	var a0, a1, a2, a3 uint64
	switch len(args) {
	case 4:
		a3 = args[3]
		fallthrough
	case 3:
		a2 = args[2]
		fallthrough
	case 2:
		a1 = args[1]
		fallthrough
	case 1:
		a0 = args[0]
	}
	return fn.invokeDirectIntFixed(a0, a1, a2, a3)
}

func (fn *WasmFunc) invokeDirectIntFixed(a0, a1, a2, a3 uint64) ([]uint64, error) {
	in := fn.in
	if fn.directIsolated && fn.tryBeginFastInvocation() {
		out, err := fn.invokeDirectIntSession(a0, a1, a2, a3)
		in.endFastInvocation(in.pluginState.Load())
		return out, err
	}
	if !in.tryBeginDirectInvocation() {
		if err := in.beginInvocation(); err != nil {
			return nil, fmt.Errorf("wago: invoke Wasm function: %w", err)
		}
	}
	if fn.directIsolated {
		held, valid := fn.tryDirectGateState()
		if valid {
			out, err := fn.invokeDirectIntSession(a0, a1, a2, a3)
			fn.directGate.Unlock()
			in.endDirectInvocation()
			return out, err
		}
		if held {
			fn.directGate.Unlock()
		}
	}
	return fn.invokeDirectIntFixedShared(a0, a1, a2, a3)
}

func (fn *WasmFunc) invokeDirectIntFixedShared(a0, a1, a2, a3 uint64) (result []uint64, resultErr error) {
	in := fn.in
	var preparedLease preparedInvocationLease
	leaseOwned := false
	var cleanup resourceCleanup
	cleanup.run(func() {
		preparedLease = in.lockPreparedInvocation()
		leaseOwned = true
		result, resultErr = fn.invokeDirectIntFixedSharedAdmitted(a0, a1, a2, a3)
	})
	if leaseOwned {
		cleanup.run(preparedLease.unlock)
	}
	cleanup.run(in.endDirectInvocation)
	cleanup.finish()
	return result, resultErr
}

func (fn *WasmFunc) invokeDirectIntFixedSharedAdmitted(a0, a1, a2, a3 uint64) ([]uint64, error) {
	in := fn.in
	switch fn.paramSlots {
	case 4:
		if fixedIntArgumentIsNarrow(8, fn.scalarWideMask) {
			a3 = uint64(uint32(a3))
		}
		fallthrough
	case 3:
		if fixedIntArgumentIsNarrow(4, fn.scalarWideMask) {
			a2 = uint64(uint32(a2))
		}
		fallthrough
	case 2:
		if fixedIntArgumentIsNarrow(2, fn.scalarWideMask) {
			a1 = uint64(uint32(a1))
		}
		fallthrough
	case 1:
		if fixedIntArgumentIsNarrow(1, fn.scalarWideMask) {
			a0 = uint64(uint32(a0))
		}
	}
	locked := !fn.directIsolated
	if locked {
		nativeExecutionMu.Lock()
		nativeExecutionEpoch++
	}
	if !in.lockPreparedFastState() {
		if locked {
			nativeExecutionMu.Unlock()
		}
		args := [4]uint64{a0, a1, a2, a3}
		return fn.invokeGeneralAdmitted(args[:fn.paramSlots])
	}
	defer in.unlockPreparedFastState()
	var result uint64
	var err error
	prepareRV64DirectTrap(in)
	if directIntBoundedLightForImage && fn.directIntMode == preparedIntCallBlock {
		result = in.eng.EnterPreparedIntPreboundContextBounded(&fn.directIntCall, a0, a1, a2, a3)
	} else if useBoundedDirectInt(fn.directIntBounded) {
		if useBoundedLightDirectInt(fn.directIntLight) {
			result, err = in.eng.EnterPreparedIntLightBounded(fn.directEntry, fn.directLinMem, a0, a1, a2, a3)
		} else if directIntBoundedFullForImage {
			result, err = in.eng.EnterPreparedIntBounded(fn.directEntry, fn.directLinMem, a0, a1, a2, a3)
		}
	} else if useUnboundedLightDirectInt(fn.directIntLight) {
		result, err = in.eng.EnterPreparedIntLight(fn.directEntry, fn.directLinMem, a0, a1, a2, a3)
	} else if directIntUnboundedFullForImage {
		result, err = in.eng.EnterPreparedInt(fn.directEntry, fn.directLinMem, a0, a1, a2, a3)
	}
	if err != nil {
		if locked {
			nativeExecutionMu.Unlock()
		}
		return nil, fmt.Errorf("wago: map prepared integer entry: %w", err)
	}
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		err := in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
		if locked {
			nativeExecutionMu.Unlock()
		}
		return nil, err
	}
	goruntime.KeepAlive(fn)
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:fn.resultSlots]
	storeFixedIntResult(out, result, fn.scalarResultWide)
	if locked {
		nativeExecutionMu.Unlock()
	}
	return out, nil
}

func (in *Instance) invokeDirectIntEntry(directEntry uintptr, paramSlots, resultSlots int, scalarWideMask uint8, scalarResultWide, isolatedFast, light, bounded bool, a0, a1, a2, a3 uint64) ([]uint64, error) {
	if in.isLogicallyClosed() {
		return nil, fmt.Errorf("wago: invoke Wasm function: instance is closed")
	}
	switch paramSlots {
	case 4:
		if fixedIntArgumentIsNarrow(8, scalarWideMask) {
			a3 = uint64(uint32(a3))
		}
		fallthrough
	case 3:
		if fixedIntArgumentIsNarrow(4, scalarWideMask) {
			a2 = uint64(uint32(a2))
		}
		fallthrough
	case 2:
		if fixedIntArgumentIsNarrow(2, scalarWideMask) {
			a1 = uint64(uint32(a1))
		}
		fallthrough
	case 1:
		if fixedIntArgumentIsNarrow(1, scalarWideMask) {
			a0 = uint64(uint32(a0))
		}
	}
	locked := !isolatedFast
	if locked {
		nativeExecutionMu.Lock()
		nativeExecutionEpoch++
	}
	var result uint64
	var err error
	prepareRV64DirectTrap(in)
	if useBoundedDirectInt(bounded) {
		if useBoundedLightDirectInt(light) {
			result, err = in.eng.EnterPreparedIntLightBounded(directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
		} else if directIntBoundedFullForImage {
			result, err = in.eng.EnterPreparedIntBounded(directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
		}
	} else if useUnboundedLightDirectInt(light) {
		result, err = in.eng.EnterPreparedIntLight(directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
	} else if directIntUnboundedFullForImage {
		result, err = in.eng.EnterPreparedInt(directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
	}
	if err != nil {
		if locked {
			nativeExecutionMu.Unlock()
		}
		return nil, fmt.Errorf("wago: map prepared integer entry: %w", err)
	}
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		err := in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
		if locked {
			nativeExecutionMu.Unlock()
		}
		return nil, err
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:resultSlots]
	storeFixedIntResult(out, result, scalarResultWide)
	if locked {
		nativeExecutionMu.Unlock()
	}
	return out, nil
}
func (fn *WasmFunc) invokeDirectIntSession(a0, a1, a2, a3 uint64) ([]uint64, error) {
	if fn.usesDirectIntPairABI() {
		return fn.invokeDirectIntPairSession(a0, a1, a2, a3)
	}
	in := fn.in
	switch fn.paramSlots {
	case 4:
		if fixedIntArgumentIsNarrow(8, fn.scalarWideMask) {
			a3 = uint64(uint32(a3))
		}
		fallthrough
	case 3:
		if fixedIntArgumentIsNarrow(4, fn.scalarWideMask) {
			a2 = uint64(uint32(a2))
		}
		fallthrough
	case 2:
		if fixedIntArgumentIsNarrow(2, fn.scalarWideMask) {
			a1 = uint64(uint32(a1))
		}
		fallthrough
	case 1:
		if fixedIntArgumentIsNarrow(1, fn.scalarWideMask) {
			a0 = uint64(uint32(a0))
		}
	}
	prepareRV64DirectTrap(in)
	var result uint64
	var err error
	if directIntBoundedLightForImage && fn.directIntMode == preparedIntCallBlock {
		result = in.eng.EnterPreparedIntPreboundContextBounded(&fn.directIntCall, a0, a1, a2, a3)
	} else if useBoundedDirectInt(fn.directIntBounded) {
		if useBoundedLightDirectInt(fn.directIntLight) {
			result, err = in.eng.EnterPreparedIntLightBounded(fn.directEntry, fn.directLinMem, a0, a1, a2, a3)
		} else if directIntBoundedFullForImage {
			result, err = in.eng.EnterPreparedIntBounded(fn.directEntry, fn.directLinMem, a0, a1, a2, a3)
		}
	} else if useUnboundedLightDirectInt(fn.directIntLight) {
		result, err = in.eng.EnterPreparedIntLight(fn.directEntry, fn.directLinMem, a0, a1, a2, a3)
	} else if directIntUnboundedFullForImage {
		result, err = in.eng.EnterPreparedInt(fn.directEntry, fn.directLinMem, a0, a1, a2, a3)
	}
	if err != nil {
		return nil, fmt.Errorf("wago: map prepared integer entry: %w", err)
	}
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
	}
	goruntime.KeepAlive(fn)
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:fn.resultSlots]
	storeFixedIntResult(out, result, fn.scalarResultWide)
	return out, nil
}
