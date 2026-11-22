// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build tinygo && ((linux && (amd64 || arm64)) || (darwin && arm64))

package runtime

import (
	"reflect"
	"unsafe"
)

func tinygoScalarLeafEntry(fn any, params uint32) uintptr {
	var value tinygoScalarLeafFuncValue
	switch typed := fn.(type) {
	case func(int32) int32:
		if params != 1 {
			return 0
		}
		value = *(*tinygoScalarLeafFuncValue)(unsafe.Pointer(&typed))
	case func(int32, int32) int32:
		if params != 2 {
			return 0
		}
		value = *(*tinygoScalarLeafFuncValue)(unsafe.Pointer(&typed))
	default:
		return 0
	}
	// Check before consulting the shared cache. Its key omits closure state.
	if value.context != 0 {
		return 0
	}
	return value.fnptr
}

func scalarLeafCanRegister(fn any, params uint32) bool {
	entry := tinygoScalarLeafEntry(fn, params)
	// The shared cache keys on reflect.Value.Pointer. Require it to name
	// the same address checked by the TinyGo function-value decoder.
	return entry != 0 && reflect.ValueOf(fn).Pointer() == entry
}
