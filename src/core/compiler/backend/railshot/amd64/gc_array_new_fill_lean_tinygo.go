// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && tinygo && wago_lean

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Keep separate checked constructors and fills in the lean TinyGo profile.
// Other profiles retain the lookahead fusion and its local-storage proof.
type gcArrayNewFillPlan struct{}

func (f *fn) matchGCArrayNewFill(*wasm.Reader, uint32, wasm.FieldType) (gcArrayNewFillPlan, bool) {
	return gcArrayNewFillPlan{}, false
}
func (gcArrayNewFillPlan) replaceInitializer(*fn)   {}
func (gcArrayNewFillPlan) finish(*fn, *wasm.Reader) {}
