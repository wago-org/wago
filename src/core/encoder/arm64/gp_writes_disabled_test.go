// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_regalloccheck

package arm64

import "testing"

func TestGPWritesOrdinaryObserverIsInert(t *testing.T) {
	var a Asm
	if old := a.ObserveGPWrites(func(uint32) { t.Fatal("ordinary build called GP observer") }); old != nil {
		t.Fatal("ordinary build retained observer state")
	}
	a.Add64(X6, X7, X8)
	a.LdpPost(X6, X7, SP, 16)
	a.Bl()
	if old := a.ObserveGPWrites(nil); old != nil {
		t.Fatal("ordinary build retained observer state")
	}
}
