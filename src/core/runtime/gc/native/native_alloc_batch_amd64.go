// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package gc

import "os"

const nativeStructHandleBatch = 512

var nativeStructHandleBatchSize = func() int {
	switch os.Getenv("WAGO_AMD64_GC_STRUCT_BATCH") {
	case "32":
		return 32
	case "64":
		return 64
	case "128":
		return 128
	case "256":
		return 256
	default:
		return nativeStructHandleBatch
	}
}()
