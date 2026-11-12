// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && (amd64 || arm64) && ((windows && arm64) || tinygo)

package runtime

import (
	"fmt"
	"unsafe"
)

func (p *PreparedScalarHost) CallInteger(code, linMem uintptr, context unsafe.Pointer, fixed FixedScalarHostContextCall) error {
	return fmt.Errorf("jit: prepared scalar host bridge unavailable")
}

func (p *PreparedScalarHost) CallIntegerView(code, linMem uintptr, context unsafe.Pointer, fixed FixedHostContextCallView) error {
	return fmt.Errorf("jit: prepared scalar host view unavailable")
}

func (p *PreparedScalarHost) CallIntegerI32(code, linMem uintptr, fixed func(int32) int32) error {
	return fmt.Errorf("jit: prepared integer i32 host bridge unavailable")
}
