// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo && !wago_no_gc

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

func selectLegacyGCModule(opts *CompileOptions) legacyGCModuleSelection {
	return legacyGCModuleSelection{
		i31: opts.i31Core || opts.gcRefEq || opts.gcRefTestTable || opts.gcRefTestConcrete || opts.gcRefTestAbstract ||
			opts.gcStructGet || opts.gcStructMutation || opts.gcStructNull || opts.gcStructPacked ||
			opts.gcStructBasic || opts.gcStructGlobals || opts.gcArrayLocal || opts.gcArrayUniform ||
			opts.gcArrayFixedLocal || opts.gcArrayFixedOfficial || opts.gcArrayPackedData || opts.gcArrayReference ||
			opts.gcArrayInitElem || opts.gcArrayInitData || opts.gcArrayFill || opts.gcArrayCopy,
		referenceCalls: opts.gcTypeSubtypingRecursiveFunctions || opts.gcRefTestConcrete || opts.gcRefTestAbstract ||
			opts.gcStructBasic || opts.gcArrayFixedOfficial || opts.gcArrayPackedData || opts.gcArrayReference ||
			opts.gcArrayInitElem || opts.gcArrayInitData || opts.gcArrayFill || opts.gcArrayCopy,
		referenceFunctions: opts.gcTypeSubtypingRecursiveFunctions || opts.gcTypeSubtypingRefFuncGlobals ||
			opts.gcTypeSubtypingRuntimeCallCast || opts.gcTypeSubtypingRefTestSingle ||
			opts.gcTypeSubtypingDuplicateRecursive != 0 || opts.gcTypeSubtypingExtendedRecursive != 0,
	}
}

func (state *moduleState) configureLegacyGCModule(m *wasm.Module, opts *CompileOptions) int {
	state.i31RefTest = opts.i31Core && admitRV64I31OfficialRefTest(m)
	state.i31RefEq = state.i31RefEq || opts.i31Core && (admitRV64ImmediateReferenceModule(m) || admitRV64I31RefEqPilot(m)) || opts.gcRefEq
	state.gcRefEq = opts.gcRefEq
	state.gcRefTestTable = opts.gcRefTestTable
	state.gcRefTestConcrete = opts.gcRefTestConcrete
	state.gcRefTestAbstract = opts.gcRefTestAbstract
	state.gcBrOnCastNullability = opts.gcBrOnCastNullability
	state.gcStructGet = opts.gcStructGet
	state.gcStructMutation = opts.gcStructMutation
	state.gcStructNull = opts.gcStructNull
	state.gcStructPacked = opts.gcStructPacked
	state.gcStructBasic = opts.gcStructBasic
	state.gcStructGlobals = opts.gcStructGlobals
	state.gcArrayLocal = opts.gcArrayLocal
	state.gcArrayUniform = opts.gcArrayUniform
	state.gcArrayFixedLocal = opts.gcArrayFixedLocal
	state.gcArrayFixedOfficial = opts.gcArrayFixedOfficial
	state.gcArrayPackedData = opts.gcArrayPackedData
	state.gcArrayReference = opts.gcArrayReference
	state.gcArrayInitElem = opts.gcArrayInitElem
	state.gcArrayInitData = opts.gcArrayInitData
	state.gcArrayInitDataWide = opts.gcArrayInitData && admitRV64GCArrayInitDataWide(m)
	state.gcArrayFill = opts.gcArrayFill
	state.gcArrayCopy = opts.gcArrayCopy
	state.gcTypeSubtypingRecursiveFunctions = opts.gcTypeSubtypingRecursiveFunctions
	state.gcTypeSubtypingRefFuncGlobals = opts.gcTypeSubtypingRefFuncGlobals
	state.gcTypeSubtypingRuntimeCallCast = opts.gcTypeSubtypingRuntimeCallCast
	state.gcTypeSubtypingFinalityCallCast = opts.gcTypeSubtypingFinalityCallCast
	state.gcTypeSubtypingTypedTableCall = opts.gcTypeSubtypingTypedTableCall
	state.gcTypeSubtypingRefTestSingle = opts.gcTypeSubtypingRefTestSingle
	state.gcTypeSubtypingRefTestVariant = opts.gcTypeSubtypingRefTestVariant
	state.gcTypeSubtypingLink = opts.gcTypeSubtypingLink
	state.gcTypeSubtypingDuplicateRecursive = opts.gcTypeSubtypingDuplicateRecursive != 0 || opts.gcTypeSubtypingExtendedRecursive != 0
	switch {
	case opts.gcArrayCopy:
		return 7
	case opts.gcArrayInitElem || opts.gcArrayInitData:
		return 6
	case opts.gcArrayFixedLocal || opts.gcArrayFixedOfficial || opts.gcArrayFill:
		return 5
	case opts.gcRefTestTable || opts.gcRefTestConcrete || opts.gcRefTestAbstract || opts.gcStructMutation || opts.gcStructNull || opts.gcStructPacked || opts.gcStructBasic || opts.gcArrayLocal || opts.gcArrayUniform || opts.gcArrayPackedData || opts.gcArrayReference:
		return 4
	case opts.gcStructGet:
		return 3
	case opts.gcRefEq:
		return 2
	default:
		return 0
	}
}
