// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSWARPublicSelectorBoundsBeforeScanning(t *testing.T) {
	oversized := make([]byte, MaxPublicCodeBytes+1)
	for _, module := range []*wasm.Module{
		nil,
		{Code: []wasm.Func{{BodyBytes: oversized}}},
		{Code: []wasm.Func{{BodyBytes: oversized[:MaxPublicCodeBytes]}}, Data: []wasm.Data{{Init: []byte{1}}}},
	} {
		if yes, err := UsesSWAR(module); yes || !errors.Is(err, ErrUnsupported) {
			t.Fatal("unbounded decoded selector", yes, err)
		}
	}
	if yes, err := UsesSWAR(&wasm.Module{Data: make([]wasm.Data, 257)}); yes || err != nil {
		t.Fatal("inert public data declarations rejected", yes, err)
	}
	for _, module := range []*wasm.Module{
		{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.V128}}}},
		{Code: []wasm.Func{{BodyBytes: []byte{0xfd, 12}}}},
	} {
		if yes, err := UsesSWAR(module); !yes || err != nil {
			t.Fatal("bounded actual SIMD profile", yes, err)
		}
	}
}
