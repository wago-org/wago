// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone

package wago

import (
	"runtime"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/optimization"
)

// These lightweight compile-contract definitions keep the public package
// type-correct while allowing whole-program builds to discard source codegen.
// railshotCompileModuleWith is unreachable in a wago_standalone executable.
type railshotImportBinding = shared.ImportBinding
type railshotKnobInfo = optimization.Info
type railshotOptimizationSnapshot = optimization.Snapshot
type railshotModuleStats struct{}

type railshotCompileOptions struct {
	NativeTailCalls          bool
	NativeReferenceCalls     bool
	NativeReferenceFunctions bool
	SourceMaps               bool
	UnwindMaps               bool
	Profile                  bool
	BitCountFeatures         uint8
	Optimizations            map[string]bool
	OptimizationSnapshot     railshotOptimizationSnapshot
	OptimizationDeltas       map[string]bool
	Workers                  int
	DeferCodeMapping         bool
	ElideBoundsChecks        bool
	NoBoundsFacts            bool
	ImportBindings           []railshotImportBinding
	SyncHostCalls            bool
	SyncHostSlots            int
	Interruptible            bool
	MemoryPressureAt         int
	MemoryPressure           func()
	GCTypeSubtypingRefTest   bool
	GCStructHelpers          bool
	GCArrayHelpers           bool
	GCFrameRoots             *shared.GCModuleFrameRootPlan
	GeneralI31Source         bool
	Codegen                  codegen.Options
	Stats                    *railshotModuleStats
	CustomInstructions       map[uint32]railshot.CustomInstruction
}

func railshotOptKnobs() []railshotKnobInfo {
	return optimization.InfosForArch(runtime.GOARCH)
}

func railshotOptKnobSnapshot() ([]railshotKnobInfo, railshotOptimizationSnapshot) {
	return railshotOptKnobs(), railshotOptimizationSnapshot{}
}

func railshotCurrentOptKnobSnapshot() railshotOptimizationSnapshot {
	return railshotOptimizationSnapshot{}
}

func railshotSetOptKnob(string, bool) bool { return false }

func railshotGCNativeCodeTelemetry(*railshotModuleStats) GCNativeCodeTelemetry {
	return GCNativeCodeTelemetry{}
}
