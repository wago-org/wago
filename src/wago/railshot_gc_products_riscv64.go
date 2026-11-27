// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import "errors"

func railshotNeedsLegacyGCProducts(c *Compiled) bool {
	if !wasmGCBuilt {
		return false
	}
	if !rv64SourceContractValid(c) {
		return true
	}
	// The generator has admitted this execution contract with the full
	// classifier. Keep the legacy paths if they establish any permissions.
	products, err := admitRV64LegacyGCProducts(c, rv64PrimitiveReferenceContract(c), rv64FreshRootedGC(c), errors.New)
	return err != nil || products.any
}

// rv64LegacyGCProducts records the permissions established by the exact
// metadata and code checks for older GC execution profiles.
type rv64LegacyGCProducts struct {
	any                                 bool
	typedReferences                     bool
	bulkMemory                          bool
	typeSubtypingDeclarations           bool
	typeSubtypingRecursiveFunctions     bool
	typeSubtypingRefFuncGlobals         bool
	typeSubtypingRuntimeCallCast        bool
	typeSubtypingRefTestSingle          bool
	typeSubtypingLink                   bool
	typeSubtypingFinalityLink           bool
	typeSubtypingStructLink             bool
	typeSubtypingStructProjectionLink   bool
	typeSubtypingStructMismatchLink     bool
	typeSubtypingIndependentStructLink  bool
	typeSubtypingExtendedProjectionLink bool
	typeSubtypingDuplicateRecursiveLink bool
	typeSubtypingExtendedRecursiveLink  bool
	typeSubtypingCrossGroupMismatchLink bool
	typeSubtypingTransitiveMismatchLink bool
	typeSubtypingNonFlatExport          bool
	structNumericGlobals                bool
	refTestAbstract                     bool
	extern                              bool
	refCastAbstract                     bool
	brOnCastAbstract                    bool
	brOnCastFailAbstract                bool
	brOnCastNullability                 bool
	brOnCastFailNullability             bool
	structBasic                         bool
	arrayFixedOfficial                  bool
	arrayDefaultOfficial                bool
	arrayPackedData                     bool
	arrayReference                      bool
	arrayInitElem                       bool
	arrayInitData                       bool
	arrayFill                           bool
	arrayCopy                           bool
	structPacked                        bool
}

func (p *rv64LegacyGCProducts) admitted() bool {
	return rv64LegacyGCProductsForImage && p.any
}

func (p *rv64LegacyGCProducts) admitsLinkedSubtype() bool {
	return rv64LegacyGCProductsForImage && (p.typeSubtypingLink || p.typeSubtypingFinalityLink || p.typeSubtypingStructLink ||
		p.typeSubtypingStructProjectionLink || p.typeSubtypingStructMismatchLink || p.typeSubtypingIndependentStructLink ||
		p.typeSubtypingExtendedProjectionLink || p.typeSubtypingDuplicateRecursiveLink || p.typeSubtypingExtendedRecursiveLink ||
		p.typeSubtypingCrossGroupMismatchLink || p.typeSubtypingTransitiveMismatchLink)
}

func (p *rv64LegacyGCProducts) admitsFunctionOnlySubtype() bool {
	return rv64LegacyGCProductsForImage && (p.typeSubtypingDeclarations || p.typeSubtypingRecursiveFunctions || p.typeSubtypingRefFuncGlobals ||
		p.typeSubtypingRuntimeCallCast || p.typeSubtypingRefTestSingle || p.typeSubtypingNonFlatExport ||
		p.admitsLinkedSubtype())
}

func (p *rv64LegacyGCProducts) admitsExternrefSignature() bool {
	return rv64LegacyGCProductsForImage && (p.refTestAbstract || p.extern || p.refCastAbstract ||
		p.brOnCastAbstract || p.brOnCastFailAbstract)
}

func (p *rv64LegacyGCProducts) admitsAnyrefSignature() bool {
	return rv64LegacyGCProductsForImage && (p.extern || p.brOnCastNullability || p.brOnCastFailNullability ||
		p.structBasic || p.arrayFixedOfficial || p.arrayDefaultOfficial ||
		p.arrayPackedData || p.arrayReference || p.arrayInitElem ||
		p.arrayInitData || p.arrayFill || p.arrayCopy)
}

func (p *rv64LegacyGCProducts) admitsStructInitializers() bool {
	return rv64LegacyGCProductsForImage && (p.structPacked || p.structBasic || p.structNumericGlobals)
}

func (p *rv64LegacyGCProducts) admitsArrayInitializers() bool {
	return rv64LegacyGCProductsForImage &&
		(p.arrayFixedOfficial || p.arrayDefaultOfficial || p.arrayInitElem || p.arrayInitData || p.arrayFill || p.arrayCopy)
}

