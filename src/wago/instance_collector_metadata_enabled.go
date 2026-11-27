// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

// Keep the Go-owned native view and its local type identity alive with the instance.
type instanceCollectorMetadata struct {
	typeMap    *gcTypeMapping
	nativeView *gc.NativeInstanceView
}

func newInstanceCollectorMetadata(typeMap *gcTypeMapping, nativeView *gc.NativeInstanceView) instanceCollectorMetadata {
	return instanceCollectorMetadata{typeMap: typeMap, nativeView: nativeView}
}

func (m *instanceCollectorMetadata) gcTypeMap() *gcTypeMapping { return m.typeMap }

func (m *instanceCollectorMetadata) gcNativeView() *gc.NativeInstanceView { return m.nativeView }

func (in *Instance) hasNativeCollector() bool { return in != nil && in.gc != nil }

// Called before external access acquires collector or native-context locks.
// Internal GC helpers retain the fast gate and must not revoke their own entry.
func (in *Instance) revokePrivateGCEntry() {
	if in.hasNativeCollector() {
		if state := in.pluginState.Load(); state != nil {
			state.invokeMu.revokeFast()
		}
	}
}
