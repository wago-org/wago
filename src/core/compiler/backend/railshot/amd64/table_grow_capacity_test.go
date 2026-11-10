// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestTableGrowReleasesSnapshotReference(t *testing.T) {
	sc := newScratch()
	f := &fn{
		a: &encoderamd64.Asm{}, s: newStack(), sc: sc, transient: sc.transient,
		m:               &wasm.Module{Tables: []wasm.Table{{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc)}}}},
		reserved:        maskOf(R12, R13, R14, R15),
		pinnedLocalMask: maskOf(RBP, R9, R10, R11),
	}
	f.pushValue(storage{kind: stConst, typ: mtI64})
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	if err := f.tableGrow(wasm.NewReader([]byte{0})); err != nil {
		t.Fatal(err)
	}
	if f.depth() != 1 || f.s.back().st.typ != mtI32 || f.s.back().st.kind != stReg {
		t.Fatalf("table.grow result = %+v", f.s.back().st)
	}
	if f.pinned != 0 || f.regUser[f.s.back().st.reg] != f.s.back() {
		t.Fatal("table.grow left temporary pins or lost its result register")
	}
}
