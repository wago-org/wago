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
	gpResult, gpResult1, fpResult, fpResult1 := in.eng.EnterPreparedMixedBounded(entry, in.jm.LinMemBase(), &raw)
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
	} else if len(resultWide) >= 2 {
		gpResults := [2]uint64{gpResult, gpResult1}
		fpResults := [2]uint64{fpResult, fpResult1}
		gp, fp := 0, 0
		for i := range out {
			// Bit 7 tags the encoding, so result 3's bank is implied by the
			// two-GP/two-FP four-result contract.
			floatResult := i < 3 && info&(directMixedResultFP<<i) != 0 || i == 3 && fp < 2
			if floatResult {
				out[i] = fpResults[fp]
				fp++
			} else {
				out[i] = gpResults[gp]
				gp++
			}
			if !resultWide[i] {
				out[i] = uint64(uint32(out[i]))
			}
		}
	}
	return out, nil
}
