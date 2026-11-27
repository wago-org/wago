// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

// These products describe the exact lowering selected by the Wasm GC compiler.
type compiledGCProducts struct {
	gcTypeSubtypingProduct stagedGCTypeSubtypingProduct
	gcStructProduct        stagedGCStructProduct
	gcArrayProduct         stagedGCArrayProduct
	gcI31Product           stagedGCI31Product
}
