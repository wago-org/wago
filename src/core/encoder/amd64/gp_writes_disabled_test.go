// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_regalloccheck

package amd64

import "testing"

func TestGPWritesOrdinaryNoObserver(t *testing.T) {
	var a Asm
	if old := a.ObserveGPWrites(func(uint32) { t.Fatal("ordinary build invoked GP observer") }); old != nil {
		t.Fatal("ordinary build retained GP observer")
	}
	a.MovImm64(R13, 0x1234567812345678)
	a.Add64(R13, R9)
	a.CallReg(R13)
	if old := a.ObserveGPWrites(nil); old != nil {
		t.Fatal("ordinary build retained GP observer")
	}
}
