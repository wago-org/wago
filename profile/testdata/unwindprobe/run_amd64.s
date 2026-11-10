// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

#include "textflag.h"
TEXT ·run(SB),NOSPLIT|NOFRAME,$0-24
 MOVQ code+0(FP), AX
 MOVQ sp+8(FP), CX
 MOVQ depth+16(FP), DI
 MOVQ SP, R12
 MOVQ BP, R13
 MOVQ CX, SP
 CALL AX
 MOVQ R12, SP
 MOVQ R13, BP
 RET
