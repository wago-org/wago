// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// A successful ordinary allocation returns a non-null reference whose dynamic
// type is the allocated absolute type. An immediately following ordinary cast
// to a proved supertype therefore preserves the value and cannot trap. Decode
// on a copy so every rejected or malformed candidate leaves the source untouched.
func consumeProvedGCAllocationCast(m *wasm.Module, r *wasm.Reader, typeIndex uint32) bool {
	next := *r
	op, err := next.Byte()
	if err != nil || op != 0xfb {
		return false
	}
	sub, err := next.U32()
	if err != nil || sub != 22 { // non-nullable ref.cast only
		return false
	}
	target, exact, err := readRefHeapTypeImmediate(&next)
	if err != nil || exact || target < 0 || uint64(target) > uint64(^uint32(0)) {
		return false
	}
	actual := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false)
	required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: uint32(target)}), false)
	if !m.ReferenceTypeSubtype(actual, required) {
		return false
	}
	*r = next
	return true
}

func (f *fn) skipProvedGCAllocationCast(r *wasm.Reader, typeIndex uint32) {
	if !consumeProvedGCAllocationCast(f.m, r, typeIndex) {
		return
	}
	// Match the state transitions that emitFB would perform for subopcode 22.
	f.prepareGCResolvedFB(22)
	f.prepareGCFreshStructFB(22)
	f.stats.peep("gc-allocation-cast-elide")
}

// A successful direct call with one non-null indexed reference result already
// proves the result's declared type. An adjacent ordinary cast to that type or
// a supertype therefore preserves the value and cannot trap. Recursive-local
// result indexes need their owning recursion group, so reject them here.
func consumeProvedGCReferenceCast(m *wasm.Module, r *wasm.Reader, actual wasm.RefType) bool {
	if m == nil {
		return false
	}
	heap := actual.Heap()
	if actual.Nullable() || heap.Kind() != wasm.HeapTypeIndex || heap.Type().Rec {
		return false
	}
	next := *r
	op, err := next.Byte()
	if err != nil || op != 0xfb {
		return false
	}
	sub, err := next.U32()
	if err != nil || sub != 22 { // non-nullable ref.cast only
		return false
	}
	target, exact, err := readRefHeapTypeImmediate(&next)
	if err != nil || exact || target < 0 || uint64(target) > uint64(^uint32(0)) {
		return false
	}
	required := wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: uint32(target)}), false)
	if !m.ReferenceTypeSubtype(actual, required) {
		return false
	}
	*r = next
	return true
}

func consumeProvedGCCallResultCast(m *wasm.Module, r *wasm.Reader, ft *wasm.CompType) bool {
	return ft != nil && len(ft.Results) == 1 && ft.Results[0].Kind() == wasm.ValRef &&
		consumeProvedGCReferenceCast(m, r, ft.Results[0].Ref())
}

func (f *fn) finishProvedGCCallResultCast(r *wasm.Reader, ft *wasm.CompType, err error) error {
	if err != nil {
		return err
	}
	if ft != nil && len(ft.Results) == 1 {
		f.setPendingGCReference(ft.Results[0])
	}
	if !consumeProvedGCCallResultCast(f.m, r, ft) {
		return nil
	}
	// Match the state transitions that emitFB would perform for subopcode 22.
	f.prepareGCResolvedFB(22)
	f.prepareGCFreshStructFB(22)
	f.stats.peep("gc-call-result-cast-elide")
	return nil
}

func (f *fn) skipProvedGCFieldResultCast(r *wasm.Reader, result wasm.ValType) {
	if result.Kind() != wasm.ValRef || !consumeProvedGCReferenceCast(f.m, r, result.Ref()) {
		return
	}
	// Match the state transitions that emitFB would perform for subopcode 22.
	f.prepareGCResolvedFB(22)
	f.prepareGCFreshStructFB(22)
	f.stats.peep("gc-field-result-cast-elide")
}

// A successful collector cast preserves the reference and proves the same
// adjacent cast cannot fail. Keep the first check and expose any following
// access to the ordinary cast/load fusion. No source call or root mask is lost.
func (f *fn) skipIdenticalGCCasts(r *wasm.Reader, sub uint32, heap int64, exact bool) {
	for {
		if op, ok := r.Peek(); !ok || op != 0xfb {
			return
		}
		next := *r
		_, _ = next.Byte()
		other, err := next.U32()
		if err != nil || other != sub {
			return
		}
		target, targetExact, err := readRefHeapTypeImmediate(&next)
		if err != nil || target != heap || targetExact != exact {
			return
		}
		*r = next
		f.stats.peep("gc-identical-cast-elide")
	}
}
