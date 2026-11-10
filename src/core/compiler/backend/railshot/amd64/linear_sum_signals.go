// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "os"

// The exact linear sum has no observable effects before a memory trap. Its
// existing proof covers the whole range and preserves modular memory32 wrap.
// Signal mode reads the current descriptor without reserving another register.
var linearSumSignalsEnabled = os.Getenv("WAGO_AMD64_LINEAR_SUM_SIGNALS") != "0"

func (f *fn) compareLinearSumSize(end Reg) {
	if f.memSizeReg != regNone {
		f.a.Cmp64(end, f.memSizeReg)
	} else {
		f.a.AluRM(cmpRMcode, end, RBX, -bdCurBytes, true)
	}
}
