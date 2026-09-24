//go:build (amd64 || arm64) && (linux || darwin || windows) && !tinygo

package wago

import (
	"fmt"
	goruntime "runtime"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

const preparedDirectWideSupported = true

func (fn *WasmFunc) invokeDirectIntWide(args []uint64) ([]uint64, error) {
	in := fn.in
	if err := in.beginDirectInvocation(); err != nil {
		return nil, fmt.Errorf("wago: invoke Wasm function: %w", err)
	}
	if fn.directIsolated && fn.tryDirectGate() {
		out, err := fn.invokeDirectIntWideSession(args)
		fn.directGate.Unlock()
		in.endDirectInvocation()
		return out, err
	}
	lease := in.lockPreparedInvocation()
	out, err := fn.invokeGeneralAdmitted(args)
	lease.unlock()
	in.endDirectInvocation()
	return out, err
}

func (fn *WasmFunc) invokeDirectIntWideSession(args []uint64) ([]uint64, error) {
	return fn.in.invokeDirectIntWideEntry(fn.directEntry, fn.paramWide, fn.resultWide, args)
}

func (in *Instance) invokeDirectIntWideEntry(entry uintptr, paramWide, resultWide []bool, args []uint64) ([]uint64, error) {
	if in.isLogicallyClosed() {
		return nil, fmt.Errorf("wago: invoke Wasm function: instance is closed")
	}
	var raw [8]uint64
	for i, bits := range args {
		if !paramWide[i] {
			bits = uint64(uint32(bits))
		}
		raw[i] = bits
	}
	wruntime.PreparePreparedIntTrap(in.trap)
	var results [8]uint64
	if len(resultWide) <= 5 {
		r0, r1, r2, r3, r4 := in.eng.EnterPreparedIntWideBounded(entry, in.jm.LinMemBase(), &raw)
		results[0], results[1], results[2], results[3], results[4] = r0, r1, r2, r3, r4
	} else {
		r0, r1, r2, r3, r4, r5, r6, r7 := in.eng.EnterPreparedIntOctBounded(entry, in.jm.LinMemBase(), &raw)
		results = [8]uint64{r0, r1, r2, r3, r4, r5, r6, r7}
	}
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:len(resultWide)]
	for i := range out {
		out[i] = results[i]
		if !resultWide[i] {
			out[i] = uint64(uint32(out[i]))
		}
	}
	return out, nil
}
