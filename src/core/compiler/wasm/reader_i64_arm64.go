// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package wasm

func (r *Reader) I64() (int64, error) {
	if r.pos < len(r.data) {
		if b := r.data[r.pos]; b < 0x80 {
			r.pos++
			return int64(int8(b<<1)) >> 1, nil
		}
	}
	v, err := r.leb128(true, 64)
	return int64(v), err
}
