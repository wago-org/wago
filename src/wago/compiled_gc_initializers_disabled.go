// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_no_gc

package wago

import "github.com/wago-org/wago/src/core/compiler/wasm"

type compiledGCInitializers struct{}

func (*compiledGCInitializers) gcStructGlobals() []gcStructGlobalInit { return nil }

func (*compiledGCInitializers) gcArrayGlobals() []gcArrayGlobalInit { return nil }

func (*compiledGCInitializers) gcArrayElement() *gcArrayElementInit { return nil }

func (*compiledGCInitializers) hasGCInitializers() bool { return false }

func (*Compiled) compileGCInitializers(*wasm.Module, stagedGCStructProduct, stagedGCArrayProduct) error {
	return checkCompiledFeatures(CoreFeatureGC)
}
