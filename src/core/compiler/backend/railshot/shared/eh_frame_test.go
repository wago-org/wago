// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package shared

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Typed-reference block types carry a heap-type immediate after 0x63/0x64.
// The scan must consume it rather than decode it as the next opcode.
func TestScanEHFrameShapeTypedReferenceBlockTypes(t *testing.T) {
	classifier := wasm.NewModuleInstructionClassifier(&wasm.Module{}, true)
	body := []byte{
		0x02, 0x69, // block (result exnref)
		0x02, 0x63, 0x16, // block (result (ref null 22))
		0x1f, 0x63, 0x16, 1, byte(wasm.CatchAllRef), 1, // try_table (result (ref null 22)) (catch_all_ref 1)
		0x1f, 0x64, 0x16, 0, // try_table (result (ref 22))
		0x00,             // unreachable
		0x0b, 0x0b, 0x0b, // end end end
		0x1a, 0x0b, // drop, end block
		0x1a, 0x0b, // drop, end function
	}
	shape, err := ScanEHFrameShape(&classifier, body)
	if err != nil {
		t.Fatal(err)
	}
	if shape.TryRecords != 2 || shape.RootRecords != 1 {
		t.Fatalf("shape = %+v, want 2 try records and 1 root record", shape)
	}
}
