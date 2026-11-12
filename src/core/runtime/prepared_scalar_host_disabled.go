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

func preparedScalarHostAvailable() bool     { return false }
func windowsTypedI64HostAvailable() bool    { return false }
func preparedScalarHostBridge() uintptr     { return 0 }
func preparedScalarHostViewBridge() uintptr { return 0 }

func (p *PreparedScalarHost) Call(code, linMem uintptr, context unsafe.Pointer, fixed FixedScalarHostContextCall) error {
	return fmt.Errorf("jit: prepared scalar host bridge unavailable")
}

func (p *PreparedScalarHost) CallView(code, linMem uintptr, context unsafe.Pointer, fixed FixedHostContextCallView) error {
	return fmt.Errorf("jit: prepared scalar host view unavailable")
}
