// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build riscv64 && !tinygo

package main

import "github.com/wago-org/wago/codegen"

// CustomSignature registration requires nonnil target machine code. Until RV64
// has genuine custom lowering, the example returns that registration error
// before compiling or entering a guest; it cannot select a foreign ISA.
func zeroCodegen() codegen.Lowering    { return nil }
func discardCodegen() codegen.Lowering { return nil }
