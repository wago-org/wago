// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var fusedConstantHintsEnabled = os.Getenv("WAGO_ARM64_NO_FUSED_CONSTANT_HINTS") != "1"

// An adjacent multiply, constant, add is a bounded fused-consumer opportunity.
// MADD needs the addend in a register even when ordinary ADD accepts it inline.
// Zero already uses ZR. This changes only the existing finite lease scoring.
func fusedConstantHintConsumer(previous, next, typ byte, value int64) byte {
	if fusedConstantHintsEnabled && value != 0 && (typ == 1 && previous == 0x6c && next == 0x6a || typ == 2 && previous == 0x7e && next == 0x7c) {
		return previous
	}
	return next
}
