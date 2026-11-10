// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package plugins

import (
	"github.com/wago-org/wago/codegen"
	amd64codegen "github.com/wago-org/wago/codegen/amd64"
	arm64codegen "github.com/wago-org/wago/codegen/arm64"
)

func testManagedLowering() codegen.Lowering {
	return &arm64codegen.Lowering{Compatibility: arm64codegen.CompatibilityManaged, Managed: func(arm64codegen.ManagedContext) error { return nil }}
}

func testFullAccessLowering() codegen.Lowering {
	return &arm64codegen.Lowering{Compatibility: arm64codegen.CompatibilityFullAccess, Emit: func(arm64codegen.Context) error { return nil }}
}

func testWrongTargetLowering() codegen.Lowering {
	return &amd64codegen.Lowering{Compatibility: amd64codegen.CompatibilityManaged, Managed: func(amd64codegen.ManagedContext) error { return nil }}
}

func invalidateTestLowering(lowering codegen.Lowering) {
	lowering.(*arm64codegen.Lowering).Compatibility = 0
}

func testLoweringIsManaged(lowering codegen.Lowering) bool {
	return lowering.(*arm64codegen.Lowering).Compatibility == arm64codegen.CompatibilityManaged
}
