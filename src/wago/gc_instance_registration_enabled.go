// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

type gcInstanceRegistration struct {
	registration gcInvocationRegistration
}

func newGCInstanceRegistration(domain *gcStoreDomain, imports *gcInvocationDomainSet, dynamic bool) gcInstanceRegistration {
	return gcInstanceRegistration{registration: gcInvocationRegistration{domain: domain, imports: imports, dynamic: dynamic}}
}

func (s *gcInstanceRegistration) gcRegistration() *gcInvocationRegistration {
	return &s.registration
}
