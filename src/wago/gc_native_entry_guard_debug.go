// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wagodebug

package wago

import (
	"fmt"
	"unsafe"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const nativeGCEntryValidationEnabled = true

// validateNativeGCEntry retains a coarse hardened assertion at the untrusted
// Go-to-native boundary without charging every production GC access. It checks
// only immutable ABI/view facts; mutable collector backing remains dynamically
// validated by generated semantic access code.
func validateNativeGCEntry(in *Instance) error {
	if in == nil || in.gcNativeView == nil {
		return nil
	}
	if in.jm == nil || in.jm.GCNativeViewPtr() != uintptr(unsafe.Pointer(in.gcNativeView)) {
		return fmt.Errorf("wagodebug: native GC basedata view pointer mismatch")
	}
	if err := gc.ValidateNativeInstanceView(in.gcNativeView, in.gc, uint32(len(in.c.Types))); err != nil {
		return fmt.Errorf("wagodebug: native GC entry: %w", err)
	}
	return nil
}
