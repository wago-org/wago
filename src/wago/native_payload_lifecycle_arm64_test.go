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

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestCompactExecutionDataNativePayloadOwner(t *testing.T) {
	const segmentBytes = minLargeActiveDataBytes / minLargeActiveDataSegments
	segments := make([][]byte, minLargeActiveDataSegments)
	for i := range segments {
		segment := append([]byte{0, 0x41}, wasmtest.SLEB32(int32(i*segmentBytes))...)
		segment = append(segment, 0x0b)
		segment = append(segment, wasmtest.ULEB(segmentBytes)...)
		segments[i] = append(segment, bytes.Repeat([]byte{byte(i + 1)}, segmentBytes)...)
	}
	source := wasmtest.Module(
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 2})),
		wasmtest.Section(11, wasmtest.Vec(segments...)),
	)
	closedWithoutInstance, err := Compile(bytes.Clone(source))
	if err != nil {
		t.Fatal(err)
	}
	closedOwner := closedWithoutInstance.executionView().compactData
	if err := closedWithoutInstance.Close(); err != nil {
		t.Fatal(err)
	}
	if closedOwner == nil || closedOwner.arena != nil || closedOwner.bytes != nil {
		t.Fatal("closing before instantiation retained the private payload")
	}

	compiled, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	private := compiled.executionView().compactData
	if private == nil || private.arena == nil {
		t.Fatal("private payload was not mapped")
	}
	clear(source)
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if value, ok := instance.Read(0, 1); !ok || !bytes.Equal(value, []byte{1}) {
		t.Fatal("caller-owned source changed the private payload")
	}
	if err := compiled.Close(); err != nil {
		t.Fatal(err)
	}
	if private.arena == nil {
		t.Fatal("compiled close released a live instance's payload owner")
	}
	instance.Close()
	if private.arena != nil || private.bytes != nil || private.payloadSize() != minLargeActiveDataBytes {
		t.Fatal("the last instance did not release the mapping and preserve metadata")
	}
	if len(compiled.Data) != minLargeActiveDataSegments {
		t.Fatal("closing the private mapping changed public metadata")
	}
}
