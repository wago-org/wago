// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

import "github.com/wago-org/wago/internal/buildfeatures"

func decodeExternType(r *reader) (ExternType, error) {
	k, err := r.byte()
	if err != nil {
		return ExternType{}, err
	}
	return decodeExternTypeKind(r, ExternKind(k))
}

func decodeExternTypeKind(r *reader, kind ExternKind) (ExternType, error) {
	switch kind {
	case ExternFunc:
		t, err := decodeTypeIdx(r)
		return NewFuncExternType(t), err
	case ExternTable:
		t, err := decodeTableType(r)
		return NewTableExternType(t), err
	case ExternMem:
		t, err := decodeMemType(r)
		return NewMemExternType(t), err
	case ExternGlobal:
		t, err := decodeGlobalType(r)
		return NewGlobalExternType(t), err
	case ExternTag:
		if buildfeatures.ExceptionHandling == 0 {
			return NewTagExternType(TagType{}), disabledInstruction("exception-handling", r.off())
		}
		t, err := decodeTagType(r)
		return NewTagExternType(t), err
	default:
		return ExternType{Kind: kind}, &DecodeError{Code: ErrInvalidImport, Offset: r.off() - 1}
	}
}

func decodeImports(r *reader) ([]Import, bool, error) {
	n, err := r.u32()
	if err != nil {
		return nil, false, err
	}
	if err := reserveDecodedSlice[Import](r, n); err != nil {
		return nil, false, err
	}
	capHint := boundedVecCap(n, r.left())
	imports := make([]Import, 0, capHint)
	compact := false
	// The section vector counts encoded groups, not expanded imports.
	for group := uint32(0); group < n; group++ {
		mod, err := r.name()
		if err != nil {
			return nil, compact, err
		}
		name, err := r.name()
		if err != nil {
			return nil, compact, err
		}
		kindByte, err := r.byte()
		if err != nil {
			return nil, compact, err
		}
		if name != "" || (kindByte != 0x7e && kindByte != 0x7f) {
			et, err := decodeExternTypeKind(r, ExternKind(kindByte))
			if err != nil {
				return nil, compact, err
			}
			imports = append(imports, Import{Module: mod, Name: name, Type: et})
			continue
		}

		if buildfeatures.CompactImports == 0 {
			return nil, compact, disabledInstruction("compact-imports", r.off()-1)
		}
		compact = true
		imports, err = decodeCompactImportGroup(r, imports, mod, kindByte)
		if err != nil {
			return nil, compact, err
		}
	}
	return imports, compact, nil
}

func decodeCompactImportGroup(r *reader, imports []Import, mod string, kindByte byte) ([]Import, error) {
	var sharedType ExternType
	if kindByte == 0x7e {
		k, err := r.byte()
		if err != nil {
			return nil, err
		}
		sharedType, err = decodeExternTypeKind(r, ExternKind(k))
		if err != nil {
			return nil, err
		}
	}
	count, err := r.u32()
	if err != nil {
		return nil, err
	}
	// Every item has at least a one-byte name length. Bound malformed
	// counts by input bytes, and charge expansion before allocating metadata.
	if uint64(count) > uint64(r.left()) {
		return nil, &DecodeError{Code: ErrIndexOutOfBounds, Offset: r.off()}
	}
	if err := reserveDecodedSlice[Import](r, count); err != nil {
		return nil, err
	}
	for i := uint32(0); i < count; i++ {
		field, err := r.name()
		if err != nil {
			return nil, err
		}
		et := sharedType
		if kindByte == 0x7f {
			k, err := r.byte()
			if err != nil {
				return nil, err
			}
			et, err = decodeExternTypeKind(r, ExternKind(k))
			if err != nil {
				return nil, err
			}
		}
		imports = append(imports, Import{Module: mod, Name: field, Type: et})
	}
	return imports, nil
}
