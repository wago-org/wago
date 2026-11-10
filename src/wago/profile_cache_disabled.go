// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_profile

package wago

// Ordinary compiled modules carry no profiling state or thunk directory.
type profileCacheState struct{}

func (*compiledCodeCache) loadProfile() *compiledProfile                             { return nil }
func (*compiledCodeCache) storeProfile(*compiledProfile)                             {}
func (*compiledCodeCache) attachProfile(*compiledProfile, []byte)                    {}
func (*compiledCodeCache) registerProfileThunk(uintptr, []byte, []CodeProfileRegion) {}
func (*compiledCodeCache) retireProfileImage(uintptr)                                {}
