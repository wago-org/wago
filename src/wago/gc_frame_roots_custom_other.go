// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !linux || !riscv64 || tinygo || wago_standalone

package wago

import (
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func newGCFrameRootPlanWithInstructions(m *wasm.Module, exact, generic, unwindOnly bool, diagnostic *string, analysis *wasm.ValidatedModuleAnalysis, _ map[uint32]railcore.CustomInstruction) *shared.GCModuleFrameRootPlan {
	return newGCFrameRootPlanMode(m, exact, generic, unwindOnly, diagnostic, analysis)
}

func railshotGCRootedScalarCustomModule(*wasm.Module, map[uint32]railcore.CustomInstruction) bool {
	return false
}
