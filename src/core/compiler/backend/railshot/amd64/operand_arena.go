// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// operandArena owns stable RW storage for operand nodes. Nodes contain only
// scalar storage and links to nodes in worker-owned mapped or heap chunks;
// plugin-owned Go values remain in stack.cold, which is scanned normally.
type operandArena = coreruntime.CodeBuffer

func operandArenaChunk(a *operandArena, capacity int) []elem {
	if a != nil {
		bytes := capacity * int(unsafe.Sizeof(elem{}))
		// Never grow this mapping: live expression links require stable addresses.
		if bytes <= len(a.Mapping())-len(a.Bytes()) {
			storage, err := a.AppendSpace(bytes)
			if err == nil {
				return unsafe.Slice((*elem)(unsafe.Pointer(unsafe.SliceData(storage))), capacity)[:0:capacity]
			}
		}
	}
	return make([]elem, 0, capacity)
}

// closeOperandArenaOwner captures the stable stack owner before worker cleanup.
// An independent defer guarantees an attempted close even if other cleanup fails.
func closeOperandArenaOwner(owner *stack) {
	if owner != nil {
		_ = owner.arena.Close()
	}
}
