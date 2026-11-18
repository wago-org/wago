// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestFusedIfLocalWritePreservesBoundsTrap(t *testing.T) {
	memory := make([]byte, 65536)
	for i := range memory {
		memory[i] = byte(i)
	}
	for _, operation := range []struct {
		name string
		op   byte
		sign int32
	}{
		{"add", 0x6a, 1}, {"sub", 0x6b, -1},
	} {
		for _, offset := range []uint32{65525, 65526, 65530} {
			// local 1 starts at zero. Both supported fused arms update it to
			// a non-boolean value: x+7/x+3, or x-(-7)/x-(-3).
			body := []byte{1, 1, 0x7f, 0x20, 0, 0x04, 0x7f, 0x20, 1, 0x41}
			body = append(body, wasmtest.SLEB32(7*operation.sign)...)
			body = append(body, operation.op, 0x05, 0x20, 1, 0x41)
			body = append(body, wasmtest.SLEB32(3*operation.sign)...)
			body = append(body, operation.op, 0x0b, 0x21, 1, 0x20, 1, 0x28, 2)
			body = append(body, wasmtest.ULEB(offset)...)
			body = append(body, 0x0b)
			m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			for _, noFacts := range []bool{false, true} {
				for _, condition := range []uint32{0, 1} {
					t.Run(fmt.Sprintf("%s/offset%d/noBoundsFacts%v/condition%d", operation.name, offset, noFacts, condition), func(t *testing.T) {
						var stats ModuleStats
						opts := CompileOptions{NoBoundsFacts: noFacts}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperMemWithOptions(t, m, opts, condition, func(mem []byte) { copy(mem, memory) })
						if diagnosticsEnabled && stats.Funcs[0].Peephole["if-local-sink"] == 0 {
							t.Fatal("fused local write was not exercised")
						}
						address := offset + 3
						if condition != 0 {
							address = offset + 7
						}
						if address+4 > uint32(len(memory)) {
							if err == nil {
								t.Fatalf("load at %d must trap, returned %d", address, got)
							}
						} else {
							want := binary.LittleEndian.Uint32(memory[address : address+4])
							if err != nil || got != want {
								t.Fatalf("load at %d: got %#x, %v; want %#x", address, got, err, want)
							}
						}
					})
				}
			}
		}
	}
}
