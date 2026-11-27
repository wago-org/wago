// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"fmt"
	"testing"
)

func TestCompiledLegacyTableWitnessEncoding(t *testing.T) {
	operations := []string{
		"table.size",
		"table64.grow",
		"table64.get/set",
		"table64 indirect-call",
		"table64.fill",
		"table64.copy",
		"table64.init/drop",
		"imported table64.size",
		"imported table64.get/set",
		"imported table64.grow",
		"imported table64.fill",
		"imported table64.copy",
		"imported table64.init/drop",
	}
	if len(operations) != int(nativeTable64ImportedInitDropABI) {
		t.Fatal("legacy table witness coverage is incomplete")
	}
	if bits, err := compiledLegacyTableWitness(nativeTable64NoABI); err != nil || bits != 0 {
		t.Fatalf("empty witness encoded as %#x, %v", bits, err)
	}
	for i, operation := range operations {
		t.Run(operation, func(t *testing.T) {
			bits, err := compiledLegacyTableWitness(uint8(i + 1))
			if compiledArchitecture == 4 {
				want := uint64(1) << (48 - i)
				if err != nil || bits != want {
					t.Fatalf("witness encoded as %#x, %v, want %#x", bits, err, want)
				}
				return
			}
			want := fmt.Sprintf("RV64 %s compiler witness on a foreign architecture", operation)
			if err == nil || err.Error() != want || bits != 0 {
				t.Fatalf("foreign witness encoded as %#x, %v, want %q", bits, err, want)
			}
		})
	}
}
