//go:build arm64 && (linux || darwin || (windows && !tinygo))

package wago

import (
	"fmt"
	goruntime "runtime"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

const preparedDirectIntSupported = true
const preparedDirectIntPrivateSupported = true
const preparedDirectFPSupported = true

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
	if !fn.isolatedFast {
		nativeExecutionMu.Lock()
		nativeExecutionEpoch++
		defer nativeExecutionMu.Unlock()
	}
	var first, second uint64
	if fn.resultSlots == 2 {
		first, second = in.eng.EnterPreparedInt2(fn.directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
	} else {
		result, err := in.eng.EnterPreparedInt(fn.directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
		if err != nil {
			return nil, fmt.Errorf("wago: map prepared integer entry: %w", err)
		}
		first = result
	}
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	return fn.unpackDirectBankResults(first, second), nil
}

func (fn *PreparedFunction) invokeDirectFP(args []uint64) ([]uint64, error) {
	in := fn.in
	if in.isLogicallyClosed() {
		return nil, fmt.Errorf("wago: invoke prepared function: instance is closed")
	}
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
	if !fn.isolatedFast {
		return nil, fmt.Errorf("wago: direct prepared FP entry requires an isolated instance")
	}
	var first, second uint64
	if fn.resultSlots == 2 {
		first, second = in.eng.EnterPreparedFP2(fn.directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
	} else {
		first = in.eng.EnterPreparedFP(fn.directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
	}
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	return fn.unpackDirectBankResults(first, second), nil
}

func (fn *PreparedFunction) invokeDirectMixed(args []uint64) ([]uint64, error) {
	in := fn.in
	if in.isLogicallyClosed() {
		return nil, fmt.Errorf("wago: invoke prepared function: instance is closed")
	}
	if !fn.isolatedFast {
		return nil, fmt.Errorf("wago: direct prepared mixed entry requires an isolated instance")
	}
	g0, g1, f0, f1 := fn.packDirectMixedArgs(args)
	gpResult, fpResult := in.eng.EnterPreparedMixed(fn.directEntry, in.jm.LinMemBase(), g0, g1, f0, f1)
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	return fn.unpackDirectMixedResults(gpResult, fpResult), nil
}
