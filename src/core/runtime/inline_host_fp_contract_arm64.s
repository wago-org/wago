// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_inline_host_experiment && (linux || darwin) && arm64 && !tinygo

#include "textflag.h"

// Test-only Go ABI helper: Go has no callee-saved floating register bank.
// Deliberately overwrite the native callee-saved bank from a real Go callback.
TEXT ·inlineHostProbeClobberFP(SB), NOSPLIT|NOFRAME, $0-0
	MOVD ZR, R0
	FMOVD R0, F8
	FMOVD R0, F9
	FMOVD R0, F10
	FMOVD R0, F11
	FMOVD R0, F12
	FMOVD R0, F13
	FMOVD R0, F14
	FMOVD R0, F15
	RET
