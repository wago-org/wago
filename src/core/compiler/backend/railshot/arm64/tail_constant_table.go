// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Reuse the ordinary-call proof: one private, unmodified local table whose
// non-null entries all have the same local target and structural signature.
// No host function can intervene. Mutable/exported/imported tables retain the
// ordinary dynamic path, including its bounds/null/signature checks.
func (f *fn) privateMonomorphicTailTarget(typ, table uint32) (int, bool) {
	if !f.opt(optTailSelfLoop) || !f.opt(optRegABI) || profileEnabled || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 ||
		table != 0 || !f.immutableLocalTable || !f.immutableTableTyped || f.monomorphicTarget < 0 || len(f.m.Tables) != 1 || f.m.ImportedFuncCount() != 0 ||
		f.m.Tables[0].Init != nil || f.m.Tables[0].Type.Limits.Addr64 {
		return 0, false
	}
	ft, ok := f.m.TypeFunc(typ)
	if !ok || !sigFitsRegABI(ft) || !sigIsIntOnly(ft) {
		return 0, false
	}
	key, ok := f.m.StructuralTypeKeyChecked(typ)
	if !ok || key != f.immutableTableType {
		return 0, false
	}
	return f.monomorphicTarget, true
}

func (f *fn) constantPrivateTailTarget(typ, table uint32, slot uint64) (int, bool) {
	target, ok := f.privateMonomorphicTailTarget(typ, table)
	// Bound per-site analysis. Large element inventories keep the existing path.
	if !ok || len(f.m.Elements) > 64 || slot >= f.m.Tables[0].Type.Limits.Min {
		return 0, false
	}
	for _, e := range f.m.Elements {
		if e.Mode.Kind != wasm.ElemActive || e.Mode.Table != 0 || e.Kind.Kind != wasm.ElemFuncs {
			continue
		}
		offset, ok := tailTableConstantOffset(e.Mode.Offset)
		if ok && slot >= uint64(offset) && slot-uint64(offset) < uint64(len(e.Kind.Funcs)) {
			// Every active entry is a non-null ref.func to the monomorphic target.
			// Later overlapping segments can only replace it with that same target.
			return target, true
		}
	}
	return 0, false
}

func tailTableConstantOffset(e wasm.Expr) (uint32, bool) {
	if len(e.BodyBytes) == 0 {
		if len(e.Instrs) == 1 && e.Instrs[0].Kind == wasm.InstrI32Const {
			return uint32(e.Instrs[0].I32), true
		}
		return 0, false
	}
	r := wasm.ReaderFrom(e.BodyBytes)
	op, err := r.Byte()
	if err != nil || op != 0x41 {
		return 0, false
	}
	value, err := r.I32()
	if err != nil {
		return 0, false
	}
	end, err := r.Byte()
	return uint32(value), err == nil && end == 0x0b && !r.HasNext()
}
