// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64 && !wago_profile

package arm64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

type profileFnState struct{}
type profileOrigin struct{}

func (*fn) recordProfileCodeSite(int, string)         {}
func (*fn) profileCodeSites() []shared.NativeCodeSite { return nil }

func (*fn) rememberProfileNode(*elem)                    {}
func (*fn) enterProfileNode(*elem) profileOrigin         { return profileOrigin{} }
func (*fn) recordProfileTrap(int)                        {}
func (*fn) profileTrapOrigin(int) (uint32, uint32, bool) { return 0, 0, false }

func (*fn) rewindProfileEmission(int)                         {}
func (*fn) profileEmissionRanges() []shared.NativeSourceRange { return nil }

func (*fn) switchProfileOrigin(profileOrigin) {}

func (*fn) enterProfileInline() uint32                      { return 0 }
func (*fn) leaveProfileInline(uint32)                       {}
func (*fn) profileTrapCaller(int) uint32                    { return 0 }
func (*fn) profileInlineFrames() []shared.NativeInlineFrame { return nil }

func (*fn) enterProfileInstruction() profileOrigin { return profileOrigin{} }
