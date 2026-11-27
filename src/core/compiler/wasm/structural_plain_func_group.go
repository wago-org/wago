// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

import (
	"crypto/sha256"
	"encoding/binary"
)

// A function-only group with no indexed references uses the same encoding as
// the general graph. It needs neither dependency traversal nor member caches.
func (m *Module) writeStructuralPlainFuncGroup(typeIdx uint32, mix func(byte), groupDigests *map[int][32]byte, compactKey bool) bool {
	directory := m.typeIndex()
	if uint64(typeIdx) >= uint64(len(directory.flat)) || directory.flat[typeIdx].st.Comp.Kind != CompFunc {
		return false
	}
	group := directory.flat[typeIdx].recGroup
	members := m.Types[group].SubTypes
	member := typeIdx - uint32(directory.bases[group])

	var groupDigest [32]byte
	var cached bool
	if compactKey && groupDigests != nil {
		groupDigest, cached = (*groupDigests)[group]
	}
	var encoded []byte
	if !cached {
		var ok bool
		encoded, ok = encodeStructuralPlainFuncGroup(members)
		if !ok {
			return false
		}
	}
	if compactKey {
		if !cached {
			groupDigest = sha256.Sum256(encoded)
			if groupDigests != nil {
				if *groupDigests == nil {
					*groupDigests = make(map[int][32]byte)
				}
				(*groupDigests)[group] = groupDigest
			}
		}
		for _, b := range structuralGroupMemberDigest(groupDigest, len(members), member) {
			mix(b)
		}
		return true
	}

	prefix := [9]byte{0xf3}
	binary.LittleEndian.PutUint32(prefix[1:5], uint32(len(members)))
	binary.LittleEndian.PutUint32(prefix[5:9], member)
	for _, b := range prefix {
		mix(b)
	}
	for _, b := range encoded {
		mix(b)
	}
	return true
}

func encodeStructuralPlainFuncGroup(members []SubType) ([]byte, bool) {
	encoded := make([]byte, 0, 128)
	var budget structuralEncodingBudget
	if !budget.appendU32(&encoded, uint32(len(members))) {
		return nil, false
	}
	for i := range members {
		st := &members[i]
		if st.Comp.Kind != CompFunc || len(st.Supers) != 0 || st.Metadata.Describes.Present() || st.Metadata.Descriptor.Present() {
			return nil, false
		}
		final := byte(0)
		if st.Final {
			final = 1
		}
		if !budget.appendByte(&encoded, 0xf1) || !budget.appendByte(&encoded, final) ||
			!budget.appendU32(&encoded, 0) || !budget.appendByte(&encoded, 0) ||
			!budget.appendByte(&encoded, 0) || !budget.appendByte(&encoded, byte(CompFunc)) {
			return nil, false
		}
		for _, values := range [2][]ValType{st.Comp.Params, st.Comp.Results} {
			if !budget.appendU32(&encoded, uint32(len(values))) {
				return nil, false
			}
			for _, value := range values {
				if !budget.appendByte(&encoded, byte(value.Kind())) {
					return nil, false
				}
				switch value.Kind() {
				case ValNum:
					if !budget.appendByte(&encoded, byte(value.Num())) {
						return nil, false
					}
				case ValVec, ValBot:
				case ValRef:
					ref := value.Ref()
					heap := ref.Heap()
					if heap.Kind() != HeapAbs {
						return nil, false
					}
					nullable, exact := byte(0), byte(0)
					if ref.Nullable() {
						nullable = 1
					}
					if ref.Exact() {
						exact = 1
					}
					if !budget.appendByte(&encoded, nullable) || !budget.appendByte(&encoded, exact) ||
						!budget.appendByte(&encoded, byte(HeapAbs)) || !budget.appendByte(&encoded, byte(heap.Abs())) {
						return nil, false
					}
				default:
					return nil, false
				}
			}
		}
	}
	return encoded, true
}
