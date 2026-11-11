// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// LoopExpression owns the semantics of one expression in a bounded candidate.
// Children index that candidate's expression pool, never allocator descriptors.
// Type and constant bits retain source arithmetic; memory immediates belong in
// separate ordered access records. No expression survives candidate emission.
type LoopExpression struct {
	Bits        uint64
	Left, Right int
	PC          uint32
	Op          byte
	Type        wasm.ValType
}
