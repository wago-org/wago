// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"runtime"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// operandArena owns stable RW storage for operand nodes. Nodes contain only
// scalar storage and links to other nodes in the same worker-owned arena;
// plugin-owned Go values remain in stack.cold, which is scanned normally.
type operandArena = coreruntime.CodeBuffer

func shouldMapOperands(bodyBytes int) bool {
	// Mapping avoids repeatedly tracing large pointer-bearing chunks in TinyGo.
	return bodyBytes >= minParallelHintBodyBytes || runtime.Compiler == "tinygo" && bodyBytes >= 8<<10
}

func closeOperandArena(arena *operandArena) {
	if err := arena.Close(); err != nil && runtime.Compiler == "tinygo" {
		// Scratch normally closes before leaving its worker. Keep the fallback
		// for a failed unmap without starting TinyGo's finalizer task on success.
		runtime.SetFinalizer(arena, finalizeOperandArena)
	}
}

func finalizeOperandArena(arena *operandArena) { _ = arena.Close() }

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
