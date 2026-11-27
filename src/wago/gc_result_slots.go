// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

// gcResultSlots owns collector global slots and their public token identities.
// Callers serialize allocation, publication, and release under gcPublicState.mu.
type gcResultSlots struct {
	resultTokenCount     uint32
	resultRootsMade      uint32
	resultTokens         [gcPublicSlotLimit]uint64
	resultTokensExtra    []uint64
	resultRootSlots      [gcPublicSlotLimit]uint32
	resultRootSlotsExtra []uint32
}

func (s *gcResultSlots) resultToken(index uint32) uint64 {
	if index < gcPublicSlotLimit {
		return s.resultTokens[index]
	}
	return s.resultTokensExtra[index-gcPublicSlotLimit]
}

func (s *gcResultSlots) setResultToken(index uint32, token uint64) {
	if index < gcPublicSlotLimit {
		s.resultTokens[index] = token
		return
	}
	s.resultTokensExtra[index-gcPublicSlotLimit] = token
}

func (s *gcResultSlots) resultRootSlot(index uint32) uint32 {
	if index < gcPublicSlotLimit {
		return s.resultRootSlots[index]
	}
	return s.resultRootSlotsExtra[index-gcPublicSlotLimit]
}

func (s *gcResultSlots) appendResultRootSlot(slot uint32) uint32 {
	index := s.resultRootsMade
	if index < gcPublicSlotLimit {
		s.resultRootSlots[index] = slot
	} else {
		s.resultRootSlotsExtra = append(s.resultRootSlotsExtra, slot)
		s.resultTokensExtra = append(s.resultTokensExtra, 0)
	}
	s.resultRootsMade++
	return index
}

func (s *gcResultSlots) nextResultSlot() uint32 {
	for index := uint32(0); index < s.resultRootsMade; index++ {
		if s.resultToken(index) == 0 {
			return index
		}
	}
	return s.resultRootsMade
}

func (s *gcResultSlots) matches(index uint32, token uint64, slot uint32) bool {
	return s != nil && index < s.resultRootsMade && s.resultToken(index) == token && s.resultRootSlot(index) == slot
}

func (s *gcResultSlots) root(collector *gc.Collector, ref gc.Ref, index uint32) (uint32, error) {
	if index == s.resultRootsMade {
		slot, err := collector.NewCheckedClassifiedGlobalSlot(ref, gc.RootPublicToken)
		if err != nil {
			return 0, err
		}
		s.appendResultRootSlot(slot)
		return slot, nil
	}
	slot := s.resultRootSlot(index)
	return slot, collector.SetGlobalSlot(slot, ref)
}

func (s *gcResultSlots) publish(index uint32, token uint64) {
	s.setResultToken(index, token)
	s.resultTokenCount++
}

func (s *gcResultSlots) clear(index uint32) {
	s.setResultToken(index, 0)
	if s.resultTokenCount != 0 {
		s.resultTokenCount--
	}
}