func admitRV64LegacyGCProducts(c *Compiled, primitiveReferences, freshRootedGC bool, reject func(string) error) (rv64LegacyGCProducts, error) {
	exactGCRefEq := rv64OfficialGCRefEq(c)
	exactGCRefTestTable := rv64GCRefTestTable(c)
	exactGCRefTestConcrete := rv64GCRefTestConcrete(c)
	exactGCRefCastConcrete := rv64GCRefCastConcrete(c)
	exactGCRefTestAbstract := rv64GCRefTestAbstract(c)
	exactGCExtern := rv64GCExtern(c)
	exactGCRefCastAbstract := rv64GCRefCastAbstract(c)
	exactGCBrOnCastAbstract := rv64GCBrOnCastAbstract(c)
	exactGCBrOnCastFailAbstract := rv64GCBrOnCastFailAbstract(c)
	exactGCBrOnCastConcrete := rv64GCBrOnCastConcrete(c)
	exactGCBrOnCastFailConcrete := rv64GCBrOnCastFailConcrete(c)
	exactGCBrOnCastNullability := rv64GCBrOnCastNullability(c)
	exactGCBrOnCastFailNullability := rv64GCBrOnCastFailNullability(c)
	exactGCTypeSubtypingDeclarations := rv64GCTypeSubtypingDeclarations(c)
	exactGCTypeSubtypingRecursiveFunctions := rv64GCTypeSubtypingRecursiveFunctions(c)
	exactGCTypeSubtypingRefFuncGlobals := rv64GCTypeSubtypingRefFuncGlobals(c)
	exactGCTypeSubtypingRuntimeFinalityCallCast := rv64GCTypeSubtypingRuntimeFinalityCallCast(c)
	exactGCTypeSubtypingRuntimeTypedTableCall := rv64GCTypeSubtypingRuntimeTypedTableCall(c)
	exactGCTypeSubtypingRefTestSingle := rv64GCTypeSubtypingRefTestSingle(c)
	exactGCTypeSubtypingLink := rv64GCTypeSubtypingLink(c)
	exactGCTypeSubtypingFinalityLink := rv64GCTypeSubtypingFinalityLink(c)
	exactGCTypeSubtypingStructLink := rv64GCTypeSubtypingStructLink(c)
	exactGCTypeSubtypingStructProjectionLink := rv64GCTypeSubtypingStructProjectionLink(c)
	exactGCTypeSubtypingStructMismatchLink := rv64GCTypeSubtypingStructMismatchLink(c)
	exactGCTypeSubtypingIndependentStructLink := rv64GCTypeSubtypingIndependentStructLink(c)
	exactGCTypeSubtypingExtendedProjectionLink := rv64GCTypeSubtypingExtendedProjectionLink(c)
	exactGCTypeSubtypingDuplicateRecursiveLink := rv64GCTypeSubtypingDuplicateRecursiveLink(c)
	exactGCTypeSubtypingExtendedRecursiveLink := rv64GCTypeSubtypingExtendedRecursiveLink(c)
	exactGCTypeSubtypingCrossGroupMismatchLink := rv64GCTypeSubtypingCrossGroupMismatchLink(c)
	exactGCTypeSubtypingTransitiveMismatchLink := rv64GCTypeSubtypingTransitiveMismatchLink(c)
	exactGCTypeSubtypingNonFlatExport := rv64GCTypeSubtypingNonFlatExport(c)
	exactGCTypeSubtypingRuntimeCallCast := rv64GCTypeSubtypingRuntimeCallCast(c) || exactGCTypeSubtypingRuntimeFinalityCallCast || exactGCTypeSubtypingRuntimeTypedTableCall
	exactGCStructGet := rv64GCStructGet(c)
	exactGCStructMutation := rv64GCStructMutation(c)
	exactGCStructNull := rv64GCStructNull(c)
	exactGCStructPacked := rv64GCStructPacked(c)
	exactGCStructBasic := rv64GCStructBasic(c)
	exactGCStructNumericGlobals := rv64GCStructNumericGlobals(c)
	exactGCArrayLocal := rv64GCArrayLocal(c)
	exactGCArrayUniform := rv64GCArrayUniform(c)
	exactGCArrayFixedLocal := rv64GCArrayFixedLocal(c)
	exactGCArrayFixedOfficial := rv64GCArrayFixedOfficial(c)
	exactGCArrayDefaultOfficial := rv64GCArrayDefaultOfficial(c)
	exactGCArrayPackedData := rv64GCArrayPackedData(c)
	exactGCArrayReference := rv64GCArrayReference(c)
	exactGCArrayInitElem := rv64GCArrayInitElem(c)
	exactGCArrayInitData := rv64GCArrayInitData(c)
	exactGCArrayFill := rv64GCArrayFill(c)
	exactGCArrayCopy := rv64GCArrayCopy(c)
	if !primitiveReferences && rv64GCTypeSubtypingRecursiveFunctionsExecutionCode(c) && !exactGCTypeSubtypingRecursiveFunctions {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping recursive-function image requires its exact retained metadata")
	}
	if !primitiveReferences && rv64GCTypeSubtypingRecursiveFunctionsProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingRecursiveFunctions {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping recursive-function metadata requires its exact retained image")
	}
	if !primitiveReferences && !freshRootedGC && rv64GCTypeSubtypingRefFuncGlobalsExecutionCode(c) && rv64GCTypeSubtypingRefFuncGlobalsExecutionClaim(c) && !exactGCTypeSubtypingRefFuncGlobals && !exactGCTypeSubtypingStructLink && !exactGCTypeSubtypingStructProjectionLink && !exactGCTypeSubtypingStructMismatchLink && !exactGCTypeSubtypingIndependentStructLink && !exactGCTypeSubtypingExtendedProjectionLink && !exactGCTypeSubtypingDuplicateRecursiveLink && !exactGCTypeSubtypingExtendedRecursiveLink && !exactGCTypeSubtypingCrossGroupMismatchLink && !exactGCTypeSubtypingTransitiveMismatchLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping ref.func-global image requires its exact retained metadata")
	}
	if !primitiveReferences && !freshRootedGC && rv64GCTypeSubtypingRefFuncGlobalsProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingRefFuncGlobals {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping ref.func-global metadata requires its exact retained image")
	}
	if !primitiveReferences && rv64GCTypeSubtypingRuntimeCallCastExecutionCode(c) && !exactGCTypeSubtypingRuntimeCallCast {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping runtime call/cast image requires its exact retained metadata")
	}
	if !primitiveReferences && rv64GCTypeSubtypingRuntimeCallCastProfileWithCode(c, true, false) && !exactGCTypeSubtypingRuntimeCallCast {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping runtime call/cast metadata requires its exact retained image")
	}
	if !primitiveReferences && rv64GCTypeSubtypingRuntimeFinalityCallCastExecutionCode(c) && !exactGCTypeSubtypingRuntimeFinalityCallCast {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping runtime finality call/cast image requires its exact retained metadata")
	}
	if !primitiveReferences && rv64GCTypeSubtypingRuntimeFinalityCallCastProfileWithCode(c, true, false) && !exactGCTypeSubtypingRuntimeFinalityCallCast {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping runtime finality call/cast metadata requires its exact retained image")
	}
	if !primitiveReferences && rv64GCTypeSubtypingRuntimeTypedTableCallExecutionCode(c) && !exactGCTypeSubtypingRuntimeTypedTableCall {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping runtime typed-table call image requires its exact retained metadata")
	}
	if !primitiveReferences && rv64GCTypeSubtypingRuntimeTypedTableCallProfileWithCode(c, true, false) && !exactGCTypeSubtypingRuntimeTypedTableCall {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping runtime typed-table call metadata requires its exact retained image")
	}
	if !primitiveReferences && rv64GCTypeSubtypingRefTestSingleExecutionCode(c) && !exactGCTypeSubtypingRefTestSingle {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping ref.test image requires its exact retained metadata")
	}
	if !primitiveReferences && rv64GCTypeSubtypingRefTestSingleProfileWithCode(c, true, false) && !exactGCTypeSubtypingRefTestSingle {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping ref.test metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingLinkExecutionCode(c) && rv64GCTypeSubtypingLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingFinalityLinkExecutionCode(c) && rv64GCTypeSubtypingFinalityLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingFinalityLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping finality-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingFinalityLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingFinalityLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping finality-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingStructLinkExecutionCode(c) && rv64GCTypeSubtypingStructLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingStructLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping struct-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingStructLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingStructLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping struct-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingStructProjectionLinkExecutionCode(c) && rv64GCTypeSubtypingStructProjectionLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingStructProjectionLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping struct-projection-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingStructProjectionLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingStructProjectionLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping struct-projection-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingStructMismatchLinkExecutionCode(c) && rv64GCTypeSubtypingStructMismatchLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingStructMismatchLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping struct-mismatch-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingStructMismatchLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingStructMismatchLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping struct-mismatch-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingIndependentStructLinkExecutionCode(c) && rv64GCTypeSubtypingIndependentStructLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingIndependentStructLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping independent-struct-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingIndependentStructLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingIndependentStructLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping independent-struct-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingExtendedProjectionLinkExecutionCode(c) && rv64GCTypeSubtypingExtendedProjectionLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingExtendedProjectionLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping extended-projection-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingExtendedProjectionLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingExtendedProjectionLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping extended-projection-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingDuplicateRecursiveLinkExecutionCode(c) && rv64GCTypeSubtypingDuplicateRecursiveLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingDuplicateRecursiveLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping duplicate-recursive-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingDuplicateRecursiveLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingDuplicateRecursiveLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping duplicate-recursive-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingExtendedRecursiveLinkExecutionCode(c) && rv64GCTypeSubtypingExtendedRecursiveLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingExtendedRecursiveLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping extended-recursive-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingExtendedRecursiveLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingExtendedRecursiveLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping extended-recursive-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingCrossGroupMismatchLinkExecutionCode(c) && rv64GCTypeSubtypingCrossGroupMismatchLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingCrossGroupMismatchLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping cross-group-mismatch-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingCrossGroupMismatchLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingCrossGroupMismatchLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping cross-group-mismatch-link metadata requires its exact retained image")
	}
	if rv64GCTypeSubtypingTransitiveMismatchLinkExecutionCode(c) && rv64GCTypeSubtypingTransitiveMismatchLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingTransitiveMismatchLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping transitive-mismatch-link image requires its exact retained metadata")
	}
	if rv64GCTypeSubtypingTransitiveMismatchLinkProfileWithCode(c, true, false) != 0 && !exactGCTypeSubtypingTransitiveMismatchLink {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping transitive-mismatch-link metadata requires its exact retained image")
	}
	if !primitiveReferences && rv64GCTypeSubtypingNonFlatExportExecutionCode(c) && !exactGCTypeSubtypingNonFlatExport {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping non-flat-export image requires its exact retained metadata")
	}
	if !primitiveReferences && rv64GCTypeSubtypingNonFlatExportProfileWithCode(c, true, false) && !exactGCTypeSubtypingNonFlatExport {
		return rv64LegacyGCProducts{}, reject("gc/type-subtyping non-flat-export metadata requires its exact retained image")
	}
	// A general rooted execution contract binds code, exact type/state receipts
	// and root maps. Legacy image collisions constrain only legacy contracts.
	if !freshRootedGC {
		if rv64OfficialGCRefEqExecutionCode(c) && !exactGCRefEq {
			return rv64LegacyGCProducts{}, reject("allocating gc/ref.eq image requires its exact retained metadata")
		}
		if rv64GCRefTestTableExecutionCode(c) && !exactGCRefTestTable {
			return rv64LegacyGCProducts{}, reject("gc object-table ref.test image requires its exact retained metadata")
		}
		if rv64GCRefTestConcreteExecutionCode(c) && !exactGCRefTestConcrete {
			return rv64LegacyGCProducts{}, reject("gc concrete ref.test image requires its exact retained metadata")
		}
		if rv64GCRefCastConcreteExecutionCode(c) && !exactGCRefCastConcrete {
			return rv64LegacyGCProducts{}, reject("gc concrete ref.cast image requires its exact retained metadata")
		}
		if rv64GCRefTestAbstractExecutionCode(c) && !exactGCRefTestAbstract {
			return rv64LegacyGCProducts{}, reject("gc abstract ref.test image requires its exact retained metadata")
		}
		if rv64GCExternExecutionCode(c) && !exactGCExtern {
			return rv64LegacyGCProducts{}, reject("gc extern conversion image requires its exact retained metadata")
		}
		if rv64GCRefCastAbstractExecutionCode(c) && !exactGCRefCastAbstract {
			return rv64LegacyGCProducts{}, reject("gc abstract ref.cast image requires its exact retained metadata")
		}
		if rv64GCBrOnCastAbstractExecutionCode(c) && !exactGCBrOnCastAbstract {
			return rv64LegacyGCProducts{}, reject("gc abstract br_on_cast image requires its exact retained metadata")
		}
		if rv64GCBrOnCastFailAbstractExecutionCode(c) && !exactGCBrOnCastFailAbstract {
			return rv64LegacyGCProducts{}, reject("gc abstract br_on_cast_fail image requires its exact retained metadata")
		}
		if rv64GCBrOnCastConcreteExecutionCode(c) && !exactGCBrOnCastConcrete {
			return rv64LegacyGCProducts{}, reject("gc concrete br_on_cast image requires its exact retained metadata")
		}
		if rv64GCBrOnCastFailConcreteExecutionCode(c) && !exactGCBrOnCastFailConcrete {
			return rv64LegacyGCProducts{}, reject("gc concrete br_on_cast_fail image requires its exact retained metadata")
		}
		if rv64GCBrOnCastNullabilityExecutionCode(c) && !exactGCBrOnCastNullability {
			return rv64LegacyGCProducts{}, reject("gc nullability br_on_cast image requires its exact retained metadata")
		}
		if rv64GCBrOnCastFailNullabilityExecutionCode(c) && !exactGCBrOnCastFailNullability {
			return rv64LegacyGCProducts{}, reject("gc nullability br_on_cast_fail image requires its exact retained metadata")
		}
		if rv64GCStructGetExecutionCode(c) && !exactGCStructGet {
			return rv64LegacyGCProducts{}, reject("allocating gc/struct.get image requires its exact retained metadata")
		}
		if rv64GCStructMutationExecutionCode(c) && !exactGCStructMutation {
			return rv64LegacyGCProducts{}, reject("allocating gc/struct mutation image requires its exact retained metadata")
		}
		if rv64GCStructNullExecutionCode(c) && !exactGCStructNull {
			return rv64LegacyGCProducts{}, reject("gc/struct null image requires its exact retained metadata")
		}
		if rv64GCStructPackedExecutionCode(c) && !exactGCStructPacked {
			return rv64LegacyGCProducts{}, reject("gc/struct packed image requires its exact retained metadata")
		}
		if rv64GCStructBasicExecutionCode(c) && !exactGCStructBasic {
			return rv64LegacyGCProducts{}, reject("gc/struct basic image requires its exact retained metadata")
		}
		if rv64GCArrayLocalExecutionCode(c) && !exactGCArrayLocal {
			return rv64LegacyGCProducts{}, reject("allocating gc/array local image requires its exact retained metadata")
		}
		if rv64GCArrayUniformExecutionCode(c) && !exactGCArrayUniform {
			return rv64LegacyGCProducts{}, reject("allocating gc/array uniform image requires its exact retained metadata")
		}
		if rv64GCArrayFixedLocalExecutionCode(c) && !exactGCArrayFixedLocal {
			return rv64LegacyGCProducts{}, reject("allocating gc/array fixed local image requires its exact retained metadata")
		}
		if rv64GCArrayFixedOfficialExecutionCode(c) && !exactGCArrayFixedOfficial {
			return rv64LegacyGCProducts{}, reject("allocating official gc/array fixed image requires its exact retained metadata")
		}
		if rv64GCArrayDefaultOfficialExecutionCode(c) && !exactGCArrayDefaultOfficial {
			return rv64LegacyGCProducts{}, reject("allocating official gc/array default image requires its exact retained metadata")
		}
		if rv64GCArrayPackedDataExecutionCode(c) && !exactGCArrayPackedData {
			return rv64LegacyGCProducts{}, reject("allocating gc/array packed data image requires its exact retained metadata")
		}
		if rv64GCArrayReferenceExecutionCode(c) && !exactGCArrayReference {
			return rv64LegacyGCProducts{}, reject("allocating gc/array reference image requires its exact retained metadata")
		}
		if rv64GCArrayInitElemExecutionCode(c) && !exactGCArrayInitElem {
			return rv64LegacyGCProducts{}, reject("allocating gc/array init elem image requires its exact retained metadata")
		}
		if rv64GCArrayInitDataExecutionCode(c) && !exactGCArrayInitData {
			return rv64LegacyGCProducts{}, reject("allocating gc/array init data image requires its exact retained metadata")
		}
		if rv64GCArrayFillExecutionCode(c) && !exactGCArrayFill {
			return rv64LegacyGCProducts{}, reject("allocating gc/array fill image requires its exact retained metadata")
		}
		if rv64GCArrayCopyExecutionCode(c) && !exactGCArrayCopy {
			return rv64LegacyGCProducts{}, reject("allocating gc/array copy image requires its exact retained metadata")
		}
	}
	return rv64LegacyGCProducts{
		any:                                 exactGCTypeSubtypingDeclarations || exactGCTypeSubtypingRecursiveFunctions || exactGCTypeSubtypingRefFuncGlobals || exactGCTypeSubtypingRuntimeCallCast || exactGCTypeSubtypingRefTestSingle || exactGCTypeSubtypingLink || exactGCTypeSubtypingFinalityLink || exactGCTypeSubtypingStructLink || exactGCTypeSubtypingStructProjectionLink || exactGCTypeSubtypingStructMismatchLink || exactGCTypeSubtypingIndependentStructLink || exactGCTypeSubtypingExtendedProjectionLink || exactGCTypeSubtypingDuplicateRecursiveLink || exactGCTypeSubtypingExtendedRecursiveLink || exactGCTypeSubtypingCrossGroupMismatchLink || exactGCTypeSubtypingTransitiveMismatchLink || exactGCTypeSubtypingNonFlatExport || exactGCRefEq || exactGCRefTestTable || exactGCRefTestConcrete || exactGCRefCastConcrete || exactGCRefTestAbstract || exactGCExtern || exactGCRefCastAbstract || exactGCBrOnCastAbstract || exactGCBrOnCastFailAbstract || exactGCBrOnCastConcrete || exactGCBrOnCastFailConcrete || exactGCBrOnCastNullability || exactGCBrOnCastFailNullability || exactGCStructGet || exactGCStructMutation || exactGCStructNull || exactGCStructPacked || exactGCStructBasic || exactGCStructNumericGlobals || exactGCArrayLocal || exactGCArrayUniform || exactGCArrayFixedLocal || exactGCArrayFixedOfficial || exactGCArrayDefaultOfficial || exactGCArrayPackedData || exactGCArrayReference || exactGCArrayInitElem || exactGCArrayInitData || exactGCArrayFill || exactGCArrayCopy,
		typedReferences:                     exactGCTypeSubtypingDeclarations || exactGCTypeSubtypingRecursiveFunctions || exactGCTypeSubtypingRefFuncGlobals || exactGCTypeSubtypingRuntimeCallCast || exactGCTypeSubtypingRefTestSingle || exactGCTypeSubtypingLink || exactGCTypeSubtypingFinalityLink || exactGCTypeSubtypingStructLink || exactGCTypeSubtypingStructProjectionLink || exactGCTypeSubtypingStructMismatchLink || exactGCTypeSubtypingIndependentStructLink || exactGCTypeSubtypingExtendedProjectionLink || exactGCTypeSubtypingDuplicateRecursiveLink || exactGCTypeSubtypingExtendedRecursiveLink || exactGCTypeSubtypingCrossGroupMismatchLink || exactGCTypeSubtypingTransitiveMismatchLink || exactGCTypeSubtypingNonFlatExport || exactGCRefTestTable || exactGCRefTestConcrete || exactGCRefCastConcrete || exactGCRefTestAbstract || exactGCExtern || exactGCRefCastAbstract || exactGCBrOnCastAbstract || exactGCBrOnCastFailAbstract || exactGCBrOnCastConcrete || exactGCBrOnCastFailConcrete || exactGCBrOnCastNullability || exactGCBrOnCastFailNullability || exactGCStructMutation || exactGCStructNull || exactGCStructPacked || exactGCStructBasic || exactGCStructNumericGlobals || exactGCArrayLocal || exactGCArrayUniform || exactGCArrayFixedLocal || exactGCArrayFixedOfficial || exactGCArrayDefaultOfficial || exactGCArrayPackedData || exactGCArrayReference || exactGCArrayInitElem || exactGCArrayInitData || exactGCArrayFill || exactGCArrayCopy,
		bulkMemory:                          exactGCTypeSubtypingRefTestSingle || exactGCArrayPackedData || exactGCArrayReference || exactGCArrayInitElem || exactGCArrayInitData,
		typeSubtypingDeclarations:           exactGCTypeSubtypingDeclarations,
		typeSubtypingRecursiveFunctions:     exactGCTypeSubtypingRecursiveFunctions,
		typeSubtypingRefFuncGlobals:         exactGCTypeSubtypingRefFuncGlobals,
		typeSubtypingRuntimeCallCast:        exactGCTypeSubtypingRuntimeCallCast,
		typeSubtypingRefTestSingle:          exactGCTypeSubtypingRefTestSingle,
		typeSubtypingLink:                   exactGCTypeSubtypingLink,
		typeSubtypingFinalityLink:           exactGCTypeSubtypingFinalityLink,
		typeSubtypingStructLink:             exactGCTypeSubtypingStructLink,
		typeSubtypingStructProjectionLink:   exactGCTypeSubtypingStructProjectionLink,
		typeSubtypingStructMismatchLink:     exactGCTypeSubtypingStructMismatchLink,
		typeSubtypingIndependentStructLink:  exactGCTypeSubtypingIndependentStructLink,
		typeSubtypingExtendedProjectionLink: exactGCTypeSubtypingExtendedProjectionLink,
		typeSubtypingDuplicateRecursiveLink: exactGCTypeSubtypingDuplicateRecursiveLink,
		typeSubtypingExtendedRecursiveLink:  exactGCTypeSubtypingExtendedRecursiveLink,
		typeSubtypingCrossGroupMismatchLink: exactGCTypeSubtypingCrossGroupMismatchLink,
		typeSubtypingTransitiveMismatchLink: exactGCTypeSubtypingTransitiveMismatchLink,
		typeSubtypingNonFlatExport:          exactGCTypeSubtypingNonFlatExport,
		structNumericGlobals:                exactGCStructNumericGlobals,
		refTestAbstract:                     exactGCRefTestAbstract,
		extern:                              exactGCExtern,
		refCastAbstract:                     exactGCRefCastAbstract,
		brOnCastAbstract:                    exactGCBrOnCastAbstract,
		brOnCastFailAbstract:                exactGCBrOnCastFailAbstract,
		brOnCastNullability:                 exactGCBrOnCastNullability,
		brOnCastFailNullability:             exactGCBrOnCastFailNullability,
		structBasic:                         exactGCStructBasic,
		arrayFixedOfficial:                  exactGCArrayFixedOfficial,
		arrayDefaultOfficial:                exactGCArrayDefaultOfficial,
		arrayPackedData:                     exactGCArrayPackedData,
		arrayReference:                      exactGCArrayReference,
		arrayInitElem:                       exactGCArrayInitElem,
		arrayInitData:                       exactGCArrayInitData,
		arrayFill:                           exactGCArrayFill,
		arrayCopy:                           exactGCArrayCopy,
		structPacked:                        exactGCStructPacked,
	}, nil
}

