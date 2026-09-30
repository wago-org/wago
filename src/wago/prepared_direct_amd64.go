//go:build amd64 && (!tinygo || linux)

package wago

import (
	"fmt"
	"os"
	goruntime "runtime"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

const preparedDirectIntSupported = true
const preparedDirectIntPrivateSupported = false
const preparedIntCallBlockDefault = false

var preparedIntPreboundContextEnabled = os.Getenv("WAGO_PREPARED_INT_PREBOUND_CONTEXT") != "0"

func (fn *WasmFunc) initDirectIntCall() {
	if preparedIntPreboundContextEnabled && !preparedIntCallBlockEnabled && fn.directIntBounded {
		fn.in.eng.PrepareBoundedIntContext(fn.directLinMem)
	}
	if fn.directIntBounded && (preparedIntCallBlockEnabled || preparedIntPreboundContextEnabled) {
		fn.in.eng.PrepareIntCall(&fn.directIntCall, fn.directEntry, fn.directLinMem)
	}
	if preparedIntCallBlockEnabled && fn.directIntBounded {
		fn.directIntMode = preparedIntCallBlock
	} else if preparedIntPreboundContextEnabled && fn.directIntBounded {
		fn.directIntMode = preparedIntCallPrebound
	}
}

func (in *Instance) invokeCachedDirectInt1(ic *invokeCache, entry uintptr, arg uint64) ([]uint64, error) {
	if ic.scalarWideMask&1 == 0 {
		arg = uint64(uint32(arg))
	}
	wruntime.PreparePreparedIntTrap(in.trap)
	var result uint64
	var err error
	if ic.directIntBounded {
		result, err = in.eng.EnterPreparedIntBounded(entry, in.jm.LinMemBase(), arg, 0, 0, 0)
	} else {
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
	if ic.resultSlots == 1 {
		if ic.scalarResultWide {
			out[0] = result
		} else {
			out[0] = uint64(uint32(result))
		}
	}
	return out, nil
}

func (in *Instance) invokeCachedDirectI32ToI32(ic *invokeCache, arg uint64) ([]uint64, error) {
	wruntime.PreparePreparedIntTrap(in.trap)
	result, err := in.eng.EnterPreparedIntBounded(ic.directEntry, in.jm.LinMemBase(), uint64(uint32(arg)), 0, 0, 0)
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
	if err := in.beginDirectInvocation(); err != nil {
		return nil, fmt.Errorf("wago: invoke Wasm function: %w", err)
	}
	if fn.directIsolated && fn.tryDirectGate() {
		out, err := fn.invokeDirectIntSession(a0, a1, a2, a3)
		fn.directGate.Unlock()
		in.endDirectInvocation()
		return out, err
	}
	lease := in.lockPreparedInvocation()
	args := [4]uint64{a0, a1, a2, a3}
	out, err := fn.invokeGeneralAdmitted(args[:fn.paramSlots])
	lease.unlock()
	in.endDirectInvocation()
	return out, err
}

func (in *Instance) invokeDirectIntEntry(directEntry uintptr, paramSlots, resultSlots int, scalarWideMask uint8, scalarResultWide, _, _, bounded bool, a0, a1, a2, a3 uint64) ([]uint64, error) {
	if in.isLogicallyClosed() {
		return nil, fmt.Errorf("wago: invoke Wasm function: instance is closed")
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
	var result uint64
	var err error
	wruntime.PreparePreparedIntTrap(in.trap)
	if bounded {
		result, err = in.eng.EnterPreparedIntBounded(directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
	} else {
		result, err = in.eng.EnterPreparedInt(directEntry, in.jm.LinMemBase(), a0, a1, a2, a3)
	}
	if err != nil {
		return nil, fmt.Errorf("wago: map prepared integer entry: %w", err)
	}
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
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
	return out, nil
}
func (fn *WasmFunc) invokeDirectIntSession(a0, a1, a2, a3 uint64) ([]uint64, error) {
	if fn.resultSlots == 2 {
		return fn.invokeDirectIntPairSession(a0, a1, a2, a3)
	}
	in := fn.in
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
	wruntime.PreparePreparedIntTrap(in.trap)
	var result uint64
	var err error
	if fn.directIntMode == preparedIntCallPrebound {
		result = in.eng.EnterPreparedIntPreboundContextBounded(&fn.directIntCall, a0, a1, a2, a3)
	} else if fn.directIntMode == preparedIntCallBlock {
		result = in.eng.EnterPreparedIntCallBounded(&fn.directIntCall, a0, a1, a2, a3)
	} else if fn.directIntBounded {
		result, err = in.eng.EnterPreparedIntBounded(fn.directEntry, fn.directLinMem, a0, a1, a2, a3)
	} else {
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
	if fn.resultSlots == 1 {
		if fn.scalarResultWide {
			out[0] = result
		} else {
			out[0] = uint64(uint32(result))
		}
	}
	return out, nil
}
