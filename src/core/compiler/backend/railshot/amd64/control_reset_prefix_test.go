// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "testing"

func TestControlResetOnlyTracksTouchedSidecarPrefix(t *testing.T) {
	sc := newScratch()
	sc.ctrlMerges = make([]ctrlFrameMerge, 2048)
	f := fn{sc: sc}
	deep := ctrlFrame{mergeIndex: 2048}
	f.ctrlMerge(&deep).branchState = []locState{lsStackReg}
	f.ensureCtrlRoots(&deep).flags = []bool{true}
	sc.reset()
	if sc.ctrlMerges[2047].branchState != nil || sc.ctrlRoots[:cap(sc.ctrlRoots)][2047].flags != nil {
		t.Fatal("deep references survived reset")
	}
	shallow := ctrlFrame{mergeIndex: 1}
	f.ctrlMerge(&shallow).branchState = []locState{lsStackReg}
	f.ensureCtrlRoots(&shallow).flags = []bool{true}
	if sc.ctrlMergeTouched != 1 {
		t.Fatalf("shallow function touched %d slots", sc.ctrlMergeTouched)
	}
	sc.reset()
	if sc.ctrlMerges[0].branchState != nil || sc.ctrlRoots[:cap(sc.ctrlRoots)][0].flags != nil {
		t.Fatal("shallow references survived reset")
	}
	if sc.ctrlMergeTouched != 0 {
		t.Fatal("reset retained touched prefix")
	}
}
