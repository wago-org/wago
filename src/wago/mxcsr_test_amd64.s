// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !tinygo

#include "textflag.h"

TEXT ·readTestMXCSR(SB), NOSPLIT, $0-4
	STMXCSR ret+0(FP)
	RET

TEXT ·writeTestMXCSR(SB), NOSPLIT, $0-4
	LDMXCSR value+0(FP)
	RET
