// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// readV128ConstExpr reads the vector initializer that follows a SIMD prefix.
func readV128ConstExpr(r *wasm.Reader) ([]byte, error) {
	sub, err := r.U32()
	if err != nil {
		return nil, err
	}
	if sub != 12 {
		return nil, fmt.Errorf("unsupported SIMD constant expression opcode 0xfd %d", sub)
	}
	return r.Bytes(16)
}
