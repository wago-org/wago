// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo && wago_no_gc && wago_no_exception_handling

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type gcFrameOperandPlanning struct{}

type gcFrameModuleState struct{}

func newGCFrameModuleState(*CompileOptions) gcFrameModuleState { return gcFrameModuleState{} }

func (*gcFrameModuleState) bindFunction(*emissionMode, int) {}

type gcFrameEmissionMode struct{}

func (*gcFrameEmissionMode) gcFramePlan() *shared.GCFrameRootPlan { return nil }

func (*gcFrameEmissionMode) setRootedTypeDirectory(*internalReferenceTypeDirectory) {}

func (*gcFrameEmissionMode) rootedTypeDirectory() internalReferenceTypeDirectory {
	return internalReferenceTypeDirectory{}
}

func (*gcFrameEmissionMode) relocateGCFrameCallsites(int) error { return nil }

func prepareCollectorOperandPlans(*wasm.Module, *CompileOptions) error { return nil }
