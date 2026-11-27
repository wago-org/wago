// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !linux || !riscv64 || tinygo || wago_target_tinygo || (wago_no_reference_types && wago_no_gc && wago_no_exception_handling)

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

func (*Compiled) functionEnvironmentFrameRoots() bool { return false }
func (*Compiled) usesManagedFunctionStorage() bool    { return false }

func compiledManagedFunctionReachability(*Compiled) bool                        { return false }
func collectManagedFunctionGraph(*Instance, *gcPublicState, bool) (bool, error) { return false, nil }

func managedFunctionOwnerRootsStrong(*Instance, *gc.Collector) bool { return true }

func visitManagedFunctionFrameRoot(_ *Instance, descriptor uint64, _ func(gc.RootSlot) bool, _ gc.RootRefSink) bool {
	if descriptor != 0 {
		panic(gcHelperFailuref("function frame roots require the managed owner runtime"))
	}
	return true
}
func visitManagedFunctionOwnerFrameRoot(*Instance, *gc.Collector, func(gc.RootSlot) bool, gc.RootRefSink) bool {
	return true
}

func (*Instance) managedFunctionAggregateMayAllocate(uint32, []uint64) bool { return false }
func (*Instance) dispatchManagedFunctionAggregate(uint32, []uint64, []uint64, gc.RootSet, *gcPublicState) bool {
	return false
}
