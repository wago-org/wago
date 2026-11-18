// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package arm64

import (
	"encoding/binary"
	"errors"
)

// errIndexedReuseTarget reports that an omitted X16 address computation was
// later found to be entered by a branch, jump-table case, or landing pad whose
// predecessor never established X16. compileFunc retries without the reuse.
var errIndexedReuseTarget = errors.New("arm64: indexed-base reuse window contains a branch target")

// reuseDataRange marks non-instruction bytes (jump tables, literal data, plugin
// payloads). Jump-table words are signed offsets from the table start.
type reuseDataRange struct {
	start, end int
	jump       bool
}

func (f *fn) noteReuseData(start, end int, jump bool) {
	if end <= start {
		return
	}
	sc := f.scratchState()
	sc.reuseData = append(sc.reuseData, reuseDataRange{start: start, end: end, jump: jump})
}

// validateIndexedReuse proves that every indexed-base reuse in the finished body
// is entered only by straight-line fallthrough. It must run before any pass
// that moves code. Entry offsets are additional targets.
func (f *fn) validateIndexedReuse(entries ...int) error {
	if f.a.IndexedReuseCount() == 0 {
		return nil
	}
	sc := f.scratchState()
	targets := append(sc.reuseTargets[:0], entries...)
	b := f.a.B
	for _, r := range sc.reuseData {
		if !r.jump {
			continue
		}
		for pc := r.start; pc+4 <= r.end && pc+4 <= len(b); pc += 4 {
			targets = append(targets, r.start+int(int32(binary.LittleEndian.Uint32(b[pc:]))))
		}
	}
	sc.reuseTargets = targets
	next := 0
	opaque := func(pc int) bool {
		for next < len(sc.reuseData) && pc >= sc.reuseData[next].end {
			next++
		}
		// Ranges are recorded in emission order; check the current one and any
		// overlapping successor conservatively.
		for i := next; i < len(sc.reuseData) && sc.reuseData[i].start <= pc; i++ {
			if pc < sc.reuseData[i].end {
				return true
			}
		}
		return false
	}
	if !f.a.ValidateIndexedReuse(targets, opaque) {
		return errIndexedReuseTarget
	}
	return nil
}
