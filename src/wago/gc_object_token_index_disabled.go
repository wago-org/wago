// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone_no_reference_store || wago_standalone_no_collector || (wago_no_gc && (!linux || !riscv64 || tinygo || wago_target_tinygo || wago_no_exception_handling))

package wago

type gcObjectTokenIndex = struct{}

func (*referenceStore) objectTokensLocked() map[uint64]gcRefTokenEntry {
	return nil
}

func (*referenceStore) insertObjectTokenLocked(gcRefTokenEntry) {
	panic("wago: GC object tokens are disabled")
}

func (*referenceStore) takeObjectTokenRootsLocked() []gcResultRoot {
	return nil
}
