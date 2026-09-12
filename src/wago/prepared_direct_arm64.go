//go:build arm64 && (linux || darwin || (windows && !tinygo))

package wago

import (
	"fmt"
	goruntime "runtime"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

const preparedDirectIntSupported = true
const preparedDirectIntPrivateSupported = true
const preparedIntCallBlockDefault = true

func (fn *PreparedFunction) initDirectIntCall() {
	if preparedIntCallBlockEnabled && fn.directIntBounded && fn.directIntLight {
		fn.in.eng.PrepareIntCall(&fn.directIntCall, fn.directEntry, fn.directLinMem)
		fn.directIntMode = preparedIntCallBlock
	}
}

func (fn *PreparedFunction) invokeDirectInt(args []uint64) ([]uint64, error) {
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

func (fn *PreparedFunction) invokeDirectIntFixed(a0, a1, a2, a3 uint64) ([]uint64, error) {
	in := fn.in
	if err := in.beginInvocation(); err != nil {
		return nil, fmt.Errorf("wago: invoke prepared function: %w", err)
	}
	defer in.endInvocation()
	preparedLease := in.lockPreparedInvocation()
	defer preparedLease.unlock()
	switch fn.paramSlots {
	case 4:
		if fn.scalarWideMask&8 == 0 {
			a3 = uint64(uint32(a3))
		}
		fallthrough
	case 3:
		if fn.scalarWideMask&4 == 0 {
			a2 = uint64(uint32(a2))
		}
		fallthrough
	case 2:
		if fn.scalarWideMask&2 == 0 {
			a1 = uint64(uint32(a1))
		}
		fallthrough
	case 1:
		if fn.scalarWideMask&1 == 0 {
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
	wruntime.PreparePreparedIntTrap(in.trap)
	if fn.directIntMode == preparedIntCallBlock {
		result = in.eng.EnterPreparedIntCallBounded(&fn.directIntCall, a0, a1, a2, a3)
	} else if fn.directIntBounded {
		if fn.directIntLight {
			result, err = in.eng.EnterPreparedIntLightBounded(fn.directEntry, fn.directLinMem, a0, a1, a2, a3)
		} else {
			result, err = in.eng.EnterPreparedIntBounded(fn.directEntry, fn.directLinMem, a0, a1, a2, a3)
		}
	} else if fn.directIntLight {
		result, err = in.eng.EnterPreparedIntLight(fn.directEntry, fn.directLinMem, a0, a1, a2, a3)
	} else {
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
	if fn.resultSlots == 1 {
		if fn.scalarResultWide {
			out[0] = result
		} else {
			out[0] = uint64(uint32(result))
		}
	}
	if locked {
		nativeExecutionMu.Unlock()
	}
	return out, nil
}

func (in *Instance) invokeDirectIntEntry(directEntry uintptr, paramSlots, resultSlots int, scalarWideMask uint8, scalarResultWide, isolatedFast, light, bounded bool, a0, a1, a2, a3 uint64) ([]uint64, error) {
	if in.isLogicallyClosed() {
		return nil, fmt.Errorf("wago: invoke prepared function: instance is closed")
	}
	switch paramSlots {
	case 4:
		if scalarWideMask&8 == 0 {
			a3 = uint64(uint32(a3))
		}
		fallthrough
	case 3:
		if scalarWideMask&4 == 0 {
			a2 = uint64(uint32(a2))
		}
		fallthrough
	case 2:
		if scalarWideMask&2 == 0 {
			a1 = uint64(uint32(a1))
		}
		fallthrough
	case 1:
		if scalarWideMask&1 == 0 {
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
	wruntime.PreparePreparedIntTrap(in.trap)
	if bounded {
		if light {
			result, err = in.eng.EnterPreparedIntLightBounded(directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
		} else {
			result, err = in.eng.EnterPreparedIntBounded(directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
		}
	} else if light {
		result, err = in.eng.EnterPreparedIntLight(directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
	} else {
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
	if resultSlots == 1 {
		if scalarResultWide {
			out[0] = result
		} else {
			out[0] = uint64(uint32(result))
		}
	}
	if locked {
		nativeExecutionMu.Unlock()
	}
	return out, nil
}
