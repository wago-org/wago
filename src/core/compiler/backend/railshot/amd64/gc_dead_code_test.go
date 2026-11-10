// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSkipImmediatesConsumesGCStructNew(t *testing.T) {
	r := wasm.NewReader([]byte{0x00, 0x23, 0x0b}) // struct.new subopcode, type 35, end
	if err := skipImmediates(r, 0xfb); err != nil {
		t.Fatalf("skip struct.new immediate: %v", err)
	}
	if got := r.Offset(); got != 2 {
		t.Fatalf("reader offset = %d, want 2", got)
	}
	if op, err := r.Byte(); err != nil || op != 0x0b {
		t.Fatalf("next opcode = %#x, %v; want end", op, err)
	}
}
