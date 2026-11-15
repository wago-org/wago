// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestInlineDefinedCastStaticSubtypePath(t *testing.T) {
	savedShared, savedInline := gcSharedStubsEnabled, gcInlineCastsEnabled
	defer func() { gcSharedStubsEnabled, gcInlineCastsEnabled = savedShared, savedInline }()
	gcSharedStubsEnabled = true
	var intervalLoad x64.Asm
	intervalLoad.Load64(R10, R8, gc.NativeViewSubtypeIntervalsOffset)
	for _, tc := range []struct {
		name         string
		final, exact bool
	}{
		{"final", true, false}, {"open", false, false}, {"exact-open", false, true}, {"exact-final", true, true},
	} {
		for _, nullable := range []bool{false, true} {
			for _, inline := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/nullable-%t/inline-%t", tc.name, nullable, inline), func(t *testing.T) {
					gcInlineCastsEnabled = inline
					decl := byte(0x50)
					if tc.final {
						decl = 0x4f
					}
					sub, result := byte(22), byte(0x64)
					if nullable {
						sub, result = 23, 0x63
					}
					body := []byte{0x20, 0, 0xfb, sub}
					if tc.exact {
						body = append(body, 0x62)
					}
					body = append(body, 0, 0x0b)
					data := wasmtest.Module(
						wasmtest.Section(1, wasmtest.Vec([]byte{decl, 0, 0x5f, 0}, []byte{0x60, 1, 0x6e, 1, result, 0})),
						wasmtest.Section(3, wasmtest.Vec([]byte{1})),
						wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
					)
					m, err := wasm.DecodeModule(data)
					if err != nil {
						t.Fatal(err)
					}
					if err = wasm.ValidateModule(m); err != nil {
						t.Fatal(err)
					}
					cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true})
					if cm != nil && cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					if err != nil {
						t.Fatal(err)
					}
					hasInterval := bytes.Contains(cm.Code, intervalLoad.B)
					wantInterval := !inline || !(tc.final || tc.exact)
					if hasInterval != wantInterval {
						t.Fatalf("subtype interval load present=%t, want %t for final=%t exact=%t inline=%t", hasInterval, wantInterval, tc.final, tc.exact, inline)
					}
					t.Logf("code bytes=%d", len(cm.Code))
				})
			}
		}
	}
}

func TestInlineDefinedCastStaticTargetLoad(t *testing.T) {
	savedShared, savedInline := gcSharedStubsEnabled, gcInlineCastsEnabled
	defer func() { gcSharedStubsEnabled, gcInlineCastsEnabled = savedShared, savedInline }()
	gcSharedStubsEnabled = true
	var dynamicLoad x64.Asm
	dynamicLoad.ImulRI(RDX, 4, true)
	dynamicLoad.LoadIdx(RDX, R10, RDX, 0, 4, false, false)
	for _, target := range []uint32{0, 1, 127, 128, 255} {
		for _, final := range []bool{false, true} {
			for _, inline := range []bool{false, true} {
				t.Run(fmt.Sprintf("type-%d/final-%t/inline-%t", target, final, inline), func(t *testing.T) {
					gcInlineCastsEnabled = inline
					decl := byte(0x50)
					if final {
						decl = 0x4f
					}
					var types [][]byte
					for i := uint32(0); i <= target; i++ {
						types = append(types, []byte{decl, 0, 0x5f, 0})
					}
					heap := wasmtest.SLEB64(int64(target))
					ft := append([]byte{0x60, 1, 0x6e, 1, 0x64}, heap...)
					types = append(types, ft)
					body := append([]byte{0x20, 0, 0xfb, 22}, heap...)
					body = append(body, 0x0b)
					data := wasmtest.Module(
						wasmtest.Section(1, wasmtest.Vec(types...)),
						wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(target+1))),
						wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
					)
					m, err := wasm.DecodeModule(data)
					if err != nil {
						t.Fatal(err)
					}
					if err = wasm.ValidateModule(m); err != nil {
						t.Fatal(err)
					}
					cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true})
					if cm != nil && cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					if err != nil {
						t.Fatal(err)
					}
					if got := bytes.Contains(cm.Code, dynamicLoad.B); got != !inline {
						t.Fatalf("runtime target scaling present=%t, want %t", got, !inline)
					}
					var constantLoad x64.Asm
					constantLoad.Load32(RDX, R10, int32(target*4))
					if inline && !bytes.Contains(cm.Code, constantLoad.B) {
						t.Fatal("inline cast does not load its known local target by displacement")
					}
					t.Logf("code bytes=%d", len(cm.Code))
				})
			}
		}
	}
}
