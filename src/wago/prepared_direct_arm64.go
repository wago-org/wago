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
	if in.isLogicallyClosed() {
		return nil, fmt.Errorf("wago: invoke prepared function: instance is closed")
	}
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
	if !fn.isolatedFast {
		nativeExecutionMu.Lock()
		nativeExecutionEpoch++
		defer nativeExecutionMu.Unlock()
	}
	result, err := in.eng.EnterPreparedInt(fn.directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
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
