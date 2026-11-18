// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var teeAddressCertEnabled = os.Getenv("WAGO_ARM64_NO_TEE_ADDRESS_CERT") != "1"

// An owned local.tee result still names the assigned local when its canonical
// frame store and physical carrier are unchanged. Reuse the bounded native
// write/alias proof, and separately reject semantic writes that emitted no word.
func (f *fn) teeAddressLocal(e *elem) (uint32, bool) {
	if !teeAddressCertEnabled || !f.boundsFacts || !f.frameForwardLocalLive || f.localBase != 0 || f.inlineForwardN != 0 || e == nil || e.elemKind() != ekValue || e.st.kind != stReg {
		return 0, false
	}
	local := f.frameForwardLocal
	if local < 0 || local >= f.nLocals || f.localType[local] != e.st.typ || f.localOff(local) != f.frameForwardOff {
		return 0, false
	}
	reg, ok := f.frameForwardSource(f.frameForwardOff)
	if !ok || reg != e.st.reg {
		return 0, false
	}
	f.stats.peep("tee-address-certificate")
	return uint32(local), true
}
