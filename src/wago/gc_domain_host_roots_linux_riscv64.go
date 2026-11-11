// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64

package wago

// Only RV64 root sets enumerate older same-domain host activations. Reuse cold
// overflow storage without adding unused state bytes on the other targets.
type gcDomainHostRootScratch struct {
	domainHostOwnersExtra []gcSuspendedRootOwner
}

func (s *gcDomainHostRootScratch) suspendedOwnerScratch() []gcSuspendedRootOwner {
	return s.domainHostOwnersExtra[:0]
}

func (s *gcDomainHostRootScratch) retainSuspendedOwnerScratch(owners []gcSuspendedRootOwner) {
	s.domainHostOwnersExtra = owners[:0]
}
