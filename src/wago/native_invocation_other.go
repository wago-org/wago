// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !riscv64

package wago

import coreruntime "github.com/wago-org/wago/src/core/runtime"

const railshotNativeInvocationContextBytes = coreruntime.InstanceContextBytes

func railshotBindNativeInvocation(*Instance)                        {}
func railshotRestoreNativeInvocation(*Instance, *Instance, uintptr) {}
func railshotSaveNativeInvocation(*Instance) uintptr                { return 0 }
func railshotRestoreSavedNativeInvocation(*Instance, uintptr)       {}

func railshotParkedNativeInvocation(*Instance) uintptr { return 0 }

func railshotParkedNativeContextChanged(*Instance, uintptr, uintptr) bool { return false }

type railshotPublicFuncrefArguments struct{}

func railshotBeginPublicFuncrefArguments(*Instance) (railshotPublicFuncrefArguments, error) {
	return railshotPublicFuncrefArguments{}, nil
}
func railshotStagePublicFuncrefArgument(*Instance, *railshotPublicFuncrefArguments, uint64, uint64) error {
	return nil
}
func railshotStagePublicExternrefArgument(*Instance, *railshotPublicFuncrefArguments, uint64) error {
	return nil
}
func railshotReleasePublicFuncrefArguments(railshotPublicFuncrefArguments) {}
