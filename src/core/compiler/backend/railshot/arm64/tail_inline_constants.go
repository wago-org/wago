// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// A tail-loop inline has no native calls. Reuse the existing bounded immutable
// constant leases for its unsigned division multipliers and remainder divisors.
func (f *fn) noteTailInlineDivConstants(body []byte) {
	if f.makesCalls || !f.opt(optMagicDiv) || len(body) > 512 {
		return
	}
	r := wasm.ReaderFrom(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return
		}
		if op != 0x41 {
			if f.classifier.SkipImmediate(&r, op) != nil {
				return
			}
			continue
		}
		value, err := r.I32()
		if err != nil {
			return
		}
		next, ok := r.Peek()
		d := uint32(value)
		if !ok || (next != 0x6e && next != 0x70) || d <= 1 || d&(d-1) == 0 {
			continue
		}
		magic, _, _ := railshot.MagicU(uint64(d), 32)
		f.noteTailInlineDivConstant(int64(int32(uint32(magic))))
		if next == 0x70 {
			f.noteTailInlineDivConstant(int64(value))
		}
	}
}
func (f *fn) noteTailInlineDivConstant(value int64) {
	h := &f.scopedConstHints
	for i := 0; i < int(h.loopIntConstCount); i++ {
		if h.loopIntConst[i] == value {
			return
		}
	}
	if int(h.loopIntConstCount) == len(h.loopIntConst) {
		return
	}
	i := h.loopIntConstCount
	h.loopIntConst[i] = value
	h.loopIntConstTypes |= 1 << (2 * i)
	h.loopIntConstCount++
}
func (f *fn) preloadTailInlineDivConstants() {
	if f.makesCalls || f.scopedConstHints.loopIntConstCount == 0 {
		return
	}
	h := funcHintView{funcHints: funcHints{flags: f.scopedConstHints.flags}, loopIntConst: f.scopedConstHints.loopIntConst, loopIntConstTypes: f.scopedConstHints.loopIntConstTypes, loopIntConstCount: f.scopedConstHints.loopIntConstCount}
	before := f.iconstN
	f.preloadLoopIntConsts(&h)
	f.stats.peepN("tail-inline-div-const", int(f.iconstN-before))
}
