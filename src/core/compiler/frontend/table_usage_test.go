// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package frontend

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSupportedTableRuntimeShapesTracksGrowPerTable(t *testing.T) {
	huge := uint64(1 << 40)
	small := uint64(4)
	m := wasm.Module{
		Tables: []wasm.Table{
			{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Min: 1, Max: huge, HasMax: true}}},
			{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Min: 1, Max: small, HasMax: true}}},
		},
		Code: []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrTableGrow, Index: 1}}}}},
	}
	shapes, err := SupportedTableRuntimeShapes(&m)
	if err != nil {
		t.Fatal(err)
	}
	if got := shapes[0].Capacity; got != 1 {
		t.Fatalf("inert table capacity = %d, want minimum 1", got)
	}
	if got := shapes[1].Capacity; got != 4 {
		t.Fatalf("grown table capacity = %d, want declared maximum 4", got)
	}

	m.Code[0].Body.Instrs[0].Index = 2
	if _, err := SupportedTableRuntimeShapes(&m); err == nil {
		t.Fatal("out-of-range table.grow index accepted")
	}
}
