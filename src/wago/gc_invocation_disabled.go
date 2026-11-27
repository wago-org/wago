// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone_no_reference_store || wago_standalone_no_collector || (wago_no_gc && (!linux || !riscv64 || tinygo || wago_target_tinygo || wago_no_exception_handling))

package wago

import "context"

type gcInvocationLease struct{}

func (gcInvocationLease) isAcquired() bool { return false }

func (gcInvocationLease) isDynamic() bool { return false }

func (gcInvocationLease) leasedTopology() *gcDomainTopology { return nil }

func (gcInvocationLease) release(*resourceCleanup) {}

func (gcInvocationLease) unlock() {}

func (*Instance) lockGCInvocation(invocationID) gcInvocationLease { return gcInvocationLease{} }

func (*Instance) lockGCInvocationContext(ctx context.Context, _ invocationID) (gcInvocationLease, error) {
	if ctx != nil {
		return gcInvocationLease{}, ctx.Err()
	}
	return gcInvocationLease{}, nil
}

func (*Instance) ownsGCInvocation(invocationID) bool { return false }

func hostGCLeaseOwner(*Instance, *Instance) *Instance { return nil }

type gcInvocationSuspension struct{}

func (*Instance) suspendGCInvocation(invocationID) gcInvocationSuspension {
	return gcInvocationSuspension{}
}

func (*gcInvocationSuspension) resume() {}
