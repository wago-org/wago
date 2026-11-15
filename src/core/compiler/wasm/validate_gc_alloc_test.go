// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

import (
	"errors"
	"reflect"
	"testing"
)

func gcStreamCastModule(t *testing.T, count int) *Module {
	t.Helper()
	body := []byte{0} // zero local declarations
	for i := 0; i < count; i++ {
		body = append(body, 0xd0, 0x6d, 0xfb, 23, 0x6d, 0x1a)
	}
	body = append(body, 0x0b)
	code := append([]byte{1}, u32(uint32(len(body)))...)
	code = append(code, body...)
	m, err := DecodeModule(module(section(secType, 1, 0x60, 0, 0), section(secFunction, 1, 0), section(secCode, code...)))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestValidateGCImmediatesDoNotAllocatePerOpcode(t *testing.T) {
	one := validationAllocs(t, gcStreamCastModule(t, 1))
	many := validationAllocs(t, gcStreamCastModule(t, 96))
	t.Logf("validation allocations: one GC cast=%.0f, 96 GC casts=%.0f", one, many)
	if many-one > 2 {
		t.Fatalf("GC cast validation added %.0f allocations, want bounded scratch", many-one)
	}
}

func TestStreamedGCDecodeMatchesOwned(t *testing.T) {
	cases := [][]byte{
		{0xfb, 20, 0x6d}, {0xfb, 21, 1}, {0xfb, 22, 0x6d}, {0xfb, 23, 1},
		{0xfb, 22, 0x62, 1}, {0xfb, 23, 0x62, 1},
		{0xfb, 24, 3, 0, 0x6d, 0x6c}, {0xfb, 25, 1, 0x80, 1, 0x6e, 0x6d},
		{0xfb, 35, 1}, {0xfb, 36, 0x62, 1}, {0xfb, 2, 0, 1}, {0xfb, 15},
	}
	var fv funcValidator
	for _, data := range cases {
		for cut := 1; cut <= len(data); cut++ {
			data := data[:cut]
			fv.opExt = instrExt{HeapType2: AbsHeap(HeapFunc), Indices: []uint32{777}, ValTypes: []ValType{I64}}
			fv.rd.reset(data)
			var got directOp
			gotErr := fv.decodeDirectOp(&fv.rd, fixedMemargWidths(false), false, &got)
			owned := reader{data: data, pos: 1}
			want, wantErr := decodeFB(&owned)
			if !reflect.DeepEqual(gotErr, wantErr) || fv.rd.off() != owned.off() || !reflect.DeepEqual(got.instr, want) {
				t.Fatalf("GC immediate %x: streamed=%+v/%v@%d owned=%+v/%v@%d", data, got.instr, gotErr, fv.rd.off(), want, wantErr, owned.off())
			}
		}
	}
}

func TestOwnedGCDecodedInstructionsRetainIndependentImmediates(t *testing.T) {
	first, err := decodeFB(newReader([]byte{24, 0, 0, 0x6e, 0x6d}))
	if err != nil {
		t.Fatal(err)
	}
	original := *first.ext
	second, err := decodeFB(newReader([]byte{22, 0x6c}))
	if err != nil {
		t.Fatal(err)
	}
	if first.ext == second.ext || !reflect.DeepEqual(*first.ext, original) {
		t.Fatal("owned GC instructions share mutable immediate storage")
	}
}

func TestStreamedGCCastStackTypeSurvivesScratchReuse(t *testing.T) {
	// The first eqref stays on the stack while a later i31 cast reuses opExt.
	// Returning it must still match the declared nullable eqref result.
	body := []byte{0, 0x20, 0, 0xfb, 23, 0x6d, 0x20, 1, 0xfb, 23, 0x6c, 0x1a, 0x0b}
	code := append([]byte{1}, u32(uint32(len(body)))...)
	code = append(code, body...)
	m, err := DecodeModule(module(section(secType, 1, 0x60, 2, 0x6e, 0x6e, 1, 0x6d), section(secFunction, 1, 0), section(secCode, code...)))
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateModule(m); err != nil {
		t.Fatalf("earlier cast type changed after scratch reuse: %v", err)
	}
}

func TestStreamedGCImmediateErrorsKeepCodesAndOffsets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   []byte
		code   DecodeErrorCode
		offset int
	}{
		{"invalid branch flags", []byte{0xfb, 24, 4}, ErrInvalidInstruction, 2},
		{"truncated exact cast", []byte{0xfb, 22, 0x62}, ErrIndexOutOfBounds, 3},
		{"missing second branch heap type", []byte{0xfb, 24, 0, 0, 0x6e}, ErrIndexOutOfBounds, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fv funcValidator
			fv.rd.reset(tc.data)
			var op directOp
			err := fv.decodeDirectOp(&fv.rd, fixedMemargWidths(false), false, &op)
			var de *DecodeError
			if !errors.As(err, &de) || de.Code != tc.code || de.Offset != tc.offset {
				t.Fatalf("decode=%v, want code=%v offset=%d", err, tc.code, tc.offset)
			}
		})
	}
}
