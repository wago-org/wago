// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

// Opt-in while paired compilation and execution qualification is pending.
var sharedLoopRegionFactsEnabled = os.Getenv("WAGO_AMD64_REGION_FACTS") == "1"

// loopRegionFacts lives only across the two loop-entry consumers. Completion
// certifies the pin scanner's strict flat, scalar, call-free loop language.
// Failed recognition never supplies facts to the more permissive call-free scan.
type loopRegionFacts struct {
	complete  bool
	bytes     int
	written   uint64
	constants [16]struct {
		value storage
		count uint8
	}
	count uint8
}

func (f *loopRegionFacts) observe(op byte, immediate wasm.Reader) {
	var value storage
	switch op {
	case 0x43:
		bits, _ := immediate.LEU32()
		value = storage{kind: stConst, typ: mtF32, cval: int64(bits)}
	case 0x44:
		bits, _ := immediate.LEU64()
		value = storage{kind: stConst, typ: mtF64, cval: int64(bits)}
	default:
		return
	}
	for i := 0; i < int(f.count); i++ {
		if f.constants[i].value.typ == value.typ && f.constants[i].value.cval == value.cval {
			f.constants[i].count++
			return
		}
	}
	if int(f.count) < len(f.constants) {
		f.constants[f.count].value = value
		f.constants[f.count].count = 1
		f.count++
	}
}

func scanLoopCallFreeRegion(r *wasm.Reader, classifier wasm.ModuleInstructionClassifier, gc bool, pins []int, wantFloat bool, budget *int, facts *loopRegionFacts) (bool, uint64, storage, bool) {
	// Fall back on budget exhaustion to preserve the old scanner's exact partial
	// accounting and admission behavior, including malformed-input exits.
	if facts == nil || !facts.complete || budget == nil || *budget >= maxCallFreeLoopLookaheadBytes || facts.bytes > maxCallFreeLoopLookaheadBytes-*budget {
		return scanLoopCallFreeDetails(r, classifier, gc, pins, wantFloat, budget)
	}
	*budget += facts.bytes
	var writes uint64
	for i, x := range pins {
		if x >= 0 && x < 64 && facts.written&(uint64(1)<<x) != 0 {
			writes |= uint64(1) << i
		}
	}
	if !wantFloat || facts.count == 0 {
		return true, writes, storage{}, false
	}
	best := 0
	for i := 1; i < int(facts.count); i++ {
		if facts.constants[i].count > facts.constants[best].count {
			best = i
		}
	}
	return true, writes, facts.constants[best].value, true
}
