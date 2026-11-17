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

// This test intentionally never calls the recursive oracle: TinyGo's fixed
// stack is precisely the production constraint the iterative scanner addresses.
func TestIterativeHintsDeepWithoutOracle(t *testing.T) {
	for _, n := range []int{20000, 20001} {
		b := make([]byte, 0, 3*n+1)
		for i := 0; i < n; i++ {
			b = append(b, 2, 0x40)
		}
		for i := 0; i <= n; i++ {
			b = append(b, 0x0b)
		}
		_, err := scanBodyBytes(b, 0, 0, 0)
		if n == 20000 {
			if err != nil {
				t.Fatal(err)
			}
		} else {
			de, ok := err.(*wasm.DecodeError)
			if !ok || de.Code != wasm.ErrInstructionNestingLimitExceeded || de.Offset != 2*n {
				t.Fatalf("depth %d error=%v", n, err)
			}
		}
		err = scanBodyBytesGlobalScores(nil, nil, b, 0, func(uint32, int64) {})
		if n == 20000 {
			if err != nil {
				t.Fatal(err)
			}
		} else {
			de, ok := err.(*wasm.DecodeError)
			if !ok || de.Code != wasm.ErrInstructionNestingLimitExceeded || de.Offset != 2*n {
				t.Fatalf("global depth %d error=%v", n, err)
			}
		}
	}
}
