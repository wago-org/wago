// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

import (
	"bytes"
	"testing"
)

func TestReviewMemoryIndexRoundTrip(t *testing.T) {
	want := []byte{0x28, 0x42, 0x01, 0x00, 0x0b}
	expr, err := decodeExpr(newReader(want), 0)
	if err != nil {
		t.Fatal(err)
	}
	if expr.Instrs[0].MemArg().Mem == nil || *expr.Instrs[0].MemArg().Mem != 1 {
		t.Fatal("decode did not select memory 1")
	}
	got, err := EncodeExpr(expr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("encoded memory-1 load = % x, want % x", got, want)
	}
}

func TestMemoryIndexEncodingBoundaries(t *testing.T) {
	for _, op := range []byte{0x28, 0x29, 0x36, 0x37} {
		for _, index := range []MemIdx{0, 1, 127, 128, 16384} {
			want := []byte{op, 0x42}
			appendU32(&want, uint32(index))
			want = append(want, 0, 0x0b)
			expr, err := decodeExpr(newReader(want), 0)
			if err != nil {
				t.Fatal(err)
			}
			got, err := EncodeExpr(expr)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("got %x, want %x", got, want)
			}
		}
	}
}
