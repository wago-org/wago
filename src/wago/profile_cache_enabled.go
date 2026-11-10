// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_profile

package wago

import (
	"sync"
	"sync/atomic"
)

// Only profiling builds retain a directory of live thunk mappings. These views
// carry no lifetime reference: unmapping removes them under the same mutex used
// by attachment to copy any requested bytes into the session.
type profileCacheState struct {
	profile           atomic.Pointer[compiledProfile]
	profileMu         sync.Mutex
	liveProfileThunks map[uintptr]profileThunkImage
}
type profileThunkImage struct {
	code    []byte
	regions []CodeProfileRegion
}

func (cc *compiledCodeCache) loadProfile() *compiledProfile {
	if cc == nil {
		return nil
	}
	return cc.profile.Load()
}
func (cc *compiledCodeCache) storeProfile(p *compiledProfile) { cc.profile.Store(p) }

// The caller also holds cc.mu, ordering body publication and final retirement.
func (cc *compiledCodeCache) attachProfile(p *compiledProfile, code []byte) {
	cc.profileMu.Lock()
	defer cc.profileMu.Unlock()
	cc.storeProfile(p)
	if cc.sealed && cc.mem != nil {
		p.register(cc.base, code, p.image.Regions, p.image.Functions, true, true)
	}
	for base, thunk := range cc.liveProfileThunks {
		p.register(base, thunk.code, thunk.regions, nil, false, true)
	}
}
func (cc *compiledCodeCache) registerProfileThunk(base uintptr, code []byte, regions []CodeProfileRegion) {
	cc.profileMu.Lock()
	defer cc.profileMu.Unlock()
	if cc.liveProfileThunks == nil {
		cc.liveProfileThunks = make(map[uintptr]profileThunkImage)
	}
	cc.liveProfileThunks[base] = profileThunkImage{code: code, regions: regions}
	if p := cc.loadProfile(); p != nil {
		p.register(base, code, regions, nil, false, false)
	}
}
func (cc *compiledCodeCache) retireProfileImage(base uintptr) {
	cc.profileMu.Lock()
	defer cc.profileMu.Unlock()
	delete(cc.liveProfileThunks, base)
	if p := cc.loadProfile(); p != nil {
		p.retire(base)
	}
}
