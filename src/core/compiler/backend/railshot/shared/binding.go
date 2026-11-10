// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package shared

// ImportBinding describes imported-function lowering. Dynamic selects the
// per-instance dispatch table, while CrossInstance retains the legacy immediate
// binding used by focused backend tests and older low-level callers.
type ImportBinding struct {
	Dynamic       bool
	ImportIndex   uint32
	CrossInstance bool
	CalleeLinMem  uint64
	CalleeEntry   uint64
	EHTransfer    bool // exact staged EH caller/callee transfer; amd64 only
}
