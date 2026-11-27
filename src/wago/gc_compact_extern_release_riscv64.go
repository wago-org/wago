// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && (!wago_no_reference_types || !wago_no_gc || !wago_no_exception_handling)

package wago

// A host resource can issue a collector value after every importer closes.
// Its token holds a domain claim directly and releases outside store.mu.
type directRootedExternrefValue interface {
	externrefPublicValue() any
	releaseExternrefRoot()
}

type compactExternRoot struct {
	result compactExternResultRoot
	direct directRootedExternrefValue
}

func claimCompactExternRoot(value any) compactExternRoot {
	if direct, ok := value.(directRootedExternrefValue); ok {
		return compactExternRoot{direct: direct}
	}
	return compactExternRoot{result: claimCompactExternResultRoot(value)}
}

func (r compactExternRoot) release() {
	if r.direct != nil {
		r.direct.releaseExternrefRoot()
		return
	}
	r.result.release()
}

func publicExternrefValue(value any) any {
	if wrapped, ok := value.(interface{ externrefPublicValue() any }); ok {
		return wrapped.externrefPublicValue()
	}
	return value
}

type compactExternReleaseBatch struct {
	directExternrefs []directRootedExternrefValue
}

func (entries *referenceTokenEntries) claimCompactExternRoot(value any) bool {
	root := claimCompactExternRoot(value)
	if root.direct != nil {
		entries.directExternrefs = append(entries.directExternrefs, root.direct)
		return true
	}
	return root.result.appendTo(&entries.gcResultReleaseBatch)
}

func (entries *referenceTokenEntries) claimCompactExternRoots(slots []externrefSlot) {
	for _, slot := range slots {
		entries.claimCompactExternRoot(slot.value)
	}
}

func (batch *compactExternReleaseBatch) releaseDirectExternrefs(cleanup *resourceCleanup) {
	for i, value := range batch.directExternrefs {
		batch.directExternrefs[i] = nil
		cleanup.run(value.releaseExternrefRoot)
	}
}

// Retire public tokens when a logically closed host cell remains physically
// attached to an instance. Native pins and domain claims survive until detach.
func (s *referenceStore) closedHostPublicTokenEntriesLocked() referenceTokenEntries {
	if !nativeCollectorFrameRootsBuilt || !s.runtimeClosed || s.liveInstances != 0 || s.hasObjectTokensLocked() ||
		!s.allClosedInstancesQuiescedLocked() || !railshotClosedCompactExternHostObjectsLocked(s, s.liveObjects) {
		return referenceTokenEntries{}
	}
	return s.releaseClosedHostPublicTokensLocked()
}

func (s *referenceStore) releaseClosedHostPublicTokensLocked() referenceTokenEntries {
	var entries referenceTokenEntries
	for token, entry := range s.byToken {
		if entry.resultClaims != 0 {
			continue
		}
		delete(s.byToken, token)
		entries.funcrefs = append(entries.funcrefs, entry)
	}
	for identity, entry := range s.byIdentity {
		if s.byToken[entry.token] == nil {
			delete(s.byIdentity, identity)
		}
	}
	for i := range s.externrefs {
		slot := &s.externrefs[i]
		if slot.nextFree != 0 {
			continue
		}
		if !entries.claimCompactExternRoot(slot.value) {
			continue
		}
		slot.value = releasedExternrefValue
		slot.generation = nextExternrefGeneration(slot.generation)
		slot.nextFree = s.externFree
		s.externFree = uint32(i) + 1
	}
	return entries
}
