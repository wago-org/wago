// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wagodebug && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && (!wago_no_reference_types || !wago_no_exception_handling)))

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
	if in == nil {
		return nil
	}
	view := in.gcNativeView()
	if view == nil {
		return nil
	}
	if in.jm == nil || in.jm.GCNativeViewPtr() != uintptr(unsafe.Pointer(view)) {
		return fmt.Errorf("wagodebug: native GC basedata view pointer mismatch")
	}
	if err := gc.ValidateNativeInstanceView(view, in.gc, uint32(len(in.c.Types))); err != nil {
		return fmt.Errorf("wagodebug: native GC entry: %w", err)
	}
	return nil
}
