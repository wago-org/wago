// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestIntegerMapScreenBudgetAndUnsupportedForms(t *testing.T) {
	m := &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}}
	for _, tc := range []struct {
		name string
		body []byte
		want bool
	}{
		{"load-store", []byte{0x40, 0x20, 0, 0x28, 2, 0, 0x20, 1, 0x36, 2, 0}, true},
		{"unsupported-load", []byte{0x40, 0x20, 0, 0x28, 2, 0, 0x2f, 1, 0, 0x36, 2, 0}, false},
		{"nested-control", []byte{0x40, 0x20, 0, 0x28, 2, 0, 0x02, 0x40, 0x36, 2, 0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fn{m: m}
			source := wasm.ReaderFrom(tc.body)
			if got := f.integerMapScreen(source); got != tc.want {
				t.Fatalf("screen=%v want=%v", got, tc.want)
			}
			if source.Offset() != 0 {
				t.Fatal("screen changed caller reader")
			}
		})
	}
	f := fn{m: m}
	body := []byte{0x40, 0x20, 0, 0x28, 2, 0, 0x20, 1, 0x36, 2, 0}
	for i := 0; i < 1000; i++ {
		f.integerMapScreen(wasm.ReaderFrom(body))
	}
	if f.integerMapWork > integerMapFunctionWorkLimit-integerMapMaxNodes+12 {
		t.Fatalf("unbounded probe work: %d", f.integerMapWork)
	}
	if f.integerMapScreen(wasm.ReaderFrom(body)) {
		t.Fatal("accepted after work budget exhausted")
	}
}
