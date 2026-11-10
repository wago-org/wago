// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestScalarLoopRegisterReadsIncludeAddressOperands(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, source := range []Reg{RAX, RBP, R9, R12, R15} {
			for _, kind := range []string{"move", "add", "compare", "lea-base", "lea-index", "immediate"} {
				var a x64.Asm
				reads, writes := maskOf(source), maskOf(R10)
				switch kind {
				case "move":
					if wide {
						a.MovReg64(R10, source)
					} else {
						a.MovRegReg32(R10, source)
					}
				case "add":
					a.AluRR(0x01, R10, source, wide)
					reads = reads.add(R10)
				case "compare":
					a.AluRR(0x39, R10, source, wide)
					reads, writes = reads.add(R10), 0
				case "lea-base":
					a.LeaDispW(R10, source, 0x4585e4, wide)
				case "lea-index":
					a.LeaScaledW(R10, R11, source, 2, 17, wide)
					reads = reads.add(R11)
				case "immediate":
					a.AluRI(0, R10, 0x4585e4, wide)
					reads = maskOf(R10)
				}
				gotReads, gotWrites, ok := scalarLoopBodyRegisters(a.B, maskOf(RBP))
				if !ok || gotReads != reads || gotWrites != writes {
					t.Fatalf("wide=%t source=%d kind=%s bytes=%x reads=%x/%x writes=%x/%x ok=%t", wide, source, kind, a.B, gotReads, reads, gotWrites, writes, ok)
				}
				for n := 0; n < len(a.B); n++ {
					if _, _, ok := scalarLoopBodyRegisters(a.B[:n], maskOf(RBP)); ok {
						t.Fatalf("accepted truncated instruction=%x", a.B[:n])
					}
				}
			}
		}
		var stackBase x64.Asm
		stackBase.LeaDispW(R10, RSP, 17, wide)
		reads, writes, ok := scalarLoopBodyRegisters(stackBase.B, 0)
		if !ok || reads != maskOf(RSP) || writes != maskOf(R10) {
			t.Fatalf("LEA no-index sentinel: reads=%x writes=%x ok=%t", reads, writes, ok)
		}
	}
	for _, b := range [][]byte{
		{0x41, 0x83, 0xd2, 1},          // ADC consumes incoming flags.
		{0x41, 0x83, 0xda, 1},          // SBB consumes incoming flags.
		{0x45, 0x0f, 0x45, 0xd4},       // CMOV consumes incoming flags.
		{0x45, 0x8b, 0x14, 0x24},       // A real memory load is not a LEA.
		{0x4c, 0x8d, 0x15, 0, 0, 0, 0}, // RIP-relative address.
	} {
		if _, _, ok := scalarLoopBodyRegisters(b, maskOf(RBP)); ok {
			t.Fatalf("accepted unsupported register body=%x", b)
		}
	}
}
