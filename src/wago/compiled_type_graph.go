// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

func hasAggregateTypeDeclarations(types []DefinedTypeDescriptor) bool {
	for _, typ := range types {
		if typ.Kind == CompositeTypeStruct || typ.Kind == CompositeTypeArray {
			return true
		}
	}
	return false
}

// Function-only type graphs require no collector-backed storage. This checks
// their representation; native code and reference binding checks remain separate.
func compiledFunctionOnlyTypeGraph(c *Compiled) bool {
	if c == nil || len(c.Types) == 0 || len(c.GCTypeDescs) != len(c.Types) {
		return false
	}
	for index, typ := range c.Types {
		if typ.Kind != CompositeTypeFunction || typ.HasDescribes || typ.HasDescriptor || c.GCTypeDescs[index].Kind != gc.KindFunc {
			return false
		}
		for _, values := range [][]ValueTypeDescriptor{typ.Params, typ.Results} {
			for _, value := range values {
				if value.Kind <= ValueTypeV128 {
					if value.Ref != (ReferenceTypeDescriptor{}) {
						return false
					}
					continue
				}
				if !referenceTypeDescriptorsForImage || value.Kind != ValueTypeReference || value.Ref.Exact {
					return false
				}
				heap := value.Ref.Heap
				if heap.Defined {
					if heap.Abstract != 0 || uint64(heap.TypeIndex) >= uint64(len(c.Types)) || c.Types[heap.TypeIndex].Kind != CompositeTypeFunction {
						return false
					}
				} else if heap.TypeIndex != 0 || heap.Abstract != AbstractHeapFunc && heap.Abstract != AbstractHeapNoFunc && heap.Abstract != AbstractHeapExtern && heap.Abstract != AbstractHeapNoExtern {
					return false
				}
			}
		}
	}
	return true
}

//lint:ignore U1000 Used by RISC-V admission and the standalone generator.
func compiledUsesTypedSignatures(c *Compiled) bool {
	for _, typ := range c.Types {
		if !typ.Final || len(typ.Supers) != 0 {
			return true
		}
		for _, values := range [][]ValueTypeDescriptor{typ.Params, typ.Results} {
			for _, value := range values {
				if value.Kind == ValueTypeReference && (!value.Ref.Nullable || value.Ref.Heap.Defined || value.Ref.Heap.Abstract != AbstractHeapFunc && value.Ref.Heap.Abstract != AbstractHeapExtern) {
					return true
				}
			}
		}
	}
	for index := 1; index < len(c.Types); index++ {
		if c.Types[index].RecGroup == c.Types[index-1].RecGroup {
			return true
		}
	}
	return false
}
