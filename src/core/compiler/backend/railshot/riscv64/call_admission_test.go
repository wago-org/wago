// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

import "testing"

// This exact portable fixture is also run against the accepted pre-call tree
// to record the previously unsupported opcode before its execution tests.
func TestDirectCallScalarLeafAdmission(t *testing.T) {
	b := binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x10, 1, 0x0b}}, fixtureFunc{result: 0x7f, body: []byte{0x41, 3, 0x0b}})
	if c, err := Compile(b); err != nil || c == nil {
		t.Fatal("direct scalar leaf admission", err)
	}
}
