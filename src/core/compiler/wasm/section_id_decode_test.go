// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

import (
	"errors"
	"testing"
)

func TestDecodeRejectsReservedSectionID14(t *testing.T) {
	data := module(section(14, 0x00))
	for _, path := range []struct {
		name   string
		decode func([]byte) error
	}{
		{name: "AST", decode: func(b []byte) error { _, err := decodeModuleASTForTest(b); return err }},
		{name: "DecodeModule", decode: func(b []byte) error { _, err := DecodeModule(b); return err }},
		{name: "DecodeModuleByteBacked", decode: func(b []byte) error { _, err := DecodeModuleByteBacked(b); return err }},
	} {
		t.Run(path.name, func(t *testing.T) {
			err := path.decode(data)
			var de *DecodeError
			if !errors.As(err, &de) || de.Code != ErrInvalidSection {
				t.Fatalf("expected ErrInvalidSection, got %#v / %v", de, err)
			}
			if de.SectionID != 14 || de.SectionStart != 10 || de.SectionEnd != len(data) {
				t.Fatalf("unexpected section diagnostic: %#v", de)
			}
		})
	}
}
