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
)

func TestConstantPreloadScanGates(t *testing.T) {
	tests := []struct {
		name      string
		body      []byte
		wantFloat int
		wantSIMD  int
	}{
		{name: "integer", body: []byte{0x00, 0x41, 1, 0x1a, 0x0b}},
		{name: "float", body: []byte{0x00, 0x43, 0, 0, 0x80, 0x3f, 0x1a, 0x0b}, wantFloat: 1},
		{name: "simd", body: append([]byte{0x00, 0xfd, 12}, append(make([]byte, 16), 0x1a, 0x0b)...), wantSIMD: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, nil, nil, tc.body)
			s := compileWithStats(t, m, false).Funcs[0]
			if got := s.Peephole["float-preload-scan"]; got != tc.wantFloat {
				t.Fatalf("float preload scans = %d, want %d (all %v)", got, tc.wantFloat, s.Peephole)
			}
			if got := s.Peephole["v128-preload-scan"]; got != tc.wantSIMD {
				t.Fatalf("SIMD preload scans = %d, want %d (all %v)", got, tc.wantSIMD, s.Peephole)
			}
		})
	}
}
