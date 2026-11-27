// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// admitLegacyGCShape recognizes GC products with their own lowering and
// module-shape invariants. The ordinary source admission does not need them.
func admitLegacyGCShape(m *wasm.Module, referenceFunctions bool) bool {
	if admitRV64GCTypeSubtypingDeclarations(m) != 0 || admitRV64GCTypeSubtypingRecursiveFunctions(m) != 0 || admitRV64GCTypeSubtypingRefFuncGlobals(m) != 0 || admitRV64GCTypeSubtypingRuntimeCallCast(m) || admitRV64GCTypeSubtypingRuntimeFinalityCallCast(m) || admitRV64GCTypeSubtypingRuntimeTypedTableCall(m) || admitRV64GCTypeSubtypingRefTestSingle(m) || admitRV64GCTypeSubtypingLink(m) != 0 || admitRV64GCTypeSubtypingFinalityLink(m) != 0 || admitRV64GCTypeSubtypingStructLink(m) != 0 || admitRV64GCTypeSubtypingStructProjectionLink(m) != 0 || admitRV64GCTypeSubtypingStructMismatchLink(m) != 0 || admitRV64GCTypeSubtypingIndependentStructLink(m) != 0 || admitRV64GCTypeSubtypingExtendedProjectionLink(m) != 0 || admitRV64GCTypeSubtypingDuplicateRecursiveLink(m) != 0 || admitRV64GCTypeSubtypingExtendedRecursiveLink(m) != 0 || admitRV64GCTypeSubtypingCrossGroupMismatchLink(m) != 0 || admitRV64GCTypeSubtypingTransitiveMismatchLink(m) != 0 || admitRV64GCTypeSubtypingNonFlatExport(m) {
		return true
	}
	return referenceFunctions && (admitRV64GCRefEq(m) || admitRV64GCRefTestTable(m) || admitRV64GCRefTestConcrete(m) || admitRV64GCRefCastConcrete(m) || admitRV64GCBrOnCastConcrete(m) || admitRV64GCBrOnCastFailConcrete(m) || admitRV64GCBrOnCastNullability(m) || admitRV64GCBrOnCastFailNullability(m) || admitRV64GCRefTestAbstract(m) || admitRV64GCExtern(m) || admitRV64GCRefCastAbstract(m) || admitRV64GCBrOnCastAbstract(m) || admitRV64GCBrOnCastFailAbstract(m) || admitRV64GCStructGet(m) || admitRV64GCStructMutation(m) || admitRV64GCStructNull(m) || admitRV64GCStructPacked(m) || admitRV64GCStructBasic(m) || admitRV64GCStructNumericGlobals(m) || admitRV64GCArrayLocal(m) || admitRV64GCArrayUniform(m) || admitRV64GCArrayFixedLocal(m) || admitRV64GCArrayOfficial(m) || admitRV64GCArrayPackedData(m) || admitRV64GCArrayReference(m) || admitRV64GCArrayInitElem(m) || admitRV64GCArrayInitData(m) || admitRV64GCArrayFill(m) || admitRV64GCArrayCopy(m))
}

