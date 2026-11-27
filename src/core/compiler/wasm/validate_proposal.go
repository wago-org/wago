// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

import "github.com/wago-org/wago/internal/buildfeatures"

// IsCoreAtomicInstructionKind reports whether kind belongs to the core threads
// proposal's linear-memory atomic instruction family. GC struct atomics share
// the binary prefix but deliberately remain behind their own feature boundary.
func IsCoreAtomicInstructionKind(kind InstrKind) bool {
	switch kind {
	case InstrMemoryAtomicNotify, InstrMemoryAtomicWait32, InstrMemoryAtomicWait64,
		InstrAtomicFence,
		InstrI32AtomicLoad, InstrI64AtomicLoad, InstrI32AtomicLoad8U,
		InstrI32AtomicLoad16U, InstrI64AtomicLoad8U, InstrI64AtomicLoad16U,
		InstrI64AtomicLoad32U,
		InstrI32AtomicStore, InstrI64AtomicStore, InstrI32AtomicStore8,
		InstrI32AtomicStore16, InstrI64AtomicStore8, InstrI64AtomicStore16,
		InstrI64AtomicStore32,
		InstrAtomicRmw, InstrAtomicCmpxchg:
		return true
	default:
		return false
	}
}

func (v *funcValidator) proposalStep(in *Instruction) (bool, error) {
	switch in.Kind {
	case InstrI64Add128, InstrI64Sub128, InstrI64MulWideS, InstrI64MulWideU:
		if buildfeatures.WideArithmetic == 0 {
			return true, v.verr(ErrUnsupportedFeature, "wide-arithmetic is disabled in this build")
		}
		count := 2
		if in.Kind == InstrI64Add128 || in.Kind == InstrI64Sub128 {
			count = 4
		}
		for i := 0; i < count; i++ {
			if err := v.popExpect(I64); err != nil {
				return true, err
			}
		}
		v.push(I64)
		v.push(I64)
		return true, nil

	case InstrThrow:
		if buildfeatures.ExceptionHandling == 0 {
			return true, v.verr(ErrUnsupportedFeature, "exception-handling is disabled in this build")
		}
		return true, v.stepThrow(*in)
	case InstrThrowRef:
		if buildfeatures.ExceptionHandling == 0 {
			return true, v.verr(ErrUnsupportedFeature, "exception-handling is disabled in this build")
		}
		if err := v.popExpect(RefVal(AbsRef(HeapExn))); err != nil {
			return true, err
		}
		v.unreachable()
		return true, nil
	case InstrTryTable:
		if buildfeatures.ExceptionHandling == 0 {
			return true, v.verr(ErrUnsupportedFeature, "exception-handling is disabled in this build")
		}
		return true, v.stepTryTable(*in)
	case InstrRefAsNonNull, InstrBrOnNull, InstrBrOnNonNull:
		if buildfeatures.TypedFunctionReferences == 0 {
			return true, v.verr(ErrUnsupportedFeature, "typed-function-references is disabled in this build")
		}
		return true, v.stepTypedReferenceInstruction(in)
	case InstrCallRef, InstrReturnCallRef:
		if buildfeatures.TypedFunctionReferences == 0 {
			return true, v.verr(ErrUnsupportedFeature, "typed-function-references is disabled in this build")
		}
		return true, v.stepCallRef(*in)
	case InstrMemoryInit, InstrDataDrop, InstrMemoryCopy, InstrMemoryFill:
		if buildfeatures.BulkMemoryOperations == 0 {
			return true, v.verr(ErrUnsupportedFeature, "bulk-memory-operations is disabled in this build")
		}
		return true, v.stepBulkMemoryInstruction(in)
	case InstrTableInit, InstrElemDrop, InstrTableCopy:
		if buildfeatures.BulkMemoryOperations == 0 {
			return true, v.verr(ErrUnsupportedFeature, "bulk-memory-operations is disabled in this build")
		}
		if buildfeatures.ReferenceTypes == 0 {
			return true, v.verr(ErrUnsupportedFeature, "reference-types is disabled in this build")
		}
		return true, v.stepBulkTableInstruction(in)
	case InstrTableGrow, InstrTableSize, InstrTableFill:
		if buildfeatures.ReferenceTypes == 0 {
			return true, v.verr(ErrUnsupportedFeature, "reference-types is disabled in this build")
		}
		return true, v.stepReferenceTableInstruction(in)
	case InstrMemoryAtomicNotify, InstrMemoryAtomicWait32, InstrMemoryAtomicWait64, InstrAtomicFence,
		InstrI32AtomicLoad, InstrI64AtomicLoad, InstrI32AtomicLoad8U, InstrI32AtomicLoad16U,
		InstrI64AtomicLoad8U, InstrI64AtomicLoad16U, InstrI64AtomicLoad32U,
		InstrI32AtomicStore, InstrI64AtomicStore, InstrI32AtomicStore8, InstrI32AtomicStore16,
		InstrI64AtomicStore8, InstrI64AtomicStore16, InstrI64AtomicStore32,
		InstrAtomicRmw, InstrAtomicCmpxchg:
		if buildfeatures.Threads == 0 {
			return true, v.verr(ErrUnsupportedFeature, "threads is disabled in this build")
		}
		return true, v.stepAtomic(*in)
	case InstrRefEq, InstrStringConst,
		InstrStructNew, InstrStructNewDefault, InstrStructNewDesc, InstrStructNewDefaultDesc,
		InstrStructGet, InstrStructGetS, InstrStructGetU, InstrStructAtomicGet, InstrStructAtomicGetS, InstrStructAtomicGetU, InstrStructSet,
		InstrArrayNew, InstrArrayNewDefault, InstrArrayNewFixed, InstrArrayNewData, InstrArrayNewElem,
		InstrArrayGet, InstrArrayGetS, InstrArrayGetU, InstrArraySet, InstrArrayLen, InstrArrayFill, InstrArrayCopy, InstrArrayInitData, InstrArrayInitElem,
		InstrRefGetDesc, InstrRefTest, InstrRefCast, InstrRefTestDesc, InstrRefCastDescEq, InstrBrOnCast, InstrBrOnCastFail,
		InstrAnyConvertExtern, InstrExternConvertAny, InstrRefI31, InstrI31GetS, InstrI31GetU:
		if buildfeatures.GC == 0 {
			return true, v.verr(ErrUnsupportedFeature, "gc is disabled in this build")
		}
		return true, v.stepGC(*in)
	}
	if buildfeatures.SIMD != 0 && simdOpEffect(in.Kind).cat != simdNone {
		return true, v.stepSIMD(*in)
	}
	return false, nil
}

func (v *funcValidator) stepCallRef(in Instruction) error {
	ft := v.funcTypeFromTypeIdx(TypeIdx{Index: in.Index})
	if ft == nil {
		return v.verr(ErrUnknownType, "call_ref")
	}
	callee, err := v.pop()
	if err != nil {
		return err
	}
	wantTyped := RefVal(Ref(true, IndexedHeap(TypeIdx{Index: in.Index}), false))
	if !callee.unknown() && !v.subtype(callee.t, wantTyped) {
		// call_ref requires a reference to the selected function type. Nullable
		// typed references remain valid and trap dynamically when null; abstract
		// funcref has no exact callable signature.
		return v.verr(ErrTypeMismatch, "call_ref callee")
	}
	if err := v.popAll(ft.Params); err != nil {
		return err
	}
	if buildfeatures.TailCall != 0 && in.Kind == InstrReturnCallRef {
		if !v.matchValTypes(ft.Results, v.ctrls[0].out) {
			return v.verr(ErrTypeMismatch, "return_call_ref")
		}
		v.unreachable()
	} else {
		v.pushAll(ft.Results)
	}
	return nil
}
