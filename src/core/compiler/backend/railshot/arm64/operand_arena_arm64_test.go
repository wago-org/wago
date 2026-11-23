// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64 && (darwin || linux)

package arm64

import "testing"

func TestMappedOperandScratchWorkerRelease(t *testing.T) {
	sc := newCompileScratch(defaultStackArenaCap)
	sc.mappedOperands = true
	sc.ensureTargetStack()
	arena := sc.stack.mappedArena()
	if arena == nil || len(arena.mapping) == 0 {
		t.Fatal("operand scratch has no native mapping")
	}
	t.Cleanup(arena.close)
	for range defaultStackArenaCap * 2 {
		sc.stack.alloc()
	}
	if len(sc.stack.chunks) < 2 {
		t.Fatal("operand scratch did not grow")
	}
	sc.finishStackWorker()
	if len(arena.mapping) != 0 || sc.stack.mappedArena() != nil || sc.stack.head != nil {
		t.Fatal("finished worker retained operand scratch")
	}
}

func TestMappedMemoryTrapSitesRelease(t *testing.T) {
	sc := newCompileScratch(defaultStackArenaCap)
	sc.mappedOperands = true
	t.Cleanup(sc.finishStackWorker)
	for i := range 512 {
		sc.appendTrapSite(trapMemOOB, trapSite{branch: uint32(i) * 4, function: 1, pc: uint32(i)})
	}
	arena := sc.trapArena
	if arena == nil {
		t.Fatal("large trap list stayed on the heap")
	}
	for i, site := range sc.trapSites[trapMemOOB] {
		want := trapSite{branch: uint32(i) * 4, function: 1, pc: uint32(i)}
		if site != want {
			t.Fatalf("trap site %d = %v, want %v", i, site, want)
		}
	}
	sc.finishStackWorker()
	if len(arena.mapping) != 0 || sc.trapArena != nil || sc.trapSites[trapMemOOB] != nil {
		t.Fatal("finished worker retained its memory trap mapping")
	}
}