func admitLegacyGCWire(data []byte) bool {
	return admitRV64I31CoreWire(data) ||
		admitRV64GCRefEqWire(data) ||
		admitRV64GCRefTestTableWire(data) ||
		admitRV64GCRefTestConcreteWire(data) ||
		admitRV64GCRefTestAbstractWire(data) ||
		admitRV64GCExternWire(data) ||
		admitRV64GCRefCastAbstractWire(data) ||
		admitRV64GCRefCastConcreteWire(data) ||
		admitRV64GCBrOnCastAbstractWire(data) ||
		admitRV64GCBrOnCastFailAbstractWire(data) ||
		admitRV64GCBrOnCastConcreteWire(data) ||
		admitRV64GCBrOnCastFailConcreteWire(data) ||
		admitRV64GCBrOnCastNullabilityWire(data) ||
		admitRV64GCBrOnCastFailNullabilityWire(data) ||
		admitRV64GCTypeSubtypingDeclarationsWire(data) ||
		admitRV64GCTypeSubtypingRecursiveFunctionsWire(data) ||
		admitRV64GCTypeSubtypingRefFuncGlobalsWire(data) ||
		admitRV64GCTypeSubtypingRuntimeCallCastWire(data) ||
		admitRV64GCTypeSubtypingRuntimeFinalityCallCastWire(data) ||
		admitRV64GCTypeSubtypingRuntimeTypedTableCallWire(data) ||
		admitRV64GCTypeSubtypingRefTestSingleWire(data) ||
		admitRV64GCTypeSubtypingLinkWire(data) ||
		admitRV64GCTypeSubtypingFinalityLinkWire(data) ||
		admitRV64GCTypeSubtypingStructLinkWire(data) ||
		admitRV64GCTypeSubtypingStructProjectionLinkWire(data) ||
		admitRV64GCTypeSubtypingStructMismatchLinkWire(data) ||
		admitRV64GCTypeSubtypingIndependentStructLinkWire(data) ||
		admitRV64GCTypeSubtypingExtendedProjectionLinkWire(data) ||
		admitRV64GCTypeSubtypingDuplicateRecursiveLinkWire(data) ||
		admitRV64GCTypeSubtypingExtendedRecursiveLinkWire(data) ||
		admitRV64GCTypeSubtypingCrossGroupMismatchLinkWire(data) ||
		admitRV64GCTypeSubtypingTransitiveMismatchLinkWire(data) ||
		admitRV64GCTypeSubtypingNonFlatExportWire(data) ||
		admitRV64GCStructGetWire(data) ||
		admitRV64GCStructMutationWire(data) ||
		admitRV64GCStructNullWire(data) ||
		admitRV64GCStructPackedWire(data) ||
		admitRV64GCStructBasicWire(data) ||
		admitRV64GCStructNumericGlobalsWire(data) ||
		admitRV64GCArrayLocalWire(data) ||
		admitRV64GCArrayUniformWire(data) ||
		admitRV64GCArrayFixedLocalWire(data) ||
		admitRV64GCArrayFixedOfficialWire(data) ||
		admitRV64GCArrayDefaultOfficialWire(data) ||
		admitRV64GCArrayPackedDataWire(data) ||
		admitRV64GCArrayReferenceWire(data) ||
		admitRV64GCArrayInitElemWire(data) ||
		admitRV64GCArrayInitDataWire(data) ||
		admitRV64GCArrayFillWire(data) ||
		admitRV64GCArrayCopyWire(data)
}

// Legacy GC products are selected from exact source shapes. Generic rooted
// reference transport has its own admission path.
type legacyGCStateProducts struct {
	typeSubtypingDeclarations           bool
	typeSubtypingRefTestSingle          bool
	typeSubtypingReferenceFunctions     bool
	typeSubtypingRefFuncGlobals         bool
	typeSubtypingStructLink             bool
	typeSubtypingStructProjectionLink   bool
	typeSubtypingStructMismatchLink     bool
	typeSubtypingCrossGroupMismatchLink bool
	typeSubtypingTransitiveMismatchLink bool
	typeSubtypingIndependentStructLink  bool
	typeSubtypingExtendedProjectionLink bool
	typeSubtypingDuplicateRecursiveLink bool
	refEq                               bool
	refTestTable                        bool
	brOnCastNullability                 bool
	refTestConcrete                     bool
	extern                              bool
	refCastAbstract                     bool
	brOnCastAbstract                    bool
	refTestAbstract                     bool
	structGet                           bool
	structMutation                      bool
	structNull                          bool
	structPacked                        bool
	structBasic                         bool
	structNumericGlobals                bool
	arrayLocal                          bool
	arrayUniform                        bool
	arrayFixedLocal                     bool
	arrayFixedOfficial                  bool
	arrayPackedData                     bool
	arrayReference                      bool
	arrayInitElem                       bool
	arrayInitData                       bool
	arrayFill                           bool
	arrayCopy                           bool
	i31Core                             bool
}

