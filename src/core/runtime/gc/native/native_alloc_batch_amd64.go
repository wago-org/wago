// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package gc

import "os"

const nativeStructHandleBatch = 512

var nativeStructHandleBatchSize = nativeStructHandleBatch

func init() {
	if nativeCollectorFastPaths {
		switch os.Getenv("WAGO_AMD64_GC_STRUCT_BATCH") {
		case "32":
			nativeStructHandleBatchSize = 32
		case "64":
			nativeStructHandleBatchSize = 64
		case "128":
			nativeStructHandleBatchSize = 128
		case "256":
			nativeStructHandleBatchSize = 256
		}
	}
}
