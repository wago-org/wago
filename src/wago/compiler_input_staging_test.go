// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64 && (darwin || linux)

package wago

import (
	"bytes"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestCompilerInputStagingRetainsCallerViews(t *testing.T) {
	if !compilerInputStaging {
		t.Skip("temporary input staging unavailable")
	}
	segment := func(offset byte, data []byte) []byte {
		header := append([]byte{0, 0x41, offset, 0x0b}, wasmtest.ULEB(uint32(len(data)))...)
		return append(header, data...)
	}
	makeSource := func(value byte) []byte {
		input := wasmtest.Module(
			wasmtest.Section(0, append([]byte{1, 'x'}, make([]byte, 128<<10)...)),
			wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
			wasmtest.Section(11, wasmtest.Vec(segment(0, []byte{value}), segment(1, nil))),
		)
		return append(make([]byte, 0, len(input)+32), input...)
	}
	original := makeSource(7)
	decoded, err := wasm.DecodeModule(original)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(original)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if len(compiled.Data) != len(decoded.Data) {
		t.Fatal("staging changed the public data directory")
	}
	for index, data := range compiled.Data {
		want := decoded.Data[index].Init
		if !bytes.Equal(data.Bytes, want) || cap(data.Bytes) != cap(want) ||
			unsafe.SliceData(data.Bytes) != unsafe.SliceData(want) {
			t.Fatalf("public data %d no longer views the caller's source", index)
		}
	}
	second, err := Compile(makeSource(19))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	clear(original)
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if value, ok := instance.Read(0, 1); !ok || !bytes.Equal(value, []byte{7}) {
		t.Fatal("execution snapshot changed after another compile and caller mutation")
	}
}
