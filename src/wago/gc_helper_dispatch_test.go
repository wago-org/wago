// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import "testing"

func TestInternalGCHelperKeepsNativeExecutionLease(t *testing.T) {
	in := &Instance{ctrl: make([]byte, 64)}
	ctrl := offHeapSlicePtr(in.ctrl)
	var helperObservedUnlocked bool
	in.hostCall = func(uintptr, uint32, []uint64, []uint64, hostInvocationContext) {
		if nativeExecutionMu.TryLock() {
			helperObservedUnlocked = true
			nativeExecutionMu.Unlock()
		}
	}

	nativeExecutionMu.Lock()
	in.dispatchSynchronousHostCall(ctrl, gcStructDispatchBit|gcStructGet, nil, nil)
	nativeExecutionMu.Unlock()
	if helperObservedUnlocked {
		t.Fatal("internal GC helper released the native execution lease")
	}
}

func TestOrdinaryHostCallReleasesNativeExecutionLease(t *testing.T) {
	in := &Instance{ctrl: make([]byte, 64)}
	ctrl := offHeapSlicePtr(in.ctrl)
	var hostObservedUnlocked bool
	in.hostCall = func(uintptr, uint32, []uint64, []uint64, hostInvocationContext) {
		if nativeExecutionMu.TryLock() {
			hostObservedUnlocked = true
			nativeExecutionMu.Unlock()
		}
	}

	nativeExecutionMu.Lock()
	in.dispatchSynchronousHostCall(ctrl, 0, nil, nil)
	nativeExecutionMu.Unlock()
	if !hostObservedUnlocked {
		t.Fatal("ordinary host call retained the native execution lease")
	}
}
