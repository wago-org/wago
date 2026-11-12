// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package wago

import (
	"os"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Only AMD64 currently publishes live structs from a reserved native batch.
const nativeLiveStructAllocSupported = true

var nativeStructAllocEnabled = os.Getenv("WAGO_AMD64_NO_GC_NATIVE_ALLOC") != "1"

func (in *Instance) prepareNativeArrayAllocation(typeID, length uint32) {
	in.prepareNativeArrayAllocationMode(typeID, length, false)
}

func (in *Instance) prepareNativeDefaultArrayAllocation(typeID, length uint32) {
	in.prepareNativeArrayAllocationMode(typeID, length, true)
}

func (in *Instance) prepareNativeArrayAllocationMode(typeID, length uint32, typedDefault bool) {
	if !nativeStructAllocEnabled || in == nil || in.gc == nil || in.c == nil || int(typeID) >= len(in.c.Types) || int(typeID) >= len(in.c.GCTypeDescs) {
		return
	}
	t := in.c.Types[typeID]
	d := in.c.GCTypeDescs[typeID]
	if !t.Final || t.Kind != CompositeTypeArray || d.Kind != gc.KindArray {
		return
	}
	if t.Array.Storage.Value.Kind == ValueTypeReference {
		heap := t.Array.Storage.Value.Ref.Heap
		if (d.Elem != gc.StorageRef && d.Elem != gc.StorageRefNull) || (!typedDefault && (heap.Defined || (heap.Abstract != AbstractHeapAny && heap.Abstract != AbstractHeapEq))) {
			return
		}
	}
	size, err := gc.ArraySize(d, length)
	if err != nil {
		return
	}
	if in.c.genericGCBoundaryCollectionSafe() {
		in.gc.PrepareNativeArrayAllocation(size)
		return
	}
	in.gc.PrepareNativeArrayAllocationImmediate(size)
}
