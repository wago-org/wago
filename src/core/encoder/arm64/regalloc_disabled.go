// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_regalloccheck

package arm64

import "github.com/wago-org/wago/internal/regalloccheck"

// Guard each hook with this constant so ordinary builds erase argument setup too.
const regallocCheckEnabled = false

// Keep this first in Asm: an empty trailing field can grow a Go struct.
type regallocState struct{}

func regallocCall(*Asm) {}

func (*Asm) ObserveRegalloc(func(regalloccheck.Effect)) func(regalloccheck.Effect) { return nil }
func (*Asm) regallocCopy(Reg, Reg, bool, int)                                      {}
func (*Asm) regallocLoad(Reg, Reg, int32, bool, int)                               {}
func (*Asm) regallocStore(Reg, int32, Reg, bool, int)                              {}
func (*Asm) ObserveFrameLoad(Reg, int32, int)                                      {}
func (*Asm) ObserveFrameStore(Reg, int32, int)                                     {}
func regallocWidth(wide bool) int                                                  { return 0 }

func (*Asm) regallocCrossCopy(Reg, Reg, bool, int) {}
func (*Asm) regallocKillFP(Reg)                    {}

// ObserveGPWrites is inert in ordinary builds and retains no callback state.
func (*Asm) ObserveGPWrites(func(uint32)) func(uint32) { return nil }
func (*Asm) regallocGPWrites(uint32)                   {}
func regallocGPMask(Reg, bool) uint32                  { return 0 }

func (*Asm) ObserveFPWrites(func(uint32)) func(uint32) { return nil }
func (*Asm) regallocFPWrites(uint32)                   {}
