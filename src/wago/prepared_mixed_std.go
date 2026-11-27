// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (amd64 || arm64) && (linux || darwin || windows) && !tinygo

package wago

import (
	"fmt"
	goruntime "runtime"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

const mixedFPOnlyArgs = mixedFPArgsForImage &^ mixedGPArgsForImage
const mixedEitherBankArgs = mixedFPArgsForImage & mixedGPArgsForImage
const mixedFPOnlyResults = mixedFPResultsForImage &^ mixedGPResultsForImage
const mixedEitherBankResults = mixedFPResultsForImage & mixedGPResultsForImage

func (fn *WasmFunc) invokeDirectMixed(args []uint64) ([]uint64, error) {
	in := fn.in
	if fn.directIsolated && fn.tryBeginFastInvocation() {
		out, err := fn.invokeDirectMixedSession(args)
		in.endFastInvocation(in.pluginState.Load())
		return out, err
	}
	if err := in.beginDirectInvocation(); err != nil {
		return nil, fmt.Errorf("wago: invoke Wasm function: %w", err)
	}
	if fn.directIsolated && fn.tryDirectGate() {
		out, err := fn.invokeDirectMixedSession(args)
		fn.directGate.Unlock()
		in.endDirectInvocation()
		return out, err
	}
	return fn.invokeGeneralAfterDirectAdmission(args)
}

func (fn *WasmFunc) invokeDirectMixedSession(args []uint64) ([]uint64, error) {
	return fn.in.invokeDirectMixedEntry(fn.directEntry, fn.directMixedInfo, fn.paramWide, fn.resultWide, args)
}

func (in *Instance) invokeDirectMixedEntry(entry uintptr, info uint8, paramWide []bool, resultWide []bool, args []uint64) ([]uint64, error) {
	if in.isLogicallyClosed() {
		return nil, fmt.Errorf("wago: invoke Wasm function: instance is closed")
	}
	var raw [8]uint64
	if mixedGPArgsForImage|mixedFPArgsForImage != 0 {
		gp, fp := 0, 0
		for i, bits := range args {
			bits = directBankValue(bits, i, paramWide, mixedNarrowArgsForImage, mixedWideArgsForImage)
			lane := uint8(1 << i)
			switch {
			case mixedGPArgsForImage == 0:
				raw[4+i] = bits
			case mixedFPArgsForImage == 0:
				raw[i] = bits
			case mixedFPOnlyArgs&lane != 0 ||
				(mixedEitherBankArgs == directMixedParamMask || mixedEitherBankArgs&lane != 0) && info&directMixedParamMask&lane != 0:
				raw[4+fp] = bits
				fp++
			default:
				raw[gp] = bits
				gp++
			}
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
	if mixedResultFormsForImage&mixedSingleResult != 0 &&
		(mixedResultFormsForImage == mixedSingleResult || len(out) == 1) {
		result := gpResult
		if mixedFPResultsForImage&1 != 0 && (mixedGPResultsForImage&1 == 0 || info&directMixedResultFP != 0) {
			result = fpResult
		}
		out[0] = directBankValue(result, 0, resultWide, mixedNarrowResultsForImage, mixedWideResultsForImage)
	} else if mixedResultFormsForImage&mixedMultipleResults != 0 &&
		(mixedResultFormsForImage&mixedVoidResult == 0 || len(out) >= 2) {
		gpResults := [2]uint64{gpResult, gpResult1}
		fpResults := [2]uint64{fpResult, fpResult1}
		gp, fp := 0, 0
		for i := range out {
			var bits uint64
			lane := uint8(1 << i)
			switch {
			case mixedGPResultsForImage == 0:
				bits = fpResults[i]
			case mixedFPResultsForImage == 0:
				bits = gpResults[i]
			// Bit 7 is a tag. The fourth result uses the remaining lane.
			case mixedFPOnlyResults&lane != 0 ||
				(mixedEitherBankResults == directMixedParamMask || mixedEitherBankResults&lane != 0) &&
					(i < 3 && info&(directMixedResultFP<<i) != 0 || i == 3 && fp < 2):
				bits = fpResults[fp]
				fp++
			default:
				bits = gpResults[gp]
				gp++
			}
			out[i] = directBankValue(bits, i, resultWide, mixedNarrowResultsForImage, mixedWideResultsForImage)
		}
	}
	return out, nil
}
