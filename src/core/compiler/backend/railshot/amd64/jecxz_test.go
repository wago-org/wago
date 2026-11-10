// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSizeDirectJecxzBulkTails(t *testing.T) {
	requireCompilerDiagnostics(t)
	params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}
	tests := []struct {
		name string
		body []byte
	}{
		{"memory.copy", []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0a, 0x00, 0x00, 0x0b}},
		{"memory.fill", []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0b, 0x00, 0x0b}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := modMem(t, 1, params, nil, test.body)
			stats := &ModuleStats{}
			cm, err := CompileModuleWith(m, CompileOptions{CompactNative: true, Stats: stats, Workers: 1})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				t.Cleanup(func() { cm.CodeImage.Close() })
			}
			if got := stats.Funcs[0].Peephole["direct-jecxz"]; got != 1 {
				t.Fatalf("direct-jecxz hits = %d, want 1", got)
			}
		})
	}
}