func rv64LegacyGCHelperSlots(c *Compiled, params, results int) (int, int) {
	if rv64GCArrayInitElem(c) || rv64GCArrayInitData(c) {
		// The generated helper bridge stages array, destination, source, length,
		// type, and element indexes in the ordinary wrapper argument arena. This
		// internal six-slot call can exceed every exported function signature;
		// retain its complete capacity before allocating adjacent result/trap
		// buffers so helper staging cannot cross an arena boundary.
		params = max(params, 6)
	}
	if rv64GCStructNull(c) || rv64GCStructPacked(c) || rv64GCStructBasic(c) {
		// struct.set stages the reference, value, exact type, and field index.
		params = max(params, 4)
	}
	if rv64GCRefTestTable(c) || rv64GCRefTestConcrete(c) || rv64GCRefTestAbstract(c) || rv64GCBrOnCastAbstract(c) || rv64GCBrOnCastFailAbstract(c) || rv64GCBrOnCastConcrete(c) || rv64GCBrOnCastFailConcrete(c) || rv64GCBrOnCastNullability(c) || rv64GCBrOnCastFailNullability(c) {
		// ref.test stages the candidate word, signed heap type, nullability, and
		// exactness. Preserve all four cells even when every exported function is
		// parameterless so the helper cannot cross into results/trap storage.
		params = max(params, 4)
	}
	if rv64GCRefCastConcrete(c) {
		// Concrete ref.cast uses the same four arguments and returns one reference.
		params = max(params, 4)
		results = max(results, 1)
	}
	if rv64GCExtern(c) {
		// table.set stages the zero-normalized index, full-width reference, and
		// exact table index. Conversion helpers use one slot.
		params = max(params, 3)
	}
	if rv64GCRefCastAbstract(c) {
		// ref.cast stages the reference, heap code, nullability, and exactness.
		params = max(params, 4)
		results = max(results, 1)
	}
	if rv64GCBrOnCastAbstract(c) || rv64GCBrOnCastFailAbstract(c) || rv64GCBrOnCastConcrete(c) || rv64GCBrOnCastFailConcrete(c) || rv64GCBrOnCastNullability(c) || rv64GCBrOnCastFailNullability(c) {
		// Branch casts use the four-input ref.test bridge and one predicate result.
		params = max(params, 4)
		results = max(results, 1)
	}
	if rv64GCArrayFill(c) {
		// The packed fill bridge adds the exact type index after the array,
		// destination, value, and length operands.
		params = max(params, 5)
	}
	if rv64GCArrayCopy(c) {
		// The copy bridge carries both arrays, both offsets, length, and both
		// exact type indexes.
		params = max(params, 7)
	}
	return params, results
}

