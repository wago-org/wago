// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && (amd64 || arm64) && ((windows && arm64) || tinygo)

package runtime

func (e *Engine) tryInlineBoundedHost(code uintptr, args []byte, linMem uintptr, trap, results, ctrl []byte, slots uint32, fixed FixedScalarHostCall) (bool, error) {
	return false, nil
}

func (e *Engine) tryInlineBoundedHostView(code uintptr, args []byte, linMem uintptr, trap, results, ctrl []byte, slots uint32, fixed FixedHostCallView) (bool, error) {
	return false, nil
}
