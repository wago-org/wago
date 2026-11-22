// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"bytes"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSerialScratchReuseKeepsLiveCompiledImages(t *testing.T) {
	results := []wasm.ValType{wasm.I32}
	simple := mod1(t, nil, results, []byte{0, 0x41, 5, 0x0b})
	structured := mod1(t, nil, results, []byte{0, 0x02, 0x7f, 0x41, 42, 0x0b, 0x0b})
	opts := CompileOptions{Workers: 1, DeferCodeMapping: true}
	for range 8 {
		t.Run("worker", func(t *testing.T) {
			t.Parallel()
			first, err := CompileModuleWith(simple, opts)
			if err != nil {
				t.Fatal(err)
			}
			code := bytes.Clone(first.Code)
			entry := slices.Clone(first.Entry)
			internalEntry := slices.Clone(first.InternalEntry)

			for range 16 {
				if _, err := CompileModuleWith(structured, opts); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(first.Code, code) || !slices.Equal(first.Entry, entry) || !slices.Equal(first.InternalEntry, internalEntry) {
					t.Fatal("compiling another module changed the live compiled image")
				}
			}
		})
	}
}
