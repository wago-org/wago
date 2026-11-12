// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import "fmt"

// This private binding is only constructed for a registered native custom
// instruction. Its host fallback traps; it never interprets erased values as
// numeric cells or collector/reference tokens.
type nativeCustomInstructionBinding struct {
	fn  slotHostFunc
	sig FuncSig
}

func (b nativeCustomInstructionBinding) validate(sig FuncSig) error {
	if b.fn == nil || !funcSigEqual(b.sig, sig) {
		return fmt.Errorf("native custom instruction signature mismatch")
	}
	return nil
}
