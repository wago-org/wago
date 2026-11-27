// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Keep plain reference initializers available to the compact externref
// runtime without pulling in the aggregate stack parser and its type index.
type rootedScalarReferenceInitializer struct {
	m           *wasm.Module
	valid       bool
	entry, exit []uint32
}

func newRootedScalarReferenceInitializer(m *wasm.Module) *rootedScalarReferenceInitializer {
	if buildfeatures.GC != 0 {
		return newRootedScalarAggregateParser(m)
	}
	p := &rootedScalarReferenceInitializer{m: m}
	if rootedScalarFlatTypeCount(m) <= 0 {
		return p
	}
	var index uint32
	for _, group := range m.Types {
		for _, sub := range group.SubTypes {
			if len(sub.Supers) > 1 || len(sub.Supers) == 1 && (sub.Supers[0].Rec || sub.Supers[0].Index >= index) {
				return p
			}
			index++
		}
	}
	p.valid = true
	return p
}

func (p *rootedScalarReferenceInitializer) validateExpression(expr wasm.Expr, required wasm.RefType, globalLimit int) bool {
	if buildfeatures.GC != 0 {
		return p.validateAggregateExpression(expr, required, globalLimit)
	}
	if p == nil || !p.valid {
		return false
	}
	m := p.m
	// Without GC, the only instructions that can produce a reference are
	// these three leaf forms. An expression returning one value has one leaf.
	if globalLimit < 0 || globalLimit > m.ImportedGlobalCount()+len(m.Globals) ||
		len(expr.Instrs) != 0 || len(expr.BodyBytes) < 3 || len(expr.BodyBytes) > 7 {
		return false
	}
	r := wasm.ReaderFrom(expr.BodyBytes[1:])
	var actual wasm.RefType
	switch expr.BodyBytes[0] {
	case 0x23:
		return admitGlobalExpression(expr, m, wasm.RefVal(required), globalLimit) == nil
	case 0xd0:
		heap, err := r.S33()
		if err != nil {
			return false
		}
		if heap >= 0 {
			actual = wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: uint32(heap)}), false)
		} else {
			actual = wasm.Ref(true, wasm.AbsHeap(wasm.AbsHeapType(byte(heap&0x7f))), false)
		}
	case 0xd2:
		index, err := r.U32()
		if err != nil || index < uint32(m.ImportedFuncCount()) {
			return false
		}
		typeIndex, ok := m.FuncTypeIndex(index)
		if !ok || typeIndex.Rec {
			return false
		}
		actual = wasm.Ref(false, wasm.IndexedHeap(typeIndex), false)
	default:
		return false
	}
	end, err := r.Byte()
	return err == nil && end == 0x0b && !r.HasNext() && m.ReferenceTypeSubtype(actual, required)
}

func rootedScalarAggregateElementExpression(expr wasm.Expr, ref wasm.RefType, m *wasm.Module) bool {
	return newRootedScalarReferenceInitializer(m).validateExpression(expr, ref, m.ImportedGlobalCount()+len(m.Globals))
}

func rootedScalarReferenceGlobalInitializer(global wasm.Global, p *rootedScalarReferenceInitializer, globalLimit int) bool {
	if global.Type.Type.Kind() != wasm.ValRef {
		return false
	}
	ref := global.Type.Type.Ref()
	heap := ref.Heap()
	if ref.Exact() {
		return false
	}
	switch heap.Kind() {
	case wasm.HeapAbs:
		if !ref.Nullable() && heap.Abs() != wasm.HeapI31 {
			return false
		}
		switch heap.Abs() {
		case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapExtern, wasm.HeapNoExtern:
			return p.validateExpression(global.Init, ref, globalLimit)
		default:
			return false
		}
	case wasm.HeapTypeIndex:
		return !heap.Type().Rec && rootedScalarAggregateAt(p.m, heap.Type().Index) && p.validateExpression(global.Init, ref, globalLimit)
	default:
		return false
	}
}
