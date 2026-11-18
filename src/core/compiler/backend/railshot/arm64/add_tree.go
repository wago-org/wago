// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var addTreeEnabled = os.Getenv("WAGO_ARM64_ASSOC_ADD") != "0"

type addTree struct {
	leaves [32]*elem
	count  int
}

// collectAddTree only admits concrete, nontrapping leaves and reports the
// register requirement of the balanced expression. Captured owned values
// remain separate leaves even if they originated from the same local.
func (f *fn) collectAddTree(node *elem, typ machineType, tree *addTree) int {
	if node.st.typ != typ {
		return 0
	}
	if node.elemKind() == ekValue {
		switch node.st.kind {
		case stConst, stLocalRef, stLocalReg, stGlobReg, stReg, stSlot:
			if tree.count == len(tree.leaves) {
				return 0
			}
			tree.leaves[tree.count] = node
			tree.count++
			return 1
		}
		return 0
	}
	if !node.isDeferred() || node.deferredOp() != opAdd {
		return 0
	}
	left := f.collectAddTree(f.s.arg0(node), typ, tree)
	if left == 0 {
		return 0
	}
	right := f.collectAddTree(f.s.arg1(node), typ, tree)
	if right == 0 {
		return 0
	}
	if left == right {
		return left + 1
	}
	return max(left, right)
}

func (f *fn) tryAddTree(node *elem, dest Reg) Reg {
	if !addTreeEnabled || node.deferredDepth() < 2 || f.shiftedALUAddress || len(f.customInstructions) != 0 {
		return regNone
	}
	var tree addTree
	need := f.collectAddTree(node, node.st.typ, &tree)
	if need == 0 {
		return regNone
	}
	if result := f.tryRepeatedSourceAdd(node, dest, tree.leaves[:tree.count]); result != regNone {
		return result
	}
	if need < 4 {
		return regNone
	}
	// The balanced tree uses at least four registers. Keep a single
	// accumulator, staging the result if dest still holds another input.
	result := dest
	first := tree.leaves[0]
	for _, leaf := range tree.leaves[1:tree.count] {
		switch leaf.st.kind {
		case stReg, stLocalReg, stGlobReg:
			if leaf.st.reg == dest {
				result = regNone
			}
		}
	}
	if result == regNone {
		result = f.allocReg(0)
	}
	pins := f.pinned
	f.pinned = f.pinned.add(result)
	f.condenseInto(first, result)
	w := !node.st.typ.is64()
	start := f.a.Len()
	for _, leaf := range tree.leaves[1:tree.count] {
		f.applyALU(aluTable[opAdd], result, leaf, !w)
	}
	if w && f.a.Len() == start {
		f.a.MovReg32(result, result)
	}
	f.pinned = pins
	if dest != regNone && result != dest {
		f.a.MovReg64(dest, result)
		f.release(result)
		result = dest
	}
	f.stats.peep("high-pressure-add-tree")
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	return result
}

func (f *fn) tryRepeatedSourceAdd(node *elem, dest Reg, leaves []*elem) Reg {
	var source *elem
	count, constant := 0, int64(0)
	for _, leaf := range leaves {
		switch leaf.st.kind {
		case stConst:
			constant += leaf.st.cval
		case stLocalReg, stGlobReg, stLocalRef:
			if source != nil && (source.st.kind != leaf.st.kind || source.st.idx != leaf.st.idx) {
				return regNone
			}
			source = leaf
			count++
		default:
			return regNone
		}
	}
	var shift uint8
	switch count {
	case 2, 3:
		shift = uint8(count - 2)
	case 5:
		shift = 2
	case 9:
		shift = 3
	default:
		return regNone
	}
	w := !node.st.typ.is64()
	if w {
		constant = int64(int32(constant))
	}
	if constant != 0 && !fitsAddSubImm12(constant) && !fitsShiftedAddSubImmediate(constant) {
		return regNone
	}
	src, owned := f.materializeRead(source)
	result := dest
	if result == regNone {
		result = src
		if !owned {
			result = f.allocReg(maskOf(src))
		}
	}
	f.a.AddShifted(result, src, src, shift, w)
	if owned && result != src {
		f.release(src)
	}
	if constant != 0 {
		f.addDisp(result, result, int32(constant), !w)
	}
	f.stats.peep("repeated-source-add-tree")
	f.consumeBlockBelow(node)
	f.occupy(node, result)
	return result
}
