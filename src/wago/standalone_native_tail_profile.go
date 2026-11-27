// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

//lint:ignore U1000 Used by RISC-V admission and the standalone generator.
type standaloneNativeTailProfile struct {
	directImports           bool
	indirect                bool
	reference               bool
	legacyReferenceCarriers bool
}
