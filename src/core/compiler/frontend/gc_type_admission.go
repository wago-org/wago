// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package frontend

import (
	"fmt"

	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func (p supportPass) validateAggregateType(comp wasm.CompType, ctx string, group int) error {
	switch comp.Kind {
	case wasm.CompStruct:
		for fi := range comp.Fields {
			storage := comp.Fields[fi].Storage()
			if !storage.Packed() {
				if err := p.aggregateStorageType(storage.Val(), fmt.Sprintf("%s field %d", ctx, fi), group); err != nil {
					return err
				}
			}
		}
	case wasm.CompArray:
		storage := comp.Array.Storage()
		if !storage.Packed() {
			if err := p.aggregateStorageType(storage.Val(), ctx+" array element", group); err != nil {
				return err
			}
		}
	}
	if (p.feat.StructuralTypeProducts || p.feat.GCTypeSubtypingProducts) && (comp.Kind == wasm.CompStruct || comp.Kind == wasm.CompArray) {
		return nil
	}
	if p.feat.GCStructProducts && comp.Kind == wasm.CompStruct {
		return nil
	}
	if p.feat.GCArrayProducts && comp.Kind == wasm.CompArray {
		return nil
	}
	return p.unsupported("gc type", compTypeName(comp.Kind)+" (gc disabled)", ctx)
}

func (p supportPass) supportedGCReference(rt wasm.RefType) bool {
	if buildfeatures.GC == 0 {
		return false
	}
	heap := rt.Heap()
	if rt.Exact() || heap.Kind() != wasm.HeapAbs {
		return false
	}
	if p.feat.GCI31Products && (heap.Abs() == wasm.HeapI31 || heap.Abs() == wasm.HeapEq || heap.Abs() == wasm.HeapAny || heap.Abs() == wasm.HeapNone) {
		return true
	}
	return (p.feat.GCTypeSubtypingProducts || p.feat.GCStructProducts || p.feat.GCArrayProducts) && (heap.Abs() == wasm.HeapAny || heap.Abs() == wasm.HeapEq || heap.Abs() == wasm.HeapNone || heap.Abs() == wasm.HeapStruct || heap.Abs() == wasm.HeapArray)
}

func (p supportPass) supportedStructuralTypeRef(rt wasm.RefType) bool {
	if buildfeatures.GC == 0 {
		return false
	}
	heap := rt.Heap()
	if (!p.feat.StructuralTypeProducts && !p.feat.GCTypeSubtypingProducts && !p.feat.GCStructProducts && !p.feat.GCArrayProducts) || heap.Kind() != wasm.HeapTypeIndex {
		return false
	}
	index := heap.Type().Index
	for gi := range p.m.Types {
		if index < uint32(len(p.m.Types[gi].SubTypes)) {
			kind := p.m.Types[gi].SubTypes[index].Comp.Kind
			return kind == wasm.CompStruct || kind == wasm.CompArray
		}
		index -= uint32(len(p.m.Types[gi].SubTypes))
	}
	return false
}

func (p supportPass) supportedGCHeap(heap int64) bool {
	if buildfeatures.GC == 0 {
		return false
	}
	if heap < 0 {
		if p.feat.GCStructProducts && (heap == -21 || heap == -19 || heap == -18 || heap == -15) { // struct / eq / any / none
			return true
		}
		return p.feat.GCArrayProducts && (heap == -22 || heap == -19 || heap == -18 || heap == -15) // array / eq / any / none
	}
	if uint64(heap) > uint64(^uint32(0)) || p.m == nil {
		return false
	}
	index := uint32(heap)
	for _, group := range p.m.Types {
		if index < uint32(len(group.SubTypes)) {
			kind := group.SubTypes[index].Comp.Kind
			return (p.feat.GCStructProducts && kind == wasm.CompStruct) || (p.feat.GCArrayProducts && kind == wasm.CompArray)
		}
		index -= uint32(len(group.SubTypes))
	}
	return false
}

// aggregateStorageType admits exact indexed aggregate references only in GC
// storage. Exactness changes validation and casting, but not the collector's
// reference layout. Keep the exception local to aggregate fields/elements so
// signatures, globals, and tables retain their existing capability gates.
func (p supportPass) aggregateStorageType(v wasm.ValType, context string, group int) error {
	if v.Kind() == wasm.ValRef && p.supportedRecursiveAggregateStorageRef(v.Ref(), group) {
		return nil
	}
	if v.Kind() == wasm.ValRef && p.supportedExactAggregateStorageRef(v.Ref()) {
		return nil
	}
	return p.valType(v, context)
}

func (p supportPass) supportedRecursiveAggregateStorageRef(rt wasm.RefType, group int) bool {
	if !p.feat.ReferenceTypes || !p.feat.TypedFunctionReferences || rt.Exact() || p.m == nil || group < 0 || group >= len(p.m.Types) {
		return false
	}
	heap := rt.Heap()
	if heap.Kind() != wasm.HeapTypeIndex || !heap.Type().Rec || int(heap.Type().Index) >= len(p.m.Types[group].SubTypes) {
		return false
	}
	kind := p.m.Types[group].SubTypes[heap.Type().Index].Comp.Kind
	return kind == wasm.CompStruct && (p.feat.StructuralTypeProducts || p.feat.GCTypeSubtypingProducts || p.feat.GCStructProducts) ||
		kind == wasm.CompArray && (p.feat.StructuralTypeProducts || p.feat.GCTypeSubtypingProducts || p.feat.GCArrayProducts)
}

func (p supportPass) supportedExactAggregateStorageRef(rt wasm.RefType) bool {
	if !p.feat.ReferenceTypes || !p.feat.TypedFunctionReferences || !rt.Exact() {
		return false
	}
	heap := rt.Heap()
	if heap.Kind() != wasm.HeapTypeIndex || heap.Type().Rec || p.m == nil {
		return false
	}
	index := heap.Type().Index
	for gi := range p.m.Types {
		if index < uint32(len(p.m.Types[gi].SubTypes)) {
			kind := p.m.Types[gi].SubTypes[index].Comp.Kind
			return kind == wasm.CompStruct && (p.feat.StructuralTypeProducts || p.feat.GCTypeSubtypingProducts || p.feat.GCStructProducts) ||
				kind == wasm.CompArray && (p.feat.StructuralTypeProducts || p.feat.GCTypeSubtypingProducts || p.feat.GCArrayProducts)
		}
		index -= uint32(len(p.m.Types[gi].SubTypes))
	}
	return false
}
