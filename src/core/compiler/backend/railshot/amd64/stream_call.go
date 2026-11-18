// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && !tinygo && !wago_profile

package amd64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// streamCallTarget is shared by admission and emission. Imported calls and
// selected inline bodies retain their existing lowering, as do GC callsites.
func (f *fn) streamCallTarget(idx uint32) (*wasm.CompType, int, bool) {
	imported := f.importedFunctionCount()
	if uint64(idx) < uint64(imported) || !f.opt(optRegABI) || f.gcFrameRoots != nil {
		return nil, 0, false
	}
	ft, ok := f.functionSignature(idx)
	if !ok || len(ft.Params) > len(intArgRegs) || len(ft.Results) > 1 || !sigIsIntOnly(ft) || !sigFitsRegABI(ft) || f.selectedInlineTarget(idx) != nil {
		return nil, 0, false
	}
	return ft, int(idx) - imported, true
}

// integerCall uses canonical frame arguments rather than importing the legacy
// operand graph. The direct ABI preserves module-global registers and the
// memory-bound cache; only ordinary value-pinned globals need cell coherence.
func (s *streamState) integerCall(r *wasm.Reader) error {
	idx, err := r.U32()
	if err != nil {
		return err
	}
	f := s.target
	ft, localIdx, ok := f.streamCallTarget(idx)
	if !ok || len(s.stack) < len(ft.Params) {
		return fmt.Errorf("stream: unsupported direct call %d", idx)
	}
	// Select scratch before changing state. No persistent module/global register
	// can be borrowed, even though all compact owners will be evacuated below.
	excluded := s.reserved | uint64(f.reserved.union(f.pinned).union(f.pinnedLocalMask))
	for _, global := range f.globalPinIndices[:f.nGlobalPins] {
		if reg := globalRegValue(f.globalReg[int(global)]); reg != regNone {
			excluded |= uint64(1) << reg
		}
	}
	scratch := regNone
	for _, reg := range s.regs {
		if excluded&(uint64(1)<<reg) == 0 {
			scratch = Reg(reg)
			break
		}
	}
	if scratch == regNone {
		return fmt.Errorf("stream: no direct-call scratch register")
	}
	depth, params := len(s.stack), len(ft.Params)
	s.canonicalize(false)
	s.evict(^uint64(0))
	// All stream registers and backing generations are now in stable frames.
	// This is deliberately not storePinnedGlobals: that helper uses f.allocReg.
	for _, global := range f.globalPinIndices[:f.nGlobalPins] {
		g := int(global)
		reg := globalRegValue(f.globalReg[g])
		if reg == regNone || f.isModuleGlobal(g) {
			continue
		}
		f.a.Load64(scratch, RBX, -int32(abi.GlobalsPtrOffset))
		f.a.Load64(scratch, scratch, int32(g*8))
		if f.globalIs64(g) {
			f.a.Store64(scratch, 0, reg)
		} else {
			f.a.Store32(scratch, 0, reg)
		}
	}
	for i, typ := range ft.Params {
		f.Load(uint8(intArgRegs[i]), f.SpillOffset(depth-params+i+1), mtOf(typ) == mtI64)
	}
	for range params {
		s.release(s.pop())
	}
	site := f.a.CallRel32()
	f.relocs = append(f.relocs, f.newCallReloc(site, localIdx, true))
	f.stats.call(callKindRegisterABI)
	f.stats.peep("streaming-direct-call")
	// Capture before reloads. Callee traps unwind directly, so there is no
	// post-call trap check, matching emitRegisterCallVia.
	resultSlot := 0
	wide := false
	if len(ft.Results) == 1 {
		wide = mtOf(ft.Results[0]) == mtI64
		resultSlot = s.slot()
		f.Store(f.SpillOffset(resultSlot), uint8(RAX), wide)
	}
	f.derivePinnedGlobals()
	if len(ft.Results) == 1 {
		s.stack = append(s.stack, s.add(streamNode{kind: shared.ScalarFrame, slot: int32(resultSlot), wide: wide, refs: 1}))
	}
	return nil
}
