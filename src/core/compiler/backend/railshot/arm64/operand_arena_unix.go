// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64 && (darwin || linux)

package arm64

import (
	"runtime"
	"syscall"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

// Match the AMD64 amortization threshold. This is data scratch, so Darwin
// needs an anonymous read-write mapping without MAP_JIT.
func shouldMapOperands(bodyBytes int) bool {
	if runtime.GOOS == "linux" {
		return bodyBytes >= 16<<10
	}
	return bodyBytes >= 8<<10
}

func newOperandArena() *operandArena {
	mapping, err := syscall.Mmap(-1, 0, shared.MaxRetainedStackArenaBytes,
		syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return nil
	}
	return &operandArena{mapping: mapping}
}

func (arena *operandArena) close() {
	if arena == nil || len(arena.mapping) == 0 {
		return
	}
	if err := syscall.Munmap(arena.mapping); err != nil {
		runtime.SetFinalizer(arena, finalizeOperandArena)
		return
	}
	arena.mapping = nil
}

func finalizeOperandArena(arena *operandArena) { _ = syscall.Munmap(arena.mapping) }
