// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_tail_call

package wago

import "context"

type rv64NativeTailInvocationStorage rv64NativeTailInvocation

func (s *rv64NativeTailInvocationStorage) get() *rv64NativeTailInvocation {
	return (*rv64NativeTailInvocation)(s)
}

// Select the scope while native serialization still protects the active tail
// generation. The parked entry owns that generation until the callback resumes.
func railshotRetainedCallbackHostScope(in *Instance, ctrl uintptr, state *instancePluginState, parent context.Context) *hostCallScope {
	if in == nil || state == nil || ctrl == 0 || ctrl == offHeapSlicePtr(in.ctrl) {
		return nil
	}
	generation := state.railshotInvocation.tail.get().activeGeneration
	if generation == nil || generation.caller != in {
		return nil
	}
	scope := &generation.hostScope
	scope.parentMu.Lock()
	if scope.active.Load() == 0 {
		scope.parent = parent
	}
	scope.parentMu.Unlock()
	return scope
}
