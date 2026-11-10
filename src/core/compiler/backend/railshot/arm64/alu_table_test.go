// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"testing"
	"unsafe"
)

func TestALUTableStaysCompactAndComplete(t *testing.T) {
	want := map[wOp]aluEnc{
		opAdd: {op: opAdd, comm: true},
		opSub: {op: opSub},
		opAnd: {op: opAnd, comm: true},
		opOr:  {op: opOr, comm: true},
		opXor: {op: opXor, comm: true},
	}
	if got := unsafe.Sizeof(aluTable); got != 12 {
		t.Fatalf("ALU encoding table = %d bytes, want 12", got)
	}
	for op, enc := range want {
		if got := aluTable[op]; got != enc {
			t.Errorf("ALU encoding %d = %+v, want %+v", op, got, enc)
		}
	}
}
