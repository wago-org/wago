// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !tinygo

package railshot

import "runtime"

func compilerPanicStack() []byte {
	// A fixed bound keeps malformed-input diagnostics from growing without limit.
	stack := make([]byte, 16<<10)
	return stack[:runtime.Stack(stack, false)]
}
