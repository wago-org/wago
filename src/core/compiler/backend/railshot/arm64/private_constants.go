// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var privateConstantsEnabled = os.Getenv("WAGO_ARM64_NO_PRIVATE_CONSTANTS") != "1"

type privateLocalConstant struct {
	bits  int64
	local uint32
	typ   machineType
}

// Initializers retain source offsets so the proof can be consumed without
// changing the native global read, arithmetic, or local register carrier.
type privateConstantInitializer struct {
	offset        int
	local, global uint32
	value         storage
}

// Bound the inventory and byte walk. Imported and exported globals remain
// observable cells. A private defined literal can change only through a module
// instruction; scan the entire sole function, including unreachable code.
func (f *fn) provePrivateConstants() (globals, stableLocals uint64) {
	m := f.m
	if !privateConstantsEnabled || !f.opt(optValueFacts) || m == nil || len(m.Code) != 1 || m.ImportedFuncCount() != 0 ||
		m.GlobalCount() == 0 || m.GlobalCount() > 64 || len(m.Exports) > 128 || len(f.customInstructions) != 0 ||
		len(m.Code[0].BodyBytes) == 0 || len(m.Code[0].BodyBytes) > 16<<10 {
		return 0, 0
	}
	base := m.ImportedGlobalCount()
	for i, g := range m.Globals {
		if _, ok := privateGlobalLiteral(g); ok {
			globals |= uint64(1) << uint(base+i)
		}
	}
	for _, e := range m.Exports {
		if e.Index.Kind == wasm.ExternGlobal {
			globals &^= uint64(1) << e.Index.Index
		}
	}
	if globals == 0 {
		return 0, 0
	}
	var writes, loopWrites uint64
	type sourceOp struct {
		op    byte
		index uint32
		bits  int64
	}
	var previous [3]sourceOp
	var loops [256]bool
	depth, loopDepth := 0, 0
	r := wasm.ReaderFrom(m.Code[0].BodyBytes)
	for r.HasNext() {
		op, err := r.Byte()
		// Prefix extensions retain their ordinary semantics and analysis. In
		// particular, future shared-global writes must not enter this proof.
		if err != nil || op >= 0xfb {
			return 0, 0
		}
		var bits int64
		var index uint32
		switch {
		case op == 0x41:
			v, e := r.I32()
			if e != nil {
				return 0, 0
			}
			bits = int64(v)
		case op == 0x42:
			v, e := r.I64()
			if e != nil {
				return 0, 0
			}
			bits = v
		case op >= 0x20 && op <= 0x24:
			index, err = r.U32()
			if err != nil {
				return 0, 0
			}
		case op >= 0x45 && op <= 0xc4:
			// Numeric scalar operations have no immediates.
		default:
			var imm wasm.InstructionImmediate
			if err := f.classifier.ClassifyInto(&r, op, &imm); err != nil {
				return 0, 0
			}
			index = imm.Index
		}
		if (op == 0x21 || op == 0x22) && depth == 0 && int(f.privateInitializerN) < len(f.privateInitializers) && previous[0].op == 0x23 {
			if global, ok := privateGlobalLiteralAt(m, previous[0].index, globals); ok {
				integer32 := global.typ == mtI32 && previous[1].op == 0x41 && (previous[2].op == 0x6a || previous[2].op == 0x6b)
				integer64 := global.typ == mtI64 && previous[1].op == 0x42 && (previous[2].op == 0x7c || previous[2].op == 0x7d)
				if integer32 || integer64 {
					if previous[2].op == 0x6b || previous[2].op == 0x7d {
						global.cval = int64(uint64(global.cval) - uint64(previous[1].bits))
					} else {
						global.cval = int64(uint64(global.cval) + uint64(previous[1].bits))
					}
					if integer32 {
						global.cval = int64(int32(global.cval))
					}
					f.privateInitializers[f.privateInitializerN] = privateConstantInitializer{offset: r.Offset(), local: index, global: previous[0].index, value: global}
					f.privateInitializerN++
				}
			}
		}
		previous[0], previous[1], previous[2] = previous[1], previous[2], sourceOp{op: op, index: index, bits: bits}
		switch op {
		case 0x02, 0x03, 0x04, 0x06, 0x1f:
			if depth == len(loops) {
				return 0, 0
			}
			loops[depth] = op == 0x03
			depth++
			if op == 0x03 {
				loopDepth++
			}
		case 0x0b:
			if depth > 0 {
				depth--
				if loops[depth] {
					loopDepth--
				}
			}
		case 0x24:
			globals &^= uint64(1) << index
		case 0x21, 0x22:
			bit := uint64(1) << index
			if loopDepth != 0 {
				loopWrites |= bit
			}
			writes |= bit
		}
	}
	return globals, writes &^ loopWrites
}