func admitLegacyGCStateProducts(m *wasm.Module) legacyGCStateProducts {
	var products legacyGCStateProducts
	products.typeSubtypingDeclarations = admitRV64GCTypeSubtypingDeclarations(m) != 0
	products.typeSubtypingRefTestSingle = admitRV64GCTypeSubtypingRefTestSingle(m)
	products.typeSubtypingReferenceFunctions = admitRV64GCTypeSubtypingRecursiveFunctions(m) != 0 || admitRV64GCTypeSubtypingRuntimeCallCast(m) || admitRV64GCTypeSubtypingRuntimeFinalityCallCast(m) || admitRV64GCTypeSubtypingRuntimeTypedTableCall(m) || products.typeSubtypingRefTestSingle || admitRV64GCTypeSubtypingLink(m) != 0 || admitRV64GCTypeSubtypingDuplicateRecursiveLink(m) != 0
	products.typeSubtypingRefFuncGlobals = admitRV64GCTypeSubtypingRefFuncGlobals(m) != 0
	products.typeSubtypingStructLink = admitRV64GCTypeSubtypingStructLink(m) != 0
	products.typeSubtypingStructProjectionLink = admitRV64GCTypeSubtypingStructProjectionLink(m) != 0
	products.typeSubtypingStructMismatchLink = admitRV64GCTypeSubtypingStructMismatchLink(m) != 0
	products.typeSubtypingCrossGroupMismatchLink = admitRV64GCTypeSubtypingCrossGroupMismatchLink(m) != 0
	products.typeSubtypingTransitiveMismatchLink = admitRV64GCTypeSubtypingTransitiveMismatchLink(m) != 0
	products.typeSubtypingIndependentStructLink = admitRV64GCTypeSubtypingIndependentStructLink(m) != 0
	products.typeSubtypingExtendedProjectionLink = admitRV64GCTypeSubtypingExtendedProjectionLink(m) != 0
	products.typeSubtypingDuplicateRecursiveLink = admitRV64GCTypeSubtypingDuplicateRecursiveLink(m) != 0 || admitRV64GCTypeSubtypingExtendedRecursiveLink(m) != 0
	products.refEq = admitRV64GCRefEq(m)
	products.refTestTable = admitRV64GCRefTestTable(m)
	products.brOnCastNullability = admitRV64GCBrOnCastNullability(m) || admitRV64GCBrOnCastFailNullability(m)
	products.refTestConcrete = admitRV64GCRefTestConcrete(m) || admitRV64GCRefCastConcrete(m) || admitRV64GCBrOnCastConcrete(m) || admitRV64GCBrOnCastFailConcrete(m) || products.brOnCastNullability
	products.extern = admitRV64GCExtern(m)
	products.refCastAbstract = admitRV64GCRefCastAbstract(m)
	products.brOnCastAbstract = admitRV64GCBrOnCastAbstract(m) || admitRV64GCBrOnCastFailAbstract(m)
	products.refTestAbstract = admitRV64GCRefTestAbstract(m) || products.extern || products.refCastAbstract || products.brOnCastAbstract
	products.structGet = admitRV64GCStructGet(m)
	products.structMutation = admitRV64GCStructMutation(m)
	products.structNull = admitRV64GCStructNull(m)
	products.structPacked = admitRV64GCStructPacked(m)
	products.structBasic = admitRV64GCStructBasic(m)
	products.structNumericGlobals = admitRV64GCStructNumericGlobals(m)
	products.arrayLocal = admitRV64GCArrayLocal(m)
	products.arrayUniform = admitRV64GCArrayUniform(m)
	products.arrayFixedLocal = admitRV64GCArrayFixedLocal(m)
	products.arrayFixedOfficial = admitRV64GCArrayOfficial(m)
	products.arrayPackedData = admitRV64GCArrayPackedData(m)
	products.arrayReference = admitRV64GCArrayReference(m)
	products.arrayInitElem = admitRV64GCArrayInitElem(m)
	products.arrayInitData = admitRV64GCArrayInitData(m)
	products.arrayFill = admitRV64GCArrayFill(m)
	products.arrayCopy = admitRV64GCArrayCopy(m)
	products.i31Core = admitRV64I31Core(m) || products.refEq || products.refTestTable || products.refTestConcrete || products.refTestAbstract || products.structGet || products.structMutation || products.structNull || products.structPacked || products.structBasic || products.structNumericGlobals || products.arrayLocal || products.arrayUniform || products.arrayFixedLocal || products.arrayFixedOfficial || products.arrayPackedData || products.arrayReference || products.arrayInitElem || products.arrayInitData || products.arrayFill || products.arrayCopy
	return products
}
