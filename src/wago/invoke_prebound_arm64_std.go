//go:build arm64 && !tinygo && (linux || darwin || windows)

package wago

import (
	"fmt"
	"runtime"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

const invokeCachedPreboundIntSupported = true

type cachedInvokeIntCall = wruntime.PreparedIntCall

func (in *Instance) prepareCachedInvokeIntCall(call *cachedInvokeIntCall, entry, linMem uintptr) {
	in.eng.PrepareIntCall(call, entry, linMem)
}

func (in *Instance) invokeCachedPreboundInt(ic *invokeCache, args []uint64) ([]uint64, error) {
	if in.isLogicallyClosed() {
		return nil, fmt.Errorf("wago: invoke Wasm function: instance is closed")
	}
	var a0, a1, a2, a3 uint64
	switch len(args) {
	case 4:
		a3 = args[3]
		if ic.scalarWideMask&8 == 0 {
			a3 = uint64(uint32(a3))
		}
		fallthrough
	case 3:
		a2 = args[2]
		if ic.scalarWideMask&4 == 0 {
			a2 = uint64(uint32(a2))
		}
		fallthrough
	case 2:
		a1 = args[1]
		if ic.scalarWideMask&2 == 0 {
			a1 = uint64(uint32(a1))
		}
		fallthrough
	case 1:
		a0 = args[0]
		if ic.scalarWideMask&1 == 0 {
			a0 = uint64(uint32(a0))
		}
	}
	wruntime.PreparePreparedIntTrap(in.trap)
	result := in.eng.EnterPreparedIntPreboundContextBounded(&ic.directIntCall, a0, a1, a2, a3)
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
	}
	runtime.KeepAlive(in)
	runtime.KeepAlive(in.c)
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
