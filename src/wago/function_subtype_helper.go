// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

const gcFuncRefTest = 14
const functionSubtypeDispatchIndex = gcStructDispatchBit | gcFuncRefTest
const functionSubtypeHelperForImage = referenceSignaturesBuilt && gcHelperMaskForImage&(1<<gcFuncRefTest) != 0

// entryOwner owns the physical entry lease when a native callee parks on a
// different instance control block. No unretained descriptor is dereferenced.
func (in *Instance) dispatchGCFunctionRefTest(entryOwner *Instance, args, results []uint64) {
	if len(args) != 3 || len(results) < 1 {
		panic(gcHelperFailuref("function ref.test helper arity = %d/%d, want 3/at-least-1", len(args), len(results)))
	}
	bits, targetType, nullable := args[0], uint32(args[1]), args[2] != 0
	if int(targetType) >= len(in.c.Types) || in.c.Types[targetType].Kind != CompositeTypeFunction {
		panic(gcHelperFailuref("function ref.test target type %d is unavailable", targetType))
	}
	if bits == 0 {
		if nullable {
			results[0] = 1
		} else {
			results[0] = 0
		}
		return
	}
	actual, actualTypes, ok := in.attachedFuncrefExactType(bits)
	if !ok {
		actual, actualTypes, ok = railshotRetainedFunctionExactType(in, bits)
	}
	if !ok && entryOwner != nil && entryOwner != in {
		actual, actualTypes, ok = railshotRetainedFunctionExactType(entryOwner, bits)
	}
	if !ok && in.refStore != nil {
		actual, actualTypes, ok = in.refStore.descriptorFuncrefExactType(in, bits)
	}
	want := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: nullable, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: targetType}}}
	if ok && valueTypeSubtype(actual, actualTypes, want, in.c.Types) {
		results[0] = 1
	} else {
		results[0] = 0
	}
}
