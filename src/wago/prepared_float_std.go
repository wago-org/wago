//go:build (amd64 || arm64) && (linux || darwin || windows) && !tinygo

package wago

import (
	"fmt"
	goruntime "runtime"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

const preparedDirectFloatSupported = true

func (fn *WasmFunc) invokeDirectFloat(args []uint64) ([]uint64, error) {
	in := fn.in
	if err := in.beginDirectInvocation(); err != nil {
		return nil, fmt.Errorf("wago: invoke Wasm function: %w", err)
	}
	if fn.directIsolated && fn.tryDirectGate() {
		out, err := fn.invokeDirectFloatSession(args)
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

func (fn *WasmFunc) invokeDirectFloatSession(args []uint64) ([]uint64, error) {
	return fn.in.invokeDirectFloatEntry(fn.directEntry, fn.paramWide, fn.resultWide, args)
}

func (in *Instance) invokeDirectFloatEntry(entry uintptr, paramWide, resultWide []bool, args []uint64) ([]uint64, error) {
	if in.isLogicallyClosed() {
		return nil, fmt.Errorf("wago: invoke Wasm function: instance is closed")
	}
	if len(args) > 4 || len(resultWide) > 4 {
		return in.invokeDirectFloatOctEntry(entry, paramWide, resultWide, args)
	}
	var raw [4]uint64
	for i, bits := range args {
		if !paramWide[i] {
			bits = uint64(uint32(bits))
		}
		raw[i] = bits
	}
	wruntime.PreparePreparedIntTrap(in.trap)
	r0, r1, r2, r3 := in.eng.EnterPreparedFloatBounded(entry, in.jm.LinMemBase(), &raw)
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:len(resultWide)]
	results := [4]uint64{r0, r1, r2, r3}
	for i := range out {
		out[i] = results[i]
		if !resultWide[i] {
			out[i] = uint64(uint32(out[i]))
		}
	}
	return out, nil
}

func (in *Instance) invokeDirectFloatOctEntry(entry uintptr, paramWide, resultWide []bool, args []uint64) ([]uint64, error) {
	var raw [8]uint64
	for i, bits := range args {
		if !paramWide[i] {
			bits = uint64(uint32(bits))
		}
		raw[i] = bits
	}
	wruntime.PreparePreparedIntTrap(in.trap)
	r0, r1, r2, r3, r4, r5, r6, r7 := in.eng.EnterPreparedFloatOctBounded(entry, in.jm.LinMemBase(), &raw)
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:len(resultWide)]
	results := [8]uint64{r0, r1, r2, r3, r4, r5, r6, r7}
	for i := range out {
		out[i] = results[i]
		if !resultWide[i] {
			out[i] = uint64(uint32(out[i]))
		}
	}
	return out, nil
}
