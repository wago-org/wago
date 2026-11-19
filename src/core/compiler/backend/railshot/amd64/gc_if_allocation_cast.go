// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

const maxGCIfAllocationCastLookahead = 256

// gcIfReferenceResult rejects numeric machine-I64 results before the bounded
// instruction scan. blockType has already validated this signature; decoding a
// copy retains the full WebAssembly value type without allocating storage.
func (f *fn) gcIfReferenceResult(signature wasm.Reader) bool {
	block, err := signature.BlockType()
	if err != nil {
		return false
	}
	switch block.Kind {
	case wasm.BlockVal:
		return block.Val.Kind() == wasm.ValRef
	case wasm.BlockTypeIndex:
		ft, ok := f.m.TypeFunc(block.Type.Index)
		return ok && len(ft.Params) == 0 && len(ft.Results) == 1 && ft.Results[0].Kind() == wasm.ValRef
	default:
		return false
	}
}

// provedGCIfAllocationCast recognizes an if whose two fallthrough arms end in
// struct constructors, followed immediately by a non-nullable upcast. Nested
// control and branches are rejected so every path reaching the join has one of
// the two proved constructor types. The fixed byte bound limits compile work.
func (f *fn) provedGCIfAllocationCast(reader *wasm.Reader) bool {
	r := *reader
	start := r.Offset()
	var source [2]uint32
	for arm := range 2 {
		constructor := false
		for {
			if r.Offset()-start >= maxGCIfAllocationCastLookahead {
				return false
			}
			op, err := r.Byte()
			if err != nil {
				return false
			}
			if op == 0x05 {
				if arm != 0 || !constructor {
					return false
				}
				break
			}
			if op == 0x0b {
				if arm != 1 || !constructor {
					return false
				}
				break
			}
			// A nested construct can transfer an arbitrary declared result to
			// this if. Direct label branches can bypass the terminal constructor.
			if op == 0x02 || op == 0x03 || op == 0x04 || op == 0x1f {
				return false
			}
			var imm wasm.InstructionImmediate
			if err := f.classifier.ClassifyInto(&r, op, &imm); err != nil {
				return false
			}
			switch imm.Kind {
			case wasm.InstrBr, wasm.InstrBrIf, wasm.InstrBrTable,
				wasm.InstrBrOnNull, wasm.InstrBrOnNonNull,
				wasm.InstrBrOnCast, wasm.InstrBrOnCastFail:
				return false
			case wasm.InstrStructNew, wasm.InstrStructNewDefault:
				constructor = true
				source[arm] = imm.Index
			default:
				constructor = false
			}
		}
	}
	if r.Offset()-start >= maxGCIfAllocationCastLookahead {
		return false
	}
	op, err := r.Byte()
	if err != nil || op != 0xfb {
		return false
	}
	sub, err := r.U32()
	if err != nil || sub != 22 {
		return false
	}
	target, exact, err := readRefHeapTypeImmediate(&r)
	if err != nil || target < 0 || exact || r.Offset()-start > maxGCIfAllocationCastLookahead {
		return false
	}
	return f.constructorSubtypeProof(source[0], uint32(target)) &&
		f.constructorSubtypeProof(source[1], uint32(target))
}

func consumeProvedGCIfAllocationCast(reader *wasm.Reader) bool {
	r := *reader
	op, err := r.Byte()
	if err != nil || op != 0xfb {
		return false
	}
	sub, err := r.U32()
	if err != nil || sub != 22 {
		return false
	}
	target, exact, err := readRefHeapTypeImmediate(&r)
	if err != nil || target < 0 || exact {
		return false
	}
	*reader = r
	return true
}
