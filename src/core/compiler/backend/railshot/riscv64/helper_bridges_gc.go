// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

var gcStructBridgeSpecs = [...]helperBridge{
	{helper: gcStructAllocDefault, slots: 1},
	{helper: gcStructAllocOne, slots: 2},
	{helper: gcStructAllocOne, slots: 4},
	{helper: gcStructGet, slots: 3},
	{helper: gcStructSet, slots: 4},
	{helper: gcStructGetS, slots: 3},
	{helper: gcStructGetU, slots: 3},
	{helper: gcStructRefTest, slots: 4},
}

var gcArrayAndConversionBridgeSpecs = [...]helperBridge{
	{helper: gcAnyConvertExtern, slots: 1},
	{helper: gcExternConvertAny, slots: 1},
	{helper: gcArrayAllocDefault, slots: 2},
	{helper: gcArrayAllocFixed, slots: 4},
	{helper: gcArrayAllocFixed, slots: 5},
	{helper: gcArrayAllocUniform, slots: 3},
	{helper: gcArrayAllocData, slots: 4},
	{helper: gcArrayAllocElem, slots: 4},
	{helper: gcArrayDropElem, slots: 1},
	{helper: gcArrayFill, slots: 5},
	{helper: gcArrayCopy, slots: 7},
	{helper: gcArrayGet, slots: 3},
	{helper: gcArrayGetS, slots: 3},
	{helper: gcArrayGetU, slots: 3},
	{helper: gcArraySet, slots: 4},
	{helper: gcArrayLen, slots: 1},
	{helper: gcArrayInitData, slots: 6},
	{helper: gcArrayInitElem, slots: 6},
}

func gcObjectBridgeAllocationSlots(helper uint32, admitted int) (int, bool) {
	switch helper {
	case gcStructAllocDefault, gcAnyConvertExtern, gcExternConvertAny:
		return 1, true
	case gcStructAllocOne:
		return admitted, admitted >= 1 && admitted <= 256
	case gcArrayAllocDefault:
		return 2, true
	case gcArrayAllocUniform:
		return 3, true
	case gcArrayAllocFixed:
		return admitted, admitted >= 2 && admitted <= maxRootedScalarArrayFixed+2
	case gcArrayAllocData, gcArrayAllocElem, gcStructSet, gcArraySet:
		return 4, true
	case gcArrayFill:
		return 5, true
	case gcArrayInitElem:
		return 6, true
	default:
		return 0, false
	}
}

func gcObjectBridgeHasNoResult(helper uint32) bool {
	switch helper {
	case gcStructSet, gcArraySet, gcArrayDropElem, gcArrayFill, gcArrayCopy, gcArrayInitData, gcArrayInitElem:
		return true
	default:
		return false
	}
}

func gcObjectBridgeIsNonCollecting(helper uint32) bool {
	switch helper {
	case gcStructGet, gcStructSet, gcStructGetS, gcStructGetU, gcStructRefTest, gcStructRefCast,
		gcArrayGet, gcArrayGetS, gcArrayGetU, gcArraySet, gcArrayLen, gcArrayDropElem, gcArrayFill, gcArrayCopy, gcArrayInitData, gcArrayInitElem:
		return true
	default:
		return false
	}
}
