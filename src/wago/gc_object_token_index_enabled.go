// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_reference_store && !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

// gcObjectTokenIndex belongs to referenceStore and is protected by its mutex.
type gcObjectTokenIndex struct {
	byObjectToken map[uint64]gcRefTokenEntry
}

func (s *referenceStore) objectTokensLocked() map[uint64]gcRefTokenEntry {
	return s.byObjectToken
}

func (s *referenceStore) insertObjectTokenLocked(entry gcRefTokenEntry) {
	if s.byObjectToken == nil {
		s.byObjectToken = make(map[uint64]gcRefTokenEntry)
	}
	s.byObjectToken[entry.token] = entry
}

func (s *referenceStore) takeObjectTokenRootsLocked() []gcResultRoot {
	if len(s.byObjectToken) == 0 {
		s.byObjectToken = nil
		return nil
	}
	entries := make([]gcResultRoot, 0, len(s.byObjectToken))
	for _, entry := range s.byObjectToken {
		entries = append(entries, entry.gcResultRoot)
	}
	s.byObjectToken = nil
	return entries
}
