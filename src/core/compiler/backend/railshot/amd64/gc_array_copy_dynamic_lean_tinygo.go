// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && tinygo && wago_lean

package amd64

// The lean TinyGo runtime keeps checked helper copies for dynamic lengths.
// Other profiles retain the inline overlap-aware numeric copy kernels.
func (f *fn) emitDynamicGCArrayCopy(uint32, uint32) bool { return false }
