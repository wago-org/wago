// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_regalloccheck

package riscv64

import "github.com/wago-org/wago/internal/regalloccheck"

const regallocCheckEnabled = false

// Leading empty compatibility state preserves the ordinary Asm layout.
type regallocState struct{}

func (*Asm) ObserveFPWrites(func(uint32)) func(uint32)                             { return nil }
func (*Asm) ObserveGPWrites(func(uint32)) func(uint32)                             { return nil }
func (*Asm) ObserveRegalloc(func(regalloccheck.Effect)) func(regalloccheck.Effect) { return nil }
func (*Asm) regallocDefinition(Reg, bool)                                          {}
func (*Asm) regallocFloatDefinition(FReg)                                          {}
func (*Asm) regallocImmediate(uint32, uint32, Reg, Reg, int32)                     {}
func (*Asm) regallocStore(Reg, Reg, int32, uint32)                                 {}

func (*Asm) regallocAtomic(Reg, Reg, bool, int) {}
