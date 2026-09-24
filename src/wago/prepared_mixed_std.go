//go:build (amd64 || arm64) && (linux || darwin || windows) && !tinygo

package wago

import (
	"fmt"
	goruntime "runtime"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

func (fn *WasmFunc) invokeDirectMixed(args []uint64) ([]uint64, error) {
	in := fn.in
	if err := in.beginDirectInvocation(); err != nil {
		return nil, fmt.Errorf("wago: invoke Wasm function: %w", err)
	}
	if fn.directIsolated && fn.tryDirectGate() {
		out, err := fn.invokeDirectMixedSession(args)
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

func (fn *WasmFunc) invokeDirectMixedSession(args []uint64) ([]uint64, error) {
	return fn.in.invokeDirectMixedEntry(fn.directEntry, fn.directMixedInfo, fn.paramWide, fn.resultWide, args)
}

func (in *Instance) invokeDirectMixedEntry(entry uintptr, info uint8, paramWide []bool, resultWide []bool, args []uint64) ([]uint64, error) {
	if in.isLogicallyClosed() {
		return nil, fmt.Errorf("wago: invoke Wasm function: instance is closed")
	}
	var raw [8]uint64
	gp, fp := 0, 0
	for i, bits := range args {
		if !paramWide[i] {
			bits = uint64(uint32(bits))
		}
		if info&directMixedParamMask&(1<<i) != 0 {
			raw[4+fp] = bits
			fp++
		} else {
			raw[gp] = bits
			gp++
		}
	}
	wruntime.PreparePreparedIntTrap(in.trap)
	gpResult, fpResult := in.eng.EnterPreparedMixedBounded(entry, in.jm.LinMemBase(), &raw)
	if wruntime.PreparedIntTrapCode(in.trap) != wruntime.TrapNone {
		return nil, in.decorateTrap(wruntime.ConsumePreparedIntTrap(in.trap))
	}
	goruntime.KeepAlive(in)
	goruntime.KeepAlive(in.c)
	out := in.resultVals[:len(resultWide)]
	if len(resultWide) == 1 {
		result := gpResult
		if info&directMixedResultFP != 0 {
			result = fpResult
		}
		if !resultWide[0] {
			result = uint64(uint32(result))
		}
		out[0] = result
	}
	return out, nil
}
