// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_no_memory64

package wasm

type memargIndexedWidths []uint64

func moduleMemargWidths(m *Module) memargWidths {
	if m == nil {
		return memargWidths{multiMemory: true}
	}
	seen, mixed := false, false
	addr64 := false
	count := 0
	for i := range m.Imports {
		if m.Imports[i].Type.Kind != ExternMem {
			continue
		}
		got := m.Imports[i].Type.MemType().Limits.Addr64
		mixed = mixed || seen && got != addr64
		seen, addr64 = true, got
		count++
	}
	for i := range m.Memories {
		got := m.Memories[i].Limits.Addr64
		mixed = mixed || seen && got != addr64
		seen, addr64 = true, got
		count++
	}
	if !seen {
		return memargWidths{multiMemory: true}
	}
	// Preserve the allocation-free fixed-width hot path for the overwhelmingly
	// common single-memory and uniform-width multi-memory modules.
	if !mixed {
		return memargWidths{multiMemory: true, fixed64: addr64}
	}
	indexed64 := make([]uint64, (count+63)/64)
	index := 0
	for i := range m.Imports {
		if m.Imports[i].Type.Kind != ExternMem {
			continue
		}
		if m.Imports[i].Type.MemType().Limits.Addr64 {
			indexed64[index>>6] |= uint64(1) << (uint(index) & 63)
		}
		index++
	}
	for i := range m.Memories {
		if m.Memories[i].Limits.Addr64 {
			indexed64[index>>6] |= uint64(1) << (uint(index) & 63)
		}
		index++
	}
	return memargWidths{multiMemory: true, memargIndexedWidths: indexed64}
}

func (w memargWidths) offset64(memoryIndex uint32) bool {
	word := memoryIndex >> 6
	if uint(word) < uint(len(w.memargIndexedWidths)) {
		return w.memargIndexedWidths[word]&(uint64(1)<<(memoryIndex&63)) != 0
	}
	return w.fixed64
}
