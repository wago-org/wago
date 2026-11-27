// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

type gcHostTempTokens struct {
	count  uint32
	tokens [gcPublicSlotLimit]uint64
	extra  []uint64
}

func (t *gcHostTempTokens) token(index uint32) uint64 {
	if index < gcPublicSlotLimit {
		return t.tokens[index]
	}
	return t.extra[index-gcPublicSlotLimit]
}

func (t *gcHostTempTokens) setToken(index uint32, token uint64) {
	if index < gcPublicSlotLimit {
		t.tokens[index] = token
		return
	}
	extra := index - gcPublicSlotLimit
	if int(extra) == len(t.extra) {
		t.extra = append(t.extra, token)
	} else {
		t.extra[extra] = token
	}
}

func (t *gcHostTempTokens) add(token uint64) {
	if token == 0 {
		return
	}
	t.setToken(t.count, token)
	t.count++
}

func (t *gcHostTempTokens) release(in *Instance) {
	if t == nil || in == nil || in.refStore == nil {
		return
	}
	for t.count != 0 {
		t.count--
		token := t.token(t.count)
		t.setToken(t.count, 0)
		if token != 0 {
			_ = in.refStore.releaseGCRef(in, token)
		}
	}
	if len(t.extra) != 0 {
		t.extra = t.extra[:0]
	}
}
