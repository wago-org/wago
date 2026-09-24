//go:build !tinygo && (amd64 || arm64) && (linux || darwin || windows)

package wago

import (
	"fmt"
	goruntime "runtime"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

const preparedDirectPairSupported = true

func (in *Instance) invokeDirectIntPairEntry(entry uintptr, paramWideMask uint8, resultWide []bool, args []uint64) ([]uint64, error) {
	if in.isLogicallyClosed() {
		return nil, fmt.Errorf("wago: invoke Wasm function: instance is closed")
	}
	var raw [4]uint64
	for i, bits := range args {
		if paramWideMask&(1<<i) == 0 {
			bits = uint64(uint32(bits))
		}
		raw[i] = bits
	}
	wruntime.PreparePreparedIntTrap(in.trap)
	r0, r1 := in.eng.EnterPreparedIntPairBounded(entry, in.jm.LinMemBase(), raw[0], raw[1], raw[2], raw[3])
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:2]
	if resultWide[0] {
		out[0] = r0
	} else {
		out[0] = uint64(uint32(r0))
	}
	if resultWide[1] {
		out[1] = r1
	} else {
		out[1] = uint64(uint32(r1))
	}
	return out, nil
}

// invokeDirectIntPairSession uses the compiler's bounded two-register integer
// return ABI. The owner has already reserved the instance and direct gate.
func (fn *WasmFunc) invokeDirectIntPairSession(a0, a1, a2, a3 uint64) ([]uint64, error) {
	in := fn.in
	args := [4]uint64{a0, a1, a2, a3}
	for i := 0; i < fn.paramSlots; i++ {
		if fn.scalarWideMask&(1<<i) == 0 {
			args[i] = uint64(uint32(args[i]))
		}
	}
	wruntime.PreparePreparedIntTrap(in.trap)
	r0, r1 := in.eng.EnterPreparedIntPairBounded(fn.directEntry, fn.directLinMem, args[0], args[1], args[2], args[3])
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
	}
	goruntime.KeepAlive(fn)
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:2]
	if fn.resultWide[0] {
		out[0] = r0
	} else {
		out[0] = uint64(uint32(r0))
	}
	if fn.resultWide[1] {
		out[1] = r1
	} else {
		out[1] = uint64(uint32(r1))
	}
	return out, nil
}
