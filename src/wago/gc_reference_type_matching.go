// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

func (in *Instance) gcRefMatchesValueType(ref gc.Ref, required ValueTypeDescriptor) bool {
	if !collectorObjectTokensBuilt || in == nil || required.Kind != ValueTypeReference {
		return false
	}
	if !wasmGCBuilt && (required.Ref.Heap.Defined || required.Ref.Heap.Abstract != AbstractHeapExn && required.Ref.Heap.Abstract != AbstractHeapNoExn) {
		return false
	}
	if ref.IsNull() {
		return required.Ref.Nullable
	}
	if ref.IsI31() {
		if !wasmGCBuilt || required.Ref.Heap.Defined {
			return false
		}
		switch required.Ref.Heap.Abstract {
		case AbstractHeapAny, AbstractHeapEq, AbstractHeapI31:
			return true
		default:
			return false
		}
	}
	if in.gc == nil {
		return false
	}
	if matched, handled := ownedExceptionReferenceMatches(in.gc, ref, required); handled {
		return matched
	}
	return wasmGCBuilt && in.gcHeapRefMatchesValueType(ref, required)
}

func (in *Instance) gcHeapRefMatchesValueType(ref gc.Ref, required ValueTypeDescriptor) bool {
	if matched, handled := compactForeignAnyMatches(in.gc, ref, required); handled {
		return matched
	}
	var target gc.RefTestTarget
	target.Nullable = required.Ref.Nullable
	if required.Ref.Heap.Defined {
		domain, ok := in.gcDomainType(required.Ref.Heap.TypeIndex)
		if !ok {
			return false
		}
		target.Kind, target.Type, target.Exact = gc.RefTestDefined, domain, required.Ref.Exact
	} else {
		switch required.Ref.Heap.Abstract {
		case AbstractHeapAny:
			target.Kind = gc.RefTestAny
		case AbstractHeapEq:
			target.Kind = gc.RefTestEq
		case AbstractHeapI31:
			target.Kind = gc.RefTestI31
		case AbstractHeapStruct:
			target.Kind = gc.RefTestStruct
		case AbstractHeapArray:
			target.Kind = gc.RefTestArray
		case AbstractHeapNone:
			target.Kind = gc.RefTestNone
		default:
			return false
		}
	}
	matched, err := in.gc.RefTest(ref, target)
	return err == nil && matched
}
