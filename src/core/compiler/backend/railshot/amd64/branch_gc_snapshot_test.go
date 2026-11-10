// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"reflect"
	"testing"
)

func TestRecordBranchGCRootsSkipsScalarStack(t *testing.T) {
	f := fn{s: newStackWithCap(128)}
	for range 128 {
		f.pushValue(storage{kind: stSlot, typ: mtI64})
	}
	fr := ctrlFrame{height: 126, resultN: 2}
	f.recordGCBranchResults(&fr, 2)
	if f.tmpRoots != nil || fr.has(ctrlHasResultGCRoots) {
		t.Fatal("scalar-only branch collected GC roots")
	}
}

func TestBranchGCRootsPreserveTypedSnapshot(t *testing.T) {
	f := fn{s: newStack()}
	want := []machineType{mtI32, mtI64, mtF64, mtI64}
	for i, typ := range want {
		value := f.pushValue(storage{kind: stSlot, typ: typ, slot: uint32(i)})
		if i == 1 || i == 3 {
			f.setStackGCRoot(value, true)
		}
	}
	fr := ctrlFrame{height: 2, resultN: 2}
	roots := f.rootsBottomToTop()
	f.recordGCBranchResultRoots(&fr, 2, roots)
	if got := f.logicalTypes(roots); !reflect.DeepEqual(got, want) {
		t.Fatalf("types=%v want=%v", got, want)
	}
	if got := f.frameResultGCRoots(&fr); !reflect.DeepEqual(got, []bool{false, true}) {
		t.Fatalf("result roots=%v", got)
	}
	if fr.has(ctrlHasBaseGCRoots) {
		t.Fatal("result capture changed prefix flags")
	}
}
