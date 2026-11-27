// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_standalone_no_collector && (!wago_no_gc || !wago_no_exception_handling)

package wago

// A converted collector value can own one public root through its extern slot.
// The root receipt is released after store.mu, like an ordinary GC token.
type rootedExternrefValue interface {
	externrefGCRoot() gcResultRoot
	externrefPublicValue() any
}

type compactExternResultRoot struct {
	root gcResultRoot
}

func claimCompactExternResultRoot(value any) compactExternResultRoot {
	if rooted, ok := value.(rootedExternrefValue); ok {
		return compactExternResultRoot{root: rooted.externrefGCRoot()}
	}
	return compactExternResultRoot{}
}

func (r compactExternResultRoot) appendTo(batch *gcResultReleaseBatch) bool {
	if r.root.owner == nil {
		return false
	}
	batch.gcRoots = append(batch.gcRoots, r.root)
	return true
}

func (r compactExternResultRoot) release() {
	if r.root.owner != nil {
		releaseRootedExternref(r.root)
	}
}

func releaseRootedExternref(root gcResultRoot) {
	unlockNative := lockNativeExecutionForHostAccess()
	domain := root.owner.lockGCCollector()
	var cleanup resourceCleanup
	cleanup.run(func() { releaseGCResultRootMetadata(root) })
	cleanup.run(func() { unlockGCCollector(domain) })
	cleanup.run(unlockNative)
	cleanup.run(root.owner.releaseResourceRoot)
	cleanup.finish()
}