// These exact products pin every indirect signature to a type already sized
// by the local function directory, or own a table that cannot call indirectly.
func rv64LegacyGCTableUsesLocalSignatures(c *Compiled) bool {
	return rv64OfficialGCRefEq(c) || rv64GCRefTestTable(c) || rv64GCRefTestConcrete(c) || rv64GCRefCastConcrete(c) || rv64GCBrOnCastConcrete(c) || rv64GCBrOnCastFailConcrete(c) || rv64GCBrOnCastNullability(c) || rv64GCBrOnCastFailNullability(c) || rv64GCRefTestAbstract(c) || rv64GCExtern(c) || rv64GCRefCastAbstract(c) || rv64GCBrOnCastAbstract(c) || rv64GCBrOnCastFailAbstract(c) || rv64GCArrayInitElem(c) || rv64GCTypeSubtypingRuntimeCallCast(c) || rv64GCTypeSubtypingRuntimeFinalityCallCast(c) || rv64GCTypeSubtypingRuntimeTypedTableCall(c)
}

func rv64LegacyGCOwnedTable(c *Compiled) bool {
	return rv64LegacyI31ProductsForImage && (rv64I31OfficialTable(c) ||
		rv64I31OfficialTableGlobalInitializer(c) ||
		rv64I31OfficialAnyTable(c) ||
		rv64I31RefEqPilot(c)) ||
		rv64LegacyGCProductsForImage && (rv64OfficialGCRefEq(c) ||
			rv64GCArrayReference(c) ||
			rv64GCArrayInitElem(c) ||
			rv64GCTypeSubtypingRuntimeCallCast(c) ||
			rv64GCTypeSubtypingRuntimeFinalityCallCast(c) ||
			rv64GCTypeSubtypingRuntimeTypedTableCall(c))
}

