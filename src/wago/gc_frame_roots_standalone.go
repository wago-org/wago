// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone && ((linux && amd64) || ((linux || darwin) && arm64))

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func newGCFrameRootPlan(_ *wasm.Module, exactRoots, _ bool, diagnostic *string, _ *wasm.ValidatedModuleAnalysis) *shared.GCModuleFrameRootPlan {
	if diagnostic != nil {
		*diagnostic = ""
	}
	if !exactRoots {
		return nil
	}
	if diagnostic != nil {
		*diagnostic = "source compilation is unavailable in a standalone executable"
	}
	return nil
}
