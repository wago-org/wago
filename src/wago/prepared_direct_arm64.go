//go:build arm64 && (linux || darwin || (windows && !tinygo))

package wago

import (
	"fmt"
	goruntime "runtime"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

const preparedDirectIntSupported = true
const preparedDirectIntPrivateSupported = true

func (fn *PreparedFunction) invokeDirectInt(args []uint64) ([]uint64, error) {
	switch len(args) {
	case 0:
		return fn.invokeDirectIntFixed(0, 0, 0, 0)
	case 1:
		return fn.invokeDirectIntFixed(args[0], 0, 0, 0)
	case 2:
		return fn.invokeDirectIntFixed(args[0], args[1], 0, 0)
	case 3:
		return fn.invokeDirectIntFixed(args[0], args[1], args[2], 0)
	case 4:
		return fn.invokeDirectIntFixed(args[0], args[1], args[2], args[3])
	default:
		return fn.invokeDirectIntFixed(0, 0, 0, 0)
	}
}

func (fn *PreparedFunction) invokeDirectIntFixed(a0, a1, a2, a3 uint64) ([]uint64, error) {
	in := fn.in
	if in.isLogicallyClosed() {
		return nil, fmt.Errorf("wago: invoke prepared function: instance is closed")
	}
	if fn.scalarWideMask&1 == 0 {
		a0 = uint64(uint32(a0))
	}
	if fn.scalarWideMask&2 == 0 {
		a1 = uint64(uint32(a1))
	}
	if fn.scalarWideMask&4 == 0 {
		a2 = uint64(uint32(a2))
	}
	if fn.scalarWideMask&8 == 0 {
		a3 = uint64(uint32(a3))
	}
	if !fn.isolatedFast && !fn.directTrapIntFast {
		nativeExecutionMu.Lock()
		nativeExecutionEpoch++
		defer nativeExecutionMu.Unlock()
	}
	var result uint64
	var err error
	if fn.directLeafIntFast {
		result, err = in.eng.EnterPreparedLeafInt(fn.directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
	} else if fn.directTrapIntFast {
		result, err = in.eng.EnterPreparedTrapInt(fn.directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
	} else {
		result, err = in.eng.EnterPreparedInt(fn.directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
	}
	if err != nil {
		return nil, fmt.Errorf("wago: map prepared integer entry: %w", err)
	}
	if !fn.directLeafIntFast && wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
	}
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
	return out, nil
}

func (fn *PreparedFunction) invokeDirectTrapIntFixed(a0, a1, a2, a3 uint64) ([]uint64, error) {
	in := fn.in
	if in.isLogicallyClosed() {
		return nil, fmt.Errorf("wago: invoke prepared function: instance is closed")
	}
	if fn.scalarWideMask&1 == 0 {
		a0 = uint64(uint32(a0))
	}
	if fn.scalarWideMask&2 == 0 {
		a1 = uint64(uint32(a1))
	}
	if fn.scalarWideMask&4 == 0 {
		a2 = uint64(uint32(a2))
	}
	if fn.scalarWideMask&8 == 0 {
		a3 = uint64(uint32(a3))
	}
	result, err := in.eng.EnterPreparedTrapInt(fn.directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
	if err != nil {
		return nil, fmt.Errorf("wago: map prepared integer entry: %w", err)
	}
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
	}
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
	return out, nil
}
