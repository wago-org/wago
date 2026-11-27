// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_no_memory64

package wasm

type memargIndexedWidths struct{}

func moduleMemargWidths(*Module) memargWidths {
	return fixedMemargWidths(false)
}

func (w memargWidths) offset64(uint32) bool {
	return w.fixed64
}
