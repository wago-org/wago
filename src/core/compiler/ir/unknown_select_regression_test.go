// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package ir

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestBuildUnreachableSelectUnknownType(t *testing.T) {
	for _, op := range []byte{0x9a, 0x8c, 0x79} {
		m := rawModule(wasm.FuncType{}, []byte{0x00, 0x1b, op, 0x1a, 0x0b})
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		assertBuilds(t, m, "trap")
	}
}
