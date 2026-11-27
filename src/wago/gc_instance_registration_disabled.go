// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone_no_collector || (wago_no_gc && (!linux || !riscv64 || tinygo || wago_target_tinygo || wago_no_exception_handling))

package wago

type gcInstanceRegistration struct{}

func newGCInstanceRegistration(*gcStoreDomain, *gcInvocationDomainSet, bool) gcInstanceRegistration {
	return gcInstanceRegistration{}
}

func (*gcInstanceRegistration) gcRegistration() *gcInvocationRegistration {
	return nil
}
