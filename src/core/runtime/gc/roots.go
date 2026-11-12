// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package gc

type RootSlot interface {
	GetRef() Ref
	SetRef(Ref)
}
type RootSet interface{ RangeRoots(func(RootSlot) bool) }
type Root Ref

func (r *Root) GetRef() Ref      { return Ref(*r) }
func (r *Root) SetRef(value Ref) { *r = Root(value) }

type Slots []RootSlot

func (s Slots) RangeRoots(visit func(RootSlot) bool) {
	for _, slot := range s {
		if !visit(slot) {
			return
		}
	}
}

type RefSliceRoots []Ref

func (s RefSliceRoots) RangeRoots(visit func(RootSlot) bool) {
	for i := range s {
		if !visit((*Root)(&s[i])) {
			return
		}
	}
}

type EmptyRoots struct{}

func (EmptyRoots) RangeRoots(func(RootSlot) bool) {}
