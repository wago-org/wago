// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64 && !wago_no_memory64

package arm64

// The module's declared memory types are immutable for this compilation.
type memory64WidthCache struct {
	known, address64 bool
}

func (f *fn) memoryAddr64(memoryIndex uint32) bool {
	if memoryIndex == 0 {
		if !f.memory0Width.known {
			mt, ok := f.memoryType(0)
			f.memory0Width.address64 = ok && mt.Limits.Addr64
			f.memory0Width.known = true
		}
		return f.memory0Width.address64
	}
	mt, ok := f.memoryType(memoryIndex)
	return ok && mt.Limits.Addr64
}
