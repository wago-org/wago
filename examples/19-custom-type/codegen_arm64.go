// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64 && !tinygo

package main

import (
	"github.com/wago-org/wago/codegen"
	arm64 "github.com/wago-org/wago/codegen/arm64"
)

func zeroCodegen() codegen.Lowering {
	return &arm64.Lowering{
		Compatibility: arm64.CompatibilityFullAccess,
		Emit: func(ctx arm64.Context) error {
			low := ctx.AllocVector()
			high := ctx.AllocVector(low)
			ctx.Encoder().Eor16b(low, low, low)
			ctx.Encoder().Eor16b(high, high, high)
			return ctx.OutputCustom(low, high)
		},
	}
}

func discardCodegen() codegen.Lowering {
	return &arm64.Lowering{
		Compatibility: arm64.CompatibilityFullAccess,
		Emit: func(ctx arm64.Context) error {
			value, err := ctx.InputCustom(0)
			if err != nil {
				return err
			}
			for _, register := range value {
				ctx.ReleaseVector(register)
			}
			return nil
		},
	}
}
