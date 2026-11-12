// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package shared

// LoopStore records one source-ordered effect in a bounded loop candidate.
// Address identifies a modular dynamic address expression; Offset is the
// separately widened memory immediate. Combining them before the dynamic wrap
// would change Wasm addressing. Value and Address are owned expression indices.
type LoopStore[Node ~uint8 | ~int] struct {
	Offset         uint64
	PC             uint32
	Address, Value Node
}
