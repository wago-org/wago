// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

func (v *funcValidator) stepThrow(in Instruction) error {
	ft, ok := v.tagFuncType(in.Index)
	if !ok {
		return v.verr(ErrUnknownTag, "throw")
	}
	if err := v.popAll(ft.Params); err != nil {
		return err
	}
	v.unreachable()
	return nil
}

func (v *funcValidator) stepTryTable(in Instruction) error {
	ins, outs, err := v.blockSig(in.BlockType())
	if err != nil {
		return err
	}
	for _, c := range in.Catches() {
		if err := v.validateCatchPayload(c); err != nil {
			return err
		}
	}
	if err := v.pushCtrl(ctrlTry, ins, outs); err != nil {
		return err
	}
	for _, child := range in.Body().Instrs {
		if err := v.step(&child); err != nil {
			return err
		}
	}
	_, err = v.popCtrl()
	return err
}

func (v *funcValidator) validateCatchPayload(c Catch) error {
	lt, err := v.label(uint32(c.Label))
	if err != nil {
		return err
	}
	var params []ValType
	if c.Kind == CatchTag || c.Kind == CatchRef {
		if uint(c.Tag) >= uint(len(v.importsOfKind(ExternTag))+len(v.m.Tags)) {
			return v.verr(ErrUnknownTag, "catch")
		}
		ft, ok := v.tagFuncType(uint32(c.Tag))
		if !ok {
			return v.verr(ErrUnknownTag, "catch")
		}
		params = ft.Params
	}
	hasRef := c.Kind == CatchRef || c.Kind == CatchAllRef
	if c.Kind == CatchAll && len(lt) != 0 {
		return v.verr(ErrTypeMismatch, "catch_all label must expect no values")
	}
	payloadLen := len(params)
	if hasRef {
		payloadLen++
	}
	if payloadLen != len(lt) {
		return v.verr(ErrTypeMismatch, "catch payload label mismatch")
	}
	for i := range params {
		if !v.subtype(params[i], lt[i]) {
			return v.verr(ErrTypeMismatch, "catch payload label mismatch")
		}
	}
	if hasRef {
		// Reference catches materialize a non-null exception reference. The target
		// label may widen it to nullable exnref, but not vice versa.
		exn := RefVal(Ref(false, AbsHeap(HeapExn), false))
		if !v.subtype(exn, lt[len(params)]) {
			return v.verr(ErrTypeMismatch, "catch payload label mismatch")
		}
	}
	return nil
}

func (v *moduleValidator) tagFuncType(idx uint32) (*CompType, bool) {
	indexes := v.importsOfKind(ExternTag)
	n := uint32(len(indexes))
	if idx < n {
		im := &v.m.Imports[indexes[idx]]
		ft := v.funcTypeFromTypeIdx(im.Type.TagType().Type)
		return ft, ft != nil
	}
	local := int(idx - n)
	if local < 0 || local >= len(v.m.Tags) {
		return nil, false
	}
	ft := v.funcTypeFromTypeIdx(v.m.Tags[local].Type)
	return ft, ft != nil
}
