// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

// operandArena supplies stable storage for pointer-free operand nodes. The
// plugin values that can carry Go pointers still live in the Go-owned stack sidecar.
type operandArena struct {
	mapping []byte
	used    int
}

func operandArenaChunk(arena *operandArena, capacity int) []elem {
	if arena != nil {
		const nodeBytes = int(unsafe.Sizeof(elem{}))
		if capacity <= (len(arena.mapping)-arena.used)/nodeBytes {
			start := arena.used
			arena.used += capacity * nodeBytes
			nodes := unsafe.Slice((*elem)(unsafe.Pointer(&arena.mapping[start])), capacity)
			return nodes[:0:capacity]
		}
	}
	return make([]elem, 0, capacity)
}

const nativeTrapSiteCapacity = shared.MaxRetainedStackArenaBytes / int(unsafe.Sizeof(trapSite{}))

func (arena *operandArena) trapSiteStorage() []trapSite {
	const recordBytes = int(unsafe.Sizeof(trapSite{}))
	return unsafe.Slice((*trapSite)(unsafe.Pointer(unsafe.SliceData(arena.mapping))), len(arena.mapping)/recordBytes)
}
