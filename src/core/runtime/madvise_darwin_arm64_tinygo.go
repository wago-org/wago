// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build darwin && arm64 && tinygo

package runtime

// TinyGo's Darwin syscall surface does not expose madvise. Clearing preserves
// the zero-on-reuse contract; it merely forgoes returning those pages to the OS.
func madviseDontNeed(b []byte) error {
	clear(b)
	return nil
}