func privateGlobalLiteral(g wasm.Global) (storage, bool) {
	typ := mtI32
	if wasm.EqualValType(wasm.GlobalValueType(g.Type), wasm.I64) {
		typ = mtI64
	} else if !wasm.EqualValType(wasm.GlobalValueType(g.Type), wasm.I32) {
		return storage{}, false
	}
	var value int64
	if len(g.Init.BodyBytes) != 0 {
		r := wasm.ReaderFrom(g.Init.BodyBytes)
		op, err := r.Byte()
		if err != nil {
			return storage{}, false
		}
		if typ == mtI32 && op == 0x41 {
			v, e := r.I32()
			err = e
			value = int64(v)
		} else if typ == mtI64 && op == 0x42 {
			value, err = r.I64()
		} else {
			return storage{}, false
		}
		if err != nil {
			return storage{}, false
		}
		end, e := r.Byte()
		if e != nil || end != 0x0b || r.HasNext() {
			return storage{}, false
		}
	} else {
		if len(g.Init.Instrs) != 1 {
			return storage{}, false
		}
		i := g.Init.Instrs[0]
		if typ == mtI32 && i.Kind == wasm.InstrI32Const {
			value = int64(i.I32)
		} else if typ == mtI64 && i.Kind == wasm.InstrI64Const {
			value = i.I64
		} else {
			return storage{}, false
		}
	}
	return storage{kind: stConst, typ: typ, cval: value}, true
}

func privateGlobalLiteralAt(m *wasm.Module, index uint32, mask uint64) (storage, bool) {
	if index >= 64 || mask&(uint64(1)<<index) == 0 {
		return storage{}, false
	}
	return privateGlobalLiteral(m.Globals[int(index)-m.ImportedGlobalCount()])
}

func (f *fn) rememberPrivateLocalConstant(x int, reader *wasm.Reader) {
	if reader == nil || f.privateGlobalConsts == 0 || f.localBase != 0 || f.inlineForwardN != 0 || f.unreachable || len(f.ctrl) != 1 || x < f.nParams || x >= 64 || f.privateStableLocals&(uint64(1)<<uint(x)) == 0 || int(f.privateLocalConstN) == len(f.privateLocalConsts) {
		return
	}
	for i := 0; i < int(f.privateInitializerN); i++ {
		init := f.privateInitializers[i]
		if init.local != uint32(x) || init.offset != reader.Offset() || f.privateGlobalConsts&(uint64(1)<<init.global) == 0 {
			continue
		}
		f.privateLocalConsts[f.privateLocalConstN] = privateLocalConstant{local: uint32(x), typ: init.value.typ, bits: init.value.cval}
		f.privateLocalConstN++
		f.stats.peep("private-local-constant-fact")
		return
	}
}

func (f *fn) privateLocalConstant(x uint32) (storage, bool) {
	for i := 0; i < int(f.privateLocalConstN); i++ {
		c := f.privateLocalConsts[i]
		if c.local == x {
			return storage{kind: stConst, typ: c.typ, cval: c.bits}, true
		}
	}
	return storage{}, false
}

// The proof excludes every local written inside a loop. Outside loops, any
// definition ends the fact for later source reads, including definitions in
// another control arm. Entry-level constant assignments can establish it anew.
func (f *fn) invalidatePrivateLocalConstant(local int) {
	for i := 0; i < int(f.privateLocalConstN); i++ {
		if f.privateLocalConsts[i].local == uint32(local) {
			f.privateLocalConstN--
			f.privateLocalConsts[i] = f.privateLocalConsts[f.privateLocalConstN]
			return
		}
	}
}