func rv64LegacyGCReferenceTable(c *Compiled) bool {
	return rv64GCRefTestTable(c) ||
		rv64GCRefTestConcrete(c) ||
		rv64GCRefCastConcrete(c) ||
		rv64GCBrOnCastConcrete(c) ||
		rv64GCBrOnCastFailConcrete(c) ||
		rv64GCRefTestAbstract(c) ||
		rv64GCExtern(c) ||
		rv64GCRefCastAbstract(c) ||
		rv64GCBrOnCastAbstract(c) ||
		rv64GCBrOnCastFailAbstract(c)
}

func rv64LegacyGCReferenceLink(c *Compiled) bool {
	return rv64GCTypeSubtypingFinalityLink(c) ||
		rv64GCTypeSubtypingStructLink(c) ||
		rv64GCTypeSubtypingStructProjectionLink(c) ||
		rv64GCTypeSubtypingStructMismatchLink(c) ||
		rv64GCTypeSubtypingIndependentStructLink(c) ||
		rv64GCTypeSubtypingExtendedProjectionLink(c) ||
		rv64GCTypeSubtypingCrossGroupMismatchLink(c) ||
		rv64GCTypeSubtypingTransitiveMismatchLink(c)
}

func rv64LegacyGCImportSubtype(c, provider *Compiled) bool {
	return (rv64GCTypeSubtypingLink(c) && c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingLinkConsumer &&
		rv64GCTypeSubtypingLink(provider) && provider.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingLinkProvider) ||
		(rv64GCTypeSubtypingFinalityLink(c) && c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingFinalityLinkConsumer &&
			rv64GCTypeSubtypingFinalityLink(provider) && provider.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingFinalityLinkProvider) ||
		(rv64GCTypeSubtypingStructLink(c) && c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingStructLinkConsumer &&
			rv64GCTypeSubtypingStructLink(provider) && provider.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingStructLinkProvider) ||
		(rv64GCTypeSubtypingStructProjectionLink(c) && c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingStructProjectionLinkConsumer &&
			rv64GCTypeSubtypingStructProjectionLink(provider) && provider.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingStructProjectionLinkProvider) ||
		(rv64GCTypeSubtypingStructMismatchLink(c) && c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingStructMismatchLinkConsumer &&
			rv64GCTypeSubtypingStructMismatchLink(provider) && provider.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingStructMismatchLinkProvider) ||
		(rv64GCTypeSubtypingIndependentStructLink(c) && c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingIndependentStructLinkConsumer &&
			rv64GCTypeSubtypingIndependentStructLink(provider) && provider.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingIndependentStructLinkProvider) ||
		(rv64GCTypeSubtypingExtendedProjectionLink(c) && c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingExtendedProjectionLinkConsumer &&
			rv64GCTypeSubtypingExtendedProjectionLink(provider) && provider.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingExtendedProjectionLinkProvider) ||
		(rv64GCTypeSubtypingDuplicateRecursiveLink(c) && c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingDuplicateRecursiveLinkConsumer &&
			rv64GCTypeSubtypingDuplicateRecursiveLink(provider) && provider.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingDuplicateRecursiveLinkProvider) ||
		(rv64GCTypeSubtypingExtendedRecursiveLink(c) && c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingExtendedRecursiveLinkConsumer &&
			rv64GCTypeSubtypingExtendedRecursiveLink(provider) && provider.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingExtendedRecursiveLinkProvider) ||
		(rv64GCTypeSubtypingCrossGroupMismatchLink(c) && c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingCrossGroupMismatchLinkConsumer &&
			rv64GCTypeSubtypingCrossGroupMismatchLink(provider) && provider.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingCrossGroupMismatchLinkProvider) ||
		(rv64GCTypeSubtypingTransitiveMismatchLink(c) && c.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingTransitiveMismatchLinkConsumer &&
			rv64GCTypeSubtypingTransitiveMismatchLink(provider) && provider.stagedGCTypeSubtypingProduct() == stagedGCTypeSubtypingTransitiveMismatchLinkProvider)
}

func railshotLegacyGCImportIdentity(c *Compiled) bool {
	return rv64LegacyGCReferenceLink(c) ||
		rv64GCTypeSubtypingLink(c) ||
		rv64GCTypeSubtypingDuplicateRecursiveLink(c) ||
		rv64GCTypeSubtypingExtendedRecursiveLink(c)
}
